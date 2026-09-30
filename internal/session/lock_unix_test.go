//go:build unix

package session

import (
	"os"
	"testing"
	"time"
)

// A writer that opened the lock file before pruning deleted it must not lock
// the deleted file, or it and a writer on the new file would both hold a lock.
func TestLockOpenedRejectsReplacedFile(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":false}`)
	path := lockFile(root, "s1")
	stale, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil { // pruned
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil { // another writer's new file
		t.Fatal(err)
	}

	f, busy, err := lockOpened(stale, path)
	if f != nil || !busy || err != nil {
		if f != nil {
			unlockFile(f)
		}
		t.Fatalf("lockOpened on a replaced file = (%v, %v, %v), want busy so the caller retries", f, busy, err)
	}
	fresh, busy, err := tryLockFile(path)
	if fresh == nil || busy || err != nil {
		t.Fatalf("the retry should lock the new file: (%v, %v, %v)", fresh, busy, err)
	}
	unlockFile(fresh)
}

func TestPruneSkipsHeldLock(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "other", `{"area":"a","project":"p","dirty":false}`) // creates the directory
	path := lockFile(root, "gone")
	held, _, err := tryLockFile(path)
	if err != nil || held == nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer unlockFile(held)
	touch(t, path, time.Now().Add(-30*24*time.Hour))

	pruneStaleContexts(root, 7)

	if _, err := os.Stat(path); err != nil {
		t.Error("pruning must not delete a lock file that a writer holds")
	}
}
