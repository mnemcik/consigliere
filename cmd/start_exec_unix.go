//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// execAgent replaces the cg process with the agent, so the terminal, signals
// (Ctrl+C) and the exit code belong to the agent alone, exactly as if it had
// been started directly. It only returns on failure.
func execAgent(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ()) //nolint:gosec // path resolved via exec.LookPath from the workspace's own setting
}
