package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Badge writers run as separate processes (hook bodies, agent commands), so
// two of them can read the same badge and the later write drops the earlier
// change. withBadgeLock serialises each read-modify-write per session with an
// OS advisory lock (flock on Unix, LockFileEx on Windows) held on a lock file
// next to the badge. The OS releases the lock when its holder exits, so a
// crashed writer never leaves a stale lock behind, and lock files are never
// deleted while a session's badge exists.
//
// The lock fails open: a hook must never hang or fail the session, so after
// lockWait the change runs without the lock.
var (
	lockWait  = time.Second // below the 1.5 s budget Claude Code gives SessionEnd hooks
	lockRetry = 10 * time.Millisecond
)

// lockFile returns the lock path for a session's badge. It does not end in
// .json, so badge globs never pick it up.
func lockFile(root, sessionID string) string {
	return filepath.Join(ContextDir(root), sessionID+".lock")
}

// withBadgeLock runs fn while holding the session's badge lock. When the lock
// cannot be taken (no session-context directory yet, an OS error, or another
// writer holding it beyond lockWait), fn runs without it.
func withBadgeLock(root, sessionID string, fn func() error) error {
	if f := acquireBadgeLock(lockFile(root, sessionID)); f != nil {
		defer unlockFile(f)
	}
	return fn()
}

// acquireBadgeLock returns the open, locked lock file, or nil when the caller
// should proceed unlocked. A successful lock refreshes the file's modification
// time, which the session gate uses to prune lock files nobody uses.
func acquireBadgeLock(path string) *os.File {
	deadline := time.Now().Add(lockWait)
	for {
		f, busy, err := tryLockFile(path)
		if f != nil {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			return f
		}
		if err != nil || !busy || time.Now().After(deadline) {
			return nil // fail open rather than block the session
		}
		time.Sleep(lockRetry)
	}
}

// removeBadge deletes a session's badge file. The caller holds the lock.
func removeBadge(root, sessionID string) error {
	if err := os.Remove(ContextFile(root, sessionID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
