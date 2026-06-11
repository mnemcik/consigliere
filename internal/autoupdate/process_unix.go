//go:build !windows

package autoupdate

import "syscall"

// processAlive reports whether a process with the given PID exists. Signal 0
// performs error checking without delivering a signal: nil or EPERM means the
// process is alive (EPERM = exists but owned by another user), ESRCH means gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
