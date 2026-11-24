package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/session"
)

// committedWorkspace is a git-tracked workspace, its path with symlinks
// resolved.
func committedWorkspace(t *testing.T, cfgHome string, exts ...[]string) string {
	t.Helper()
	ws := sessionWorkspace(t, cfgHome, exts...)
	resolved, err := filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// Terms (session commands root): another Consigliere workspace is its own
// session commands root; the session's workspace and its linked worktrees use
// the session's; without CG_WORKSPACE it is the current workspace. The session
// repo root doesn't change.
func TestSessionCommandsRoot(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	wl := addBranchWorktree(t, w, "wl")
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)
	for _, tc := range []struct {
		name, cwd, want string
	}{
		{"the session's workspace", w, w},
		{"one of its linked worktrees", wl, w},
		{"another workspace", x, x},
		{"below another workspace's root", filepath.Join(x, "areas"), x},
		{"outside any workspace", t.TempDir(), w},
	} {
		if got := sessionCommandsRoot(tc.cwd); !samePath(got, tc.want) {
			t.Errorf("%s: sessionCommandsRoot = %q, want %s", tc.name, got, tc.want)
		}
		if got := sessionRepoRoot(tc.cwd); !samePath(got, w) {
			t.Errorf("%s: sessionRepoRoot = %q, want the session's workspace %s", tc.name, got, w)
		}
	}
	unsetEnv(t, envWorkspace)
	if got := sessionCommandsRoot(x); !samePath(got, x) {
		t.Errorf("without CG_WORKSPACE: %q, want %s", got, x)
	}
}

// S38c: with an inherited CG_WORKSPACE naming W, the session commands run in
// another workspace X use X's state files, gate, paused projects and badge
// root; dispatch from X still consults W first (S22).
func TestSessionCommandsInAnotherWorkspace(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)

	stdin := `{"prompt":"hi","session_id":"s1","cwd":"` + filepath.ToSlash(x) + `"}`
	out, err := runSession(t, x, stdin, "start-gate")
	if err != nil {
		t.Fatalf("start-gate: %v\n%s", err, out)
	}
	if !strings.Contains(out, session.ContextFile(x, "s1")) || strings.Contains(out, session.ContextFile(w, "s1")) {
		t.Errorf("gate from X doesn't name X's state file %s only:\n%s", session.ContextFile(x, "s1"), out)
	}

	if out, err := runSession(t, x, "", "set-context", "--session-id", "s2", "--area", "a", "--project", "p"); err != nil {
		t.Fatalf("set-context: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(x, "s2"); c == nil || c.Project != "p" {
		t.Errorf("claim from X not in X's state file: %+v", c)
	}
	if c, _ := session.ReadContext(w, "s2"); c != nil {
		t.Errorf("claim from X written to W's state file: %+v", c)
	}
	if out, err := runSession(t, x, "", "active", "--slugs"); err != nil || !strings.Contains(out, "p") {
		t.Errorf("cg active from X: %v\n%s", err, out)
	}
	if out, err := runSession(t, w, "", "active", "--slugs"); err != nil || strings.Contains(out, "p") {
		t.Errorf("cg active from W lists X's session: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, "", "pause", "--session-id", "s2"); err != nil || !strings.Contains(out, "Session paused: s2") {
		t.Errorf("pause from X: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, "", "release", "--session-id", "s2"); err != nil {
		t.Errorf("release from X: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(x, "s2"); c != nil {
		t.Errorf("release from X left X's state file: %+v", c)
	}

	wantRoots(t, x, w, x)
}

// S38c: inside one of W's linked worktrees the session commands still use W's
// state (its badge root is W's main checkout).
func TestSessionCommandsInSessionWorktree(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	wl := addBranchWorktree(t, w, "wl")
	t.Setenv(envWorkspace, w)
	if out, err := runSession(t, wl, "", "set-context", "--session-id", "s3", "--area", "a", "--project", "p"); err != nil {
		t.Fatalf("set-context: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s3"); c == nil || c.Project != "p" {
		t.Errorf("claim from W's worktree not in W's state file: %+v", c)
	}
}

// S38c/S35: a worktree created while in X doesn't enter W's record, and X's
// state file gets no record either (it isn't the session repo's worktree).
func TestWorktreeCreatedInAnotherWorkspaceLeavesRecords(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	own := addBranchWorktree(t, w, "own")
	if err := session.SetWorktree(w, "s4", recordPath(own)); err != nil {
		t.Fatal(err)
	}
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)
	t.Setenv("CG_SESSION_ID", "s4")
	if out, err := runCg(t, x, "worktree", "create", "xx"); err != nil {
		t.Fatalf("create in X: %v\n%s", err, out)
	}
	if got := recordedWorktree(t, w, "s4"); !samePath(got, own) {
		t.Errorf("W's record = %q, want %s", got, own)
	}
	if got := recordedWorktree(t, x, "s4"); got != "" {
		t.Errorf("X's state file records %q", got)
	}
}

// S38c/S38: a writer served from W while in X gets W's record, never a
// worktree recorded in X's state file.
func TestDispatchFromAnotherWorkspaceUsesSessionRecord(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome, []string{"alpha", "shop"})
	x := committedWorkspace(t, cfgHome)
	xwt := addBranchWorktree(t, x, "xw")
	if err := session.SetWorktree(x, "s5", recordPath(xwt)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, w)
	t.Setenv("CG_SESSION_ID", "s5")
	t.Chdir(x)
	if got := sessionWorktreeOf(t); got != "" {
		t.Errorf("no record in W: CG_SESSION_WORKTREE = %q, want unset", got)
	}
	wl := addBranchWorktree(t, w, "wl")
	if err := session.SetWorktree(w, "s5", recordPath(wl)); err != nil {
		t.Fatal(err)
	}
	t.Chdir(x)
	if got := sessionWorktreeOf(t); !samePath(got, wl) {
		t.Errorf("CG_SESSION_WORKTREE = %q, want W's record %s", got, wl)
	}
}

// S36: set-context --worktree records in the session repo root's state file
// wherever it runs, while --area and --project go to the session commands
// root's; one of X's own worktrees is refused.
func TestSetContextWorktreeFromAnotherWorkspace(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	wl := addBranchWorktree(t, w, "wl")
	x := committedWorkspace(t, cfgHome)
	xwt := addBranchWorktree(t, x, "xw")
	t.Setenv(envWorkspace, w)
	out, err := runCg(t, x, "session", "set-context", "--session-id", "s6", "--area", "a", "--project", "p", "--worktree", wl)
	if err != nil {
		t.Fatalf("set-context from X: %v\n%s", err, out)
	}
	if got := recordedWorktree(t, w, "s6"); !samePath(got, wl) {
		t.Errorf("W's record = %q, want %s", got, wl)
	}
	if c, _ := session.ReadContext(x, "s6"); c == nil || c.Project != "p" || c.Worktree != "" {
		t.Errorf("X's state file = %+v, want the claim and no worktree", c)
	}
	if c, _ := session.ReadContext(w, "s6"); c == nil || c.Project != "" {
		t.Errorf("W's state file = %+v, want the worktree and no claim", c)
	}
	out, err = runCg(t, x, "session", "set-context", "--session-id", "s7", "--worktree", xwt)
	if err == nil {
		t.Errorf("X's own worktree accepted:\n%s", out)
	}
	for _, root := range []string{w, x} {
		if got := recordedWorktree(t, root, "s7"); got != "" {
			t.Errorf("X's worktree recorded under %s: %q", root, got)
		}
	}
}
