package cmd

import (
	"os"
	"testing"
)

// TestMain clears the variables a session started with `cg` (or an agent)
// sets, so tests run from inside such a session don't consult the developer's
// own workspace or session state. Tests that need them set them with
// t.Setenv.
func TestMain(m *testing.M) {
	for _, k := range []string{envWorkspace, envCommand, envExtensionWorkspace, envSessionWorktree, "CG_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
