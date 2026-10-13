//go:build windows

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile opens path and takes an exclusive, non-blocking LockFileEx lock
// on its first byte. It returns the locked file, or busy=true when another
// holder has the lock.
func tryLockFile(path string) (f *os.File, busy bool, err error) {
	f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return f, false, nil
}

// unlockFile releases the lock by unlocking and closing the file.
func unlockFile(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
	_ = f.Close()
}

// removeLocked releases the lock f holds on path, then deletes path when remove
// is set. Windows can't delete a file that is open, so the lock has to go
// first. That is still safe: a writer that opens the file in between holds it
// open, and the delete then fails and leaves it.
func removeLocked(f *os.File, path string, remove bool) {
	unlockFile(f)
	if remove {
		_ = os.Remove(path)
	}
}
