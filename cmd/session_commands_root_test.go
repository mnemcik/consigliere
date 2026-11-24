package cmd

import (
	"os"
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

	if out, err := runSession(t, x, "", "set-context", "--session-id", "s2", "--area", "a", "--project", "xproj"); err != nil {
		t.Fatalf("set-context: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(x, "s2"); c == nil || c.Project != "xproj" {
		t.Errorf("claim from X not in X's state file: %+v", c)
	}
	if c, _ := session.ReadContext(w, "s2"); c != nil {
		t.Errorf("claim from X written to W's state file: %+v", c)
	}
	if out, err := runSession(t, x, "", "active", "--slugs"); err != nil || strings.TrimSpace(out) != "xproj" {
		t.Errorf("cg active from X = %q (%v), want xproj", out, err)
	}
	if out, err := runSession(t, w, "", "active", "--slugs"); err != nil || strings.Contains(out, "xproj") {
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

// end and mark-dirty, which S38c doesn't switch, reach the session's claim in
// either file: a claim made in W before moving to X is marked dirty from X and
// ended from X; the status line in X shows it.
func TestSessionHooksReachClaimInEitherFile(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)
	hook := func(id string) string {
		return `{"session_id":"` + id + `","cwd":"` + filepath.ToSlash(x) + `","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(w, "notes.md")) + `"}}`
	}

	// Claimed in W, then working from X.
	if out, err := runSession(t, w, "", "set-context", "--session-id", "s8", "--area", "a", "--project", "wproj"); err != nil {
		t.Fatalf("set-context in W: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, hook("s8"), "statusline"); err != nil || !strings.Contains(out, "wproj") {
		t.Errorf("status line from X = %q (%v), want W's claim", out, err)
	}
	if out, err := runSession(t, x, hook("s8"), "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty from X: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s8"); c == nil || !c.Dirty {
		t.Errorf("W's claim after mark-dirty from X: %+v, want dirty", c)
	}

	// Claimed in W, clean, ended from X: released.
	if out, err := runSession(t, w, "", "set-context", "--session-id", "s9", "--area", "a", "--project", "wproj"); err != nil {
		t.Fatalf("set-context in W: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, hook("s9"), "end"); err != nil {
		t.Fatalf("end from X: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s9"); c != nil {
		t.Errorf("W's clean claim survived end from X: %+v", c)
	}

	// Claimed in X: marked dirty in X's file.
	if out, err := runSession(t, x, "", "set-context", "--session-id", "s10", "--area", "a", "--project", "xproj"); err != nil {
		t.Fatalf("set-context in X: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, hook("s10"), "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty in X: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(x, "s10"); c == nil || !c.Dirty {
		t.Errorf("X's claim after mark-dirty: %+v, want dirty", c)
	}
}

// A checkout of the session's repository is recognised from its .git without
// running git (the hooks using the session commands root run often).
func TestSessionCommandsRootWithoutGit(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	wl := addBranchWorktree(t, w, "wl")
	t.Setenv(envWorkspace, w)
	t.Setenv("PATH", t.TempDir()) // no git from here on
	if got := sessionCommandsRoot(wl); !samePath(got, w) {
		t.Errorf("from W's linked worktree without git: %q, want %s", got, w)
	}
}

// With a claim in each file (made in W, then in X), mark-dirty marks both (the
// work may belong to either), so end keeps both; the status line shows X's,
// also when it has only an area, and W's when X's file can't be read.
func TestSessionHooksWithTwoClaims(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)
	stdin := `{"session_id":"s11","cwd":"` + filepath.ToSlash(x) + `","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(x, "notes.md")) + `"}}`
	if out, err := runSession(t, w, "", "set-context", "--session-id", "s11", "--area", "a", "--project", "wproj"); err != nil {
		t.Fatalf("set-context in W: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, "", "set-context", "--session-id", "s11", "--area", "a", "--project", "xproj"); err != nil {
		t.Fatalf("set-context in X: %v\n%s", err, out)
	}
	if out, err := runSession(t, x, stdin, "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(x, "s11"); c == nil || !c.Dirty {
		t.Errorf("X's current claim: %+v, want dirty", c)
	}
	if c, _ := session.ReadContext(w, "s11"); c == nil || !c.Dirty {
		t.Errorf("W's older claim: %+v, want dirty too", c)
	}
	if out, err := runSession(t, x, stdin, "statusline"); err != nil || !strings.Contains(out, "xproj") || strings.Contains(out, "wproj") {
		t.Errorf("status line = %q (%v), want X's claim", out, err)
	}
	if out, err := runSession(t, x, stdin, "end"); err != nil {
		t.Fatalf("end: %v\n%s", err, out)
	}
	for _, root := range []string{w, x} {
		if c, _ := session.ReadContext(root, "s11"); c == nil {
			t.Errorf("a dirty claim under %s released by end", root)
		}
	}

	// W's claim is marked also when X's file for the session holds no claim
	// (both files are marked).
	if err := session.WriteContext(w, "s13", session.Claim{Area: "a", Project: "wproj"}); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, x, "s13", `{}`)
	mark := `{"session_id":"s13","cwd":"` + filepath.ToSlash(x) + `","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(x, "notes.md")) + `"}}`
	if out, err := runSession(t, x, mark, "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s13"); c == nil || !c.Dirty {
		t.Errorf("W's claim with an empty X file: %+v, want dirty", c)
	}

	// X's file can't be read: W's claim is still marked and shown.
	if err := session.WriteContext(w, "s15", session.Claim{Area: "a", Project: "wproj"}); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, x, "s15", `{"area":1}`)
	bad := `{"session_id":"s15","cwd":"` + filepath.ToSlash(x) + `","tool_input":{"file_path":"` + filepath.ToSlash(filepath.Join(x, "notes.md")) + `"}}`
	if out, err := runSession(t, x, bad, "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty with an unreadable X file: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s15"); c == nil || !c.Dirty {
		t.Errorf("W's claim with an unreadable X file: %+v, want dirty", c)
	}
	if out, err := runSession(t, x, bad, "statusline"); err != nil || !strings.Contains(out, "wproj") {
		t.Errorf("status line with an unreadable X file = %q (%v), want W's claim", out, err)
	}

	// An area-only state in X is a badge too: shown instead of W's.
	if err := session.WriteContext(w, "s12", session.Claim{Area: "a", Project: "wproj"}); err != nil {
		t.Fatal(err)
	}
	writeStateFile(t, x, "s12", `{"area":"xarea"}`)
	in := `{"session_id":"s12","cwd":"` + filepath.ToSlash(x) + `"}`
	if out, err := runSession(t, x, in, "statusline"); err != nil || !strings.Contains(out, "xarea") || strings.Contains(out, "wproj") {
		t.Errorf("status line with X's area-only state = %q (%v), want X's", out, err)
	}
}

// writeStateFile writes a session state file under root as is.
func writeStateFile(t *testing.T, root, id, content string) {
	t.Helper()
	if err := os.MkdirAll(session.ContextDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.ContextFile(root, id), []byte(content), 0o644); err != nil { //nolint:gosec // a test fixture
		t.Fatal(err)
	}
}

// end and mark-dirty skip a root where the session has no state file: no git
// calls or lock wait there (SessionEnd's budget is short), and no lock file
// left behind.
func TestSessionEndSkipsRootWithoutFile(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	w := committedWorkspace(t, cfgHome)
	x := committedWorkspace(t, cfgHome)
	t.Setenv(envWorkspace, w)
	writeStateFile(t, x, "other", `{"area":"a","project":"o"}`) // X's state directory exists
	if err := session.WriteContext(w, "s14", session.Claim{Area: "a", Project: "wproj"}); err != nil {
		t.Fatal(err)
	}
	in := `{"session_id":"s14","cwd":"` + filepath.ToSlash(x) + `"}`
	if out, err := runSession(t, x, in, "mark-dirty"); err != nil {
		t.Fatalf("mark-dirty: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s14"); c == nil || !c.Dirty {
		t.Fatalf("W's claim after mark-dirty: %+v, want dirty", c)
	}
	writeStateFile(t, w, "s14", `{"area":"a","project":"wproj"}`) // clean again
	if out, err := runSession(t, x, in, "end"); err != nil {
		t.Fatalf("end: %v\n%s", err, out)
	}
	if c, _ := session.ReadContext(w, "s14"); c != nil {
		t.Errorf("W's clean claim survived end: %+v", c)
	}
	entries, _ := os.ReadDir(session.ContextDir(x))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "s14") {
			t.Errorf("end left %s in X", e.Name())
		}
	}
}
