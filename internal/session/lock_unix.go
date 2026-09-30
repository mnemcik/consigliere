//go:build unix

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile opens path and takes an exclusive, non-blocking flock on it. It
// returns the locked file, or busy=true when the caller should retry: another
// holder has the lock, or the file was replaced after it was opened.
func tryLockFile(path string) (f *os.File, busy bool, err error) {
	f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	return lockOpened(f, path)
}

// lockOpened locks an already opened lock file. Pruning deletes a lock file
// only while holding its lock, so a writer that opened the file before it was
// deleted must not keep using it: after locking, it checks that path still
// names the same file, and otherwise releases it and reports busy, so the
// retry opens the file that now exists. Without this check, two writers could
// hold locks on two different files at the same path.
func lockOpened(f *os.File, path string) (*os.File, bool, error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	held, herr := f.Stat()
	named, nerr := os.Stat(path)
	if herr != nil || nerr != nil || !os.SameFile(held, named) {
		unlockFile(f)
		return nil, true, nil
	}
	return f, false, nil
}

// unlockFile releases the lock by unlocking and closing the file.
func unlockFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
