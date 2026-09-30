package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestBadgeLockSerialisesReadModifyWrite runs many concurrent increments of a
// badge field. Without the lock, writers that read the same value lose each
// other's updates.
func TestBadgeLockSerialisesReadModifyWrite(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false,"n":0}`)
	oldWait := lockWait
	lockWait = 10 * time.Second // never fail open during this test
	t.Cleanup(func() { lockWait = oldWait })

	const workers, each = 20, 10
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				err := withBadgeLock(root, "s1", func() error {
					m, err := readContextMap(ContextFile(root, "s1"))
					if err != nil {
						return err
					}
					n, _ := m["n"].(json.Number).Int64()
					m["n"] = n + 1
					return writeJSONAtomic(ContextFile(root, "s1"), m)
				})
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()

	m, err := readContextMap(ContextFile(root, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := m["n"].(json.Number).Int64(); n != workers*each {
		t.Errorf("n = %d after %d locked increments: updates were lost", n, workers*each)
	}
	assertLockFree(t, root, "s1")
}

func TestBadgeLockFailsOpenWhenHeld(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	// Another writer holds the lock through its own open file.
	held, busy, err := tryLockFile(lockFile(root, "s1"))
	if err != nil || held == nil || busy {
		t.Fatalf("taking the lock = (%v, %v, %v)", held, busy, err)
	}
	defer unlockFile(held)
	oldWait := lockWait
	lockWait = 50 * time.Millisecond
	t.Cleanup(func() { lockWait = oldWait })

	start := time.Now()
	if err := MarkDirty(root, "s1"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < lockWait {
		t.Errorf("MarkDirty should wait for a held lock before failing open (took %v)", time.Since(start))
	}
	if c, _ := ReadContext(root, "s1"); c == nil || !c.Dirty {
		t.Errorf("a held lock must not block the change: %+v", c)
	}
	if f, busy, _ := tryLockFile(lockFile(root, "s1")); f != nil || !busy {
		if f != nil {
			unlockFile(f)
		}
		t.Error("failing open must leave the other writer's lock in place")
	}
}

func TestBadgeLockReleasedOnClose(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	held, _, err := tryLockFile(lockFile(root, "s1"))
	if err != nil || held == nil {
		t.Fatalf("taking the lock: %v", err)
	}
	// Closing (as the OS does when the holder exits) releases the lock, so
	// the next writer gets it at once. There is no stale lock to wait out.
	unlockFile(held)
	start := time.Now()
	if err := MarkDirty(root, "s1"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= lockWait {
		t.Errorf("a released lock should be taken at once (took %v)", time.Since(start))
	}
	assertLockFree(t, root, "s1")
}

func TestPruneKeepsLocksWhileBadgeExists(t *testing.T) {
	root := t.TempDir()
	ctx := ContextDir(root)
	oldTime := time.Now().Add(-30 * 24 * time.Hour)
	writeCtx(t, root, "live", `{"area":"a","project":"p","dirty":false}`)
	for _, id := range []string{"live", "gone-old", "gone-fresh"} {
		if err := os.WriteFile(lockFile(root, id), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	touch(t, lockFile(root, "live"), oldTime)
	touch(t, lockFile(root, "gone-old"), oldTime)

	pruneStaleContexts(root, 7)

	for id, want := range map[string]bool{"live": true, "gone-old": false, "gone-fresh": true} {
		_, err := os.Stat(filepath.Join(ctx, id+".lock"))
		if exists := err == nil; exists != want {
			t.Errorf("lock %s exists = %v after pruning, want %v", id, exists, want)
		}
	}
}

func TestBadgeLockIgnoredByBadgeGlobs(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	if err := os.WriteFile(lockFile(root, "s1"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ActiveProjects(root, ActiveOptions{Now: time.Now(), ActiveWindow: time.Hour, DirtyWindow: time.Hour})
	if err != nil || len(got) != 1 || got[0].SessionID != "s1" {
		t.Errorf("ActiveProjects with a lock file present = (%+v, %v), want only s1", got, err)
	}
}

// assertLockFree checks that no writer still holds the session's lock.
func assertLockFree(t *testing.T, root, sessionID string) {
	t.Helper()
	f, busy, err := tryLockFile(lockFile(root, sessionID))
	if err != nil || busy || f == nil {
		t.Errorf("lock for %s is still held (busy=%v, err=%v)", sessionID, busy, err)
		return
	}
	unlockFile(f)
}
