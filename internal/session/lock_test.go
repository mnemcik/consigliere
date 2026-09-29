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
	assertNoLockFiles(t, root)
}

func TestBadgeLockClearsStaleLock(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	lock := lockFile(root, "s1")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	touch(t, lock, time.Now().Add(-time.Minute)) // left over from a crashed writer

	start := time.Now()
	if err := MarkDirty(root, "s1"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= lockWait {
		t.Errorf("a stale lock should be cleared, not waited out (took %v)", time.Since(start))
	}
	if c, _ := ReadContext(root, "s1"); c == nil || !c.Dirty {
		t.Errorf("MarkDirty did not apply: %+v", c)
	}
	assertNoLockFiles(t, root)
}

func TestBadgeLockFailsOpenWhenHeld(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	lock := lockFile(root, "s1")
	if err := os.WriteFile(lock, nil, 0o600); err != nil { // held by another writer
		t.Fatal(err)
	}
	oldWait := lockWait
	lockWait = 50 * time.Millisecond
	t.Cleanup(func() { lockWait = oldWait })

	if err := MarkDirty(root, "s1"); err != nil {
		t.Fatal(err)
	}
	if c, _ := ReadContext(root, "s1"); c == nil || !c.Dirty {
		t.Errorf("a held lock must not block the change: %+v", c)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("the writer must not remove a lock it does not hold")
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

func assertNoLockFiles(t *testing.T, root string) {
	t.Helper()
	locks, _ := filepath.Glob(filepath.Join(ContextDir(root), "*.lock"))
	if len(locks) != 0 {
		t.Errorf("lock files left behind: %v", locks)
	}
}
