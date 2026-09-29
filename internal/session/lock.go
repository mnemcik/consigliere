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
// change. withBadgeLock serialises each read-modify-write per session with a
// lock file created exclusively next to the badge.
//
// The lock fails open: a hook must never hang or fail the session, so after
// lockWait the change runs without the lock. A lock older than lockStale is
// left over from a crashed writer and is removed.
var (
	lockWait  = time.Second // below the 1.5 s budget Claude Code gives SessionEnd hooks
	lockStale = 10 * time.Second
	lockRetry = 10 * time.Millisecond
)

// lockFile returns the lock path for a session's badge. It does not end in
// .json, so badge globs never pick it up.
func lockFile(root, sessionID string) string {
	return filepath.Join(ContextDir(root), sessionID+".lock")
}

// withBadgeLock runs fn while holding the session's badge lock. When the
// session-context directory does not exist yet there is no badge to race on,
// so fn runs without a lock (WriteContext creates the directory itself).
func withBadgeLock(root, sessionID string, fn func() error) error {
	path := lockFile(root, sessionID)
	if acquireBadgeLock(path) {
		defer func() { _ = os.Remove(path) }()
	}
	return fn()
}

// acquireBadgeLock creates the lock file exclusively and reports whether it
// holds it. It returns false, and the caller proceeds unlocked, when the lock
// cannot be taken: no session-context directory yet, an error other than
// "exists", or another writer holding it beyond lockWait.
func acquireBadgeLock(path string) bool {
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return true
		}
		if !errors.Is(err, fs.ErrExist) {
			return false // no directory yet, or cannot lock: fail open
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > lockStale {
			_ = os.Remove(path) // a crashed writer left it behind
			continue
		}
		if time.Now().After(deadline) {
			return false // held too long: fail open rather than block the session
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
