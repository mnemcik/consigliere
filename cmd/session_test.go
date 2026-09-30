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

// runSession executes `cg session <args>` (or `cg active <args>` when args
// start with "active") in dir with stdin, resetting flags left over from
// earlier runs of the shared command tree. Session-ID environment variables
// are cleared so the host agent's own session never leaks into a test; set
// them with t.Setenv after calling clearSessionEnv when a test needs them.
func runSession(t *testing.T, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	for _, c := range []*cobra.Command{sessionSetContextCmd, sessionReleaseCmd, sessionPauseCmd, activeCmd} {
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
	if len(args) > 0 && args[0] == "active" {
		rootCmd.SetArgs(args)
	} else {
		rootCmd.SetArgs(append([]string{"session"}, args...))
	}
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
	clearSessionEnv(t)
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
	clearSessionEnv(t)
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
	clearSessionEnv(t)
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

// clearSessionEnv unsets the session-ID variables resolveSessionID reads.
func clearSessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CG_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
}

func TestResolveSessionIDOrder(t *testing.T) {
	clearSessionEnv(t)
	if got := resolveSessionID(""); got != "" {
		t.Errorf("no sources: got %q, want empty", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "claude")
	if got := resolveSessionID(""); got != "claude" {
		t.Errorf("Claude fallback: got %q", got)
	}
	t.Setenv("CG_SESSION_ID", "cg")
	if got := resolveSessionID(""); got != "cg" {
		t.Errorf("CG_SESSION_ID should win over CLAUDE_CODE_SESSION_ID: got %q", got)
	}
	if got := resolveSessionID(" flag "); got != "flag" {
		t.Errorf("flag should win: got %q", got)
	}
}

func TestSessionLifecycleEndToEnd(t *testing.T) {
	clearSessionEnv(t)
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)

	// Session A claims p (session ID from the agent-neutral env var) and pauses.
	t.Setenv("CG_SESSION_ID", "sA")
	if out, err := runSession(t, repo, "", "set-context", "--area", "a", "--project", "p"); err != nil {
		t.Fatalf("A set-context: %v\n%s", err, out)
	}
	if out, err := runSession(t, repo, `{"session_id":"sA"}`, "mark-dirty"); err != nil {
		t.Fatalf("A mark-dirty: %v\n%s", err, out)
	}
	writeFile(t, repo, "projects/p/resume.md", "cursor\n")
	if out, err := runSession(t, repo, "", "pause"); err != nil || !strings.Contains(out, "Session paused: sA") {
		t.Fatalf("A pause: %v\n%s", err, out)
	}

	// Session B sees p as paused, not live.
	t.Setenv("CG_SESSION_ID", "sB")
	out, err := runSession(t, repo, "", "active", "--slugs")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("paused project must not be in --slugs: %q (%v)", out, err)
	}
	out, _ = runSession(t, repo, "", "active")
	if !strings.Contains(out, "\tsA\tpaused") {
		t.Errorf("cg active should list sA as paused:\n%s", out)
	}

	// B resumes p: A's claim is handed over, and B never lists itself.
	out, err = runSession(t, repo, "", "set-context", "--area", "a", "--project", "p")
	if err != nil || !strings.Contains(out, "released session sA") {
		t.Fatalf("B set-context should hand over from sA: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(repo, "sA"); c != nil {
		t.Errorf("sA badge should be gone after hand-over: %+v", c)
	}
	// resume.md is still there (B has not deleted it yet), but B is live.
	out, _ = runSession(t, repo, "", "active", "--slugs")
	if strings.TrimSpace(out) != "" {
		t.Errorf("the caller must not list itself: %q", out)
	}
	// A third session (flag beats env) sees B live, not paused.
	out, _ = runSession(t, repo, "", "active", "--slugs", "--session-id", "sC")
	if strings.TrimSpace(out) != "p" {
		t.Errorf("sC should see p live: %q", out)
	}
	// ...and claiming p itself does not take B's claim away.
	out, err = runSession(t, repo, "", "set-context", "--area", "a", "--project", "p", "--session-id", "sC")
	if err != nil || strings.Contains(out, "released session") {
		t.Fatalf("sC must not release B, who already resumed p: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(repo, "sB"); c == nil {
		t.Fatal("sB badge must survive a later claim on p")
	}
	if err := session.Release(repo, "sC"); err != nil {
		t.Fatal(err)
	}

	// B end-wraps: release deletes the badge, and nobody sees p afterwards.
	if out, err := runSession(t, repo, "", "release"); err != nil {
		t.Fatalf("B release: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(repo, "sB"); c != nil {
		t.Errorf("sB badge should be deleted by release: %+v", c)
	}
	out, _ = runSession(t, repo, "", "active", "--slugs", "--session-id", "sC")
	if strings.TrimSpace(out) != "" {
		t.Errorf("a released session must not be listed: %q", out)
	}
}

func TestSessionReleaseNeedsSessionID(t *testing.T) {
	clearSessionEnv(t)
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	out, err := runSession(t, repo, "", "release")
	if err == nil || !strings.Contains(err.Error(), "missing session ID") {
		t.Fatalf("release without an ID: err = %v\n%s", err, out)
	}
}

func TestSessionEndReleasesCleanClaimsSilently(t *testing.T) {
	clearSessionEnv(t)
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	for id, dirty := range map[string]string{"clean": "false", "dirty": "true"} {
		if err := session.WriteContext(repo, id, "a", "p"); err != nil {
			t.Fatal(err)
		}
		if dirty == "true" {
			if err := session.MarkDirty(repo, id); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, id := range []string{"clean", "dirty"} {
		stdin := `{"session_id":"` + id + `","cwd":"` + filepath.ToSlash(repo) + `","hook_event_name":"SessionEnd","reason":"clear"}`
		out, err := runSession(t, repo, stdin, "end")
		if err != nil {
			t.Fatalf("session end %s: %v", id, err)
		}
		if out != "" {
			t.Errorf("session end %s printed %q; Claude Code shows SessionEnd output to the user", id, out)
		}
	}
	if c, _ := session.ReadContext(repo, "clean"); c != nil {
		t.Errorf("a clean claim should be released on SessionEnd: %+v", c)
	}
	if c, _ := session.ReadContext(repo, "dirty"); c == nil {
		t.Error("a dirty claim must survive SessionEnd: its worktree may hold unlanded work")
	}

	// Malformed input and unsafe IDs are ignored, never an error.
	for _, stdin := range []string{`not json`, `{"session_id":"../x"}`, `{}`} {
		if out, err := runSession(t, repo, stdin, "end"); err != nil || out != "" {
			t.Errorf("session end with %q = (%q, %v), want silent success", stdin, out, err)
		}
	}
}
