// Package cgerr provides errors that carry a process exit code, so command
// handlers can preserve the exit-code contract the shell helpers established
// (callers such as the wrap skill branch on these codes). main.go inspects the
// returned error for an ExitCode() method and exits with that code.
package cgerr

import "fmt"

// Exit codes. These mirror the contract of the bash helpers being promoted into
// cg subcommands (notably land-worktree-commit.sh), so existing callers that
// branch on the numeric code keep working after the port.
const (
	ExitUsage      = 1 // argument / usage error
	ExitDirty      = 2 // unlanded or uncommitted work blocks the operation
	ExitConflict   = 3 // rebase conflict; a rebase is left in progress
	ExitPushFail   = 4 // push to the remote failed after retries
	ExitAssertFail = 5 // a post-operation assertion failed
)

// CodedError wraps an error with a process exit code.
type CodedError struct {
	Code int
	Err  error
}

func (c *CodedError) Error() string { return c.Err.Error() }

func (c *CodedError) Unwrap() error { return c.Err }

// ExitCode returns the process exit code associated with this error.
func (c *CodedError) ExitCode() int { return c.Code }

// New builds a CodedError with the given exit code and a formatted message.
func New(code int, format string, a ...any) *CodedError {
	return &CodedError{Code: code, Err: fmt.Errorf(format, a...)}
}

// Wrap attaches an exit code to an existing error. Returns nil if err is nil.
func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}
