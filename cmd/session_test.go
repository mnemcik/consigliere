package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mnemcik/consigliere/internal/session"
)

// runSession executes `cg session <args>` in dir with stdin, resetting flags
// left over from earlier runs of the shared command tree.
func runSession(t *testing.T, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	for _, c := range []*cobra.Command{sessionSetContextCmd} {
		c.SilenceUsage = false // a previous run's RunE sets it on the shared command
		c.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetArgs(append([]string{"session"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

// newGitRepo creates a committed git repo at dir, with a .cg.json at ws (a
// path relative to dir) when ws is non-empty. It returns dir with symlinks
// resolved, matching the paths git reports.
func newGitRepo(t *testing.T, dir, ws, cgJSON string) string {
	t.Helper()
	ctx := context.Background()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, dir, "init", "--quiet")
	if ws != "" {
		writeFile(t, dir, filepath.ToSlash(filepath.Join(ws, ".cg.json")), cgJSON)
	} else {
		writeFile(t, dir, "README.md", "plain repo\n")
	}
	mustGit(t, ctx, dir, "add", "-A")
	mustGit(t, ctx, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "init")
	return dir
}

// addWorktree adds a linked worktree of repo and returns its path.
func addWorktree(t *testing.T, repo string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"--wt")
	mustGit(t, context.Background(), repo, "worktree", "add", "--quiet", "-b", "session/wt", wt)
	return wt
}

func TestSessionSetContextFromLinkedWorktree(t *testing.T) {
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	wt := addWorktree(t, repo)

	out, err := runSession(t, wt, "", "set-context", "--session-id", "s1", "--area", "platform", "--project", "api")
	if err != nil {
		t.Fatalf("set-context: %v\n%s", err, out)
	}
	c, err := session.ReadContext(repo, "s1")
	if err != nil || c == nil || c.Area != "platform" || c.Project != "api" {
		t.Fatalf("badge not written at the main worktree root: (%+v, %v)", c, err)
	}
	if _, err := os.Stat(session.ContextDir(wt)); !os.IsNotExist(err) {
		t.Error("badge directory must not be created inside the linked worktree")
	}

	// mark-dirty and statusline, run from the linked worktree, see the same badge.
	stdin := `{"session_id":"s1","cwd":"` + filepath.ToSlash(wt) + `"}`
	if out, err := runSession(t, wt, stdin, "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(repo, "s1"); c == nil || !c.Dirty {
		t.Errorf("mark-dirty from a linked worktree did not flag the badge: %+v", c)
	}
	out, err = runSession(t, wt, stdin, "statusline")
	if err != nil {
		t.Fatalf("statusline: %v", err)
	}
	if !strings.Contains(out, "[platform/api]") {
		t.Errorf("statusline from a linked worktree = %q, want the badge", out)
	}
}

func TestSessionSetContextRefusesOutsideWorkspace(t *testing.T) {
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "plain"), "", "")

	out, err := runSession(t, repo, "", "set-context", "--session-id", "s1", "--area", "a", "--project", "p")
	if err == nil || !strings.Contains(err.Error(), "not inside a Consigliere workspace") {
		t.Fatalf("set-context in a plain git repo: err = %v, want a workspace error\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude")); !os.IsNotExist(err) {
		t.Error("set-context outside a workspace must not create .claude/")
	}
}

func TestSessionSetContextRejectsBadValuesWithoutUsage(t *testing.T) {
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	cases := [][]string{
		{"--session-id", "s1", "--area", "  ", "--project", "p"},
		{"--session-id", "../x", "--area", "a", "--project", "p"},
	}
	for _, flags := range cases {
		out, err := runSession(t, repo, "", append([]string{"set-context"}, flags...)...)
		if err == nil {
			t.Errorf("set-context %v should fail", flags)
		}
		if strings.Contains(out, "Usage:") {
			t.Errorf("set-context %v printed the usage block for a bad value:\n%s", flags, out)
		}
	}
}

func TestSessionStatuslineKeepsNestedWorkspaceSettings(t *testing.T) {
	// The git repo is outer/; the workspace (and its .cg.json) is outer/kb/.
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "outer"), "kb", `{"type":"consigliere","session":{"statuslineUpstream":"echo UPSTREAM"}}`)
	kb := filepath.Join(repo, "kb")

	stdin := `{"session_id":"s1","cwd":"` + filepath.ToSlash(kb) + `"}`
	out, err := runSession(t, kb, stdin, "statusline")
	if err != nil {
		t.Fatalf("statusline: %v", err)
	}
	if !strings.Contains(out, "UPSTREAM") {
		t.Errorf("statusline in a nested workspace = %q, want the configured upstream", out)
	}
}
