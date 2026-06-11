//go:build windows

package autoupdate

import "syscall"

// Windows process-creation flags (from winbase.h) — avoids depending on
// golang.org/x/sys/windows just for two constants.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// detachSysProcAttr starts the worker as a detached process in its own group so
// it survives the parent exiting and has no console attached.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}
