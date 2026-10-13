package cmd

import (
	"fmt"
	"io"
	"runtime"

	"github.com/mnemcik/consigliere/internal/gitbash"
)

// gitBashWarning returns the warning for a Windows machine where Git Bash
// isn't found, or "" when there is nothing to warn about. cg's hooks and status
// line are bash scripts, and without Git for Windows Claude Code runs them in
// PowerShell, where they don't run and nothing says why (consigliere#169).
func gitBashWarning(goos string, find func() (string, bool)) string {
	if goos != "windows" {
		return ""
	}
	if _, ok := find(); ok {
		return ""
	}
	return "Git Bash not found. cg's hooks and status line are bash scripts, and on Windows " +
		"Claude Code runs them only when Git for Windows is installed (https://git-scm.com/downloads/win). " +
		"Without it they don't run. If Git Bash is installed where cg can't find it, set " +
		gitbash.EnvVar + " to the path of its bash.exe."
}

// printGitBashWarning prints gitBashWarning for this machine to w.
func printGitBashWarning(w io.Writer) {
	if msg := gitBashWarning(runtime.GOOS, gitbash.Find); msg != "" {
		_, _ = fmt.Fprintf(w, "warning: %s\n", msg)
	}
}
