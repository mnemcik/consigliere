//go:build !windows

package autoupdate

import "syscall"

// detachSysProcAttr starts the worker in its own session (Setsid) so it fully
// detaches from the parent's controlling terminal and process group.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
