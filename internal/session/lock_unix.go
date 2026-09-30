//go:build unix

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile opens path and takes an exclusive, non-blocking flock on it. It
// returns the locked file, or busy=true when another holder has the lock.
func tryLockFile(path string) (f *os.File, busy bool, err error) {
	f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return f, false, nil
}

// unlockFile releases the lock by unlocking and closing the file.
func unlockFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
