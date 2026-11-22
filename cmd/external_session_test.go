package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// gitCommitAll makes dir a git repo (if it isn't one) and commits everything
// in it.
func gitCommitAll(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		for _, args := range [][]string{
			{"init", "--quiet"},
			{"config", "user.email", "t@example.com"},
			{"config", "user.name", "test"},
		} {
			if _, err := gitx.Run(ctx, dir, args...); err != nil {
				t.Fatalf("git %v: %v", args, err)
			}
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "--allow-empty", "-m", "snapshot"}} {
		if _, err := gitx.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
}

// addBranchWorktree adds a linked worktree of repo on a new branch at a fresh path and returns it.
func addBranchWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), branch)
	if _, err := gitx.Run(context.Background(), repo, "worktree", "add", "--quiet", "-b", branch, wt); err != nil {
		t.Fatal(err)
	}
	return wt
}

// sessionWorkspace is a git-tracked workspace with extensions installed, the
// session's workspace in tests below.
func sessionWorkspace(t *testing.T, cfgHome string, exts ...[]string) string {
	t.Helper()
	ws := newWorkspace(t)
	for _, e := range exts {
		installRepo(t, cfgHome, ws, commandsManifest(e[0], "1.0.0", e[1:]...))
	}
	gitCommitAll(t, ws)
	return ws
}

// otherWorkspace is a workspace with its own extensions, e.g. a cloned repo's.
func otherWorkspace(t *testing.T, cfgHome string, exts ...[]string) string {
	t.Helper()
	ws := newWorkspace(t)
	for _, e := range exts {
		installRepo(t, cfgHome, ws, commandsManifest(e[0], "1.0.0", e[1:]...))
	}
	return ws
}

// samePaths compares root lists after resolving symlinks.
func samePaths(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !samePath(got[i], want[i]) {
			return false
		}
	}
	return true
}

func wantRoots(t *testing.T, cwd string, want ...string) {
	t.Helper()
	t.Chdir(cwd)
	got, err := consultedRoots()
	if err != nil {
		t.Fatalf("consultedRoots from %s: %v", cwd, err)
	}
	if !samePaths(got, want) {
		t.Errorf("consultedRoots from %s = %q, want %q", cwd, got, want)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

// S19–S24: which workspaces are consulted, and in what order.
func TestConsultedRoots(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome, []string{"alpha", "shop"})
	wt := addBranchWorktree(t, session, "feature")
	other := otherWorkspace(t, cfgHome)
	outside := t.TempDir()
	sub := filepath.Join(session, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("S19 unset", func(t *testing.T) {
		unsetEnv(t, envWorkspace)
		wantRoots(t, other, other)
		wantRoots(t, outside)
	})
	t.Run("S20 cwd root is the session root", func(t *testing.T) {
		t.Setenv(envWorkspace, session)
		wantRoots(t, sub, session)
	})
	t.Run("S21 cwd root is a worktree of the session repo", func(t *testing.T) {
		t.Setenv(envWorkspace, session)
		wantRoots(t, wt, wt)
	})
	t.Run("S21 from the session's worktree back to the main checkout", func(t *testing.T) {
		t.Setenv(envWorkspace, wt)
		wantRoots(t, session, session)
	})
	t.Run("S22 another workspace", func(t *testing.T) {
		t.Setenv(envWorkspace, session)
		wantRoots(t, other, session, other)
	})
	t.Run("S24 outside any workspace", func(t *testing.T) {
		t.Setenv(envWorkspace, session)
		wantRoots(t, outside, session)
	})
}

// S23: when the worktree check can't run or can't be trusted, the cwd root
// counts as another workspace.
func TestConsultedRootsWorktreeCheckFailsClosed(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	t.Run("session root is not a git repo", func(t *testing.T) {
		session := otherWorkspace(t, cfgHome)
		other := otherWorkspace(t, cfgHome)
		t.Setenv(envWorkspace, session)
		wantRoots(t, other, session, other)
	})
	t.Run("session root is not the top level of its repo", func(t *testing.T) {
		// The repo's top level and a worktree of it are workspaces; the
		// session's workspace is a subdirectory of the repo.
		repo := newWorkspace(t)
		session := filepath.Join(repo, "inner")
		if err := os.Mkdir(session, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := (&workspace.Config{Type: workspace.TypeConsigliere, Version: "1.3.0"}).Save(session); err != nil {
			t.Fatal(err)
		}
		gitCommitAll(t, repo)
		wt := addBranchWorktree(t, repo, "x")
		t.Setenv(envWorkspace, session)
		wantRoots(t, wt, session, wt)
	})
	t.Run("git missing", func(t *testing.T) {
		session := sessionWorkspace(t, cfgHome)
		wt := addBranchWorktree(t, session, "y")
		t.Setenv(envWorkspace, session)
		t.Setenv("PATH", t.TempDir())
		wantRoots(t, wt, session, wt)
	})
}

// S25: a relative or invalid CG_WORKSPACE is ignored, and the hint says why.
func TestConsultedRootsIgnoresInvalidSessionRoot(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	other := newWorkspace(t)
	notWS := t.TempDir()
	for _, tc := range []struct{ value, why string }{
		{"relative/ws", "not an absolute path"},
		{filepath.Join(notWS, "gone"), "no valid .cg.json"},
		{notWS, "no valid .cg.json"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(envWorkspace, tc.value)
			wantRoots(t, other, other)
			h := unknownCommandHint(errUnknownForTest)
			if !strings.Contains(h, "ignored") || !strings.Contains(h, tc.why) {
				t.Errorf("hint %q doesn't say CG_WORKSPACE was ignored because %q", h, tc.why)
			}
		})
	}
}

// S26: a checkout with a planted .cg.json and a .git file pointing into the
// session's repo still counts as another workspace.
func TestConsultedRootsPlantedGitFile(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome)
	wt := addBranchWorktree(t, session, "real")
	planted := newWorkspace(t)
	// The .git file of the real worktree, copied: git run inside planted would
	// take it for that worktree.
	gitFile, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, ".git"), gitFile, 0o644); err != nil { //nolint:gosec // a test fixture in its own temp dir
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, session)
	wantRoots(t, planted, session, planted)

	// A real worktree of another repo is listed by that repo, not by the
	// session's: still another workspace.
	otherRepo := newWorkspace(t)
	gitCommitAll(t, otherRepo)
	foreign := addBranchWorktree(t, otherRepo, "foreign")
	wantRoots(t, foreign, session, foreign)
}

// S27: git's repository variables in the caller's environment don't steer the
// worktree check.
func TestConsultedRootsIgnoresGitEnvironment(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome)
	wt := addBranchWorktree(t, session, "env")
	bogus := filepath.Join(t.TempDir(), "nope")
	for _, v := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envWorkspace, session)
			t.Setenv(v, bogus)
			wantRoots(t, wt, wt)
		})
	}
}

// S22 through dispatch: the session's declared commands win from another
// workspace, and a namespace the session doesn't declare falls to the cwd's.
func TestDispatchPrefersSessionWorkspace(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome, []string{"alpha", "shop", "shop add"})
	clone := otherWorkspace(t, cfgHome, []string{"beta", "shop", "shop add"}, []string{"gamma", "pr"})
	t.Setenv(envWorkspace, session)
	t.Chdir(clone)
	if run := mustDispatch(t, "shop", "add"); run.root != session || run.path != binOf(t, session, "alpha") {
		t.Errorf("shop add = %+v, want the session's alpha", run)
	}
	if run := mustDispatch(t, "pr"); run.root != clone || run.path != binOf(t, clone, "gamma") {
		t.Errorf("pr = %+v, want the clone's gamma", run)
	}
	// S24: from outside any workspace, the session serves.
	t.Chdir(t.TempDir())
	if run := mustDispatch(t, "shop"); run.root != session {
		t.Errorf("shop from outside = %+v, want the session", run)
	}
}

// Q9 with S16/S17 across roots: once the session declares a namespace, a
// clone's verified command in it can't answer, even typed inside the clone.
func TestSessionNamespaceBlocksClone(t *testing.T) {
	requireGit(t)
	t.Run("S17 unverified session entry", func(t *testing.T) {
		cfgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", cfgHome)
		session := sessionWorkspace(t, cfgHome, []string{"alpha", "shop"})
		if err := os.Remove(binOf(t, session, "alpha")); err != nil {
			t.Fatal(err)
		}
		clone := otherWorkspace(t, cfgHome, []string{"beta", "shop"})
		t.Setenv(envWorkspace, session)
		t.Chdir(clone)
		pathProgram(t, "shop")
		dispatchErr(t, []string{"shop"}, `"alpha"`, "cg init")
	})
	t.Run("S16 session declares only another command", func(t *testing.T) {
		cfgHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", cfgHome)
		session := sessionWorkspace(t, cfgHome, []string{"alpha", "shop add"})
		clone := otherWorkspace(t, cfgHome, []string{"beta", "shop list"})
		t.Setenv(envWorkspace, session)
		t.Chdir(clone)
		dispatchErr(t, []string{"shop", "list"}, "cg shop add")
	})
}

// S28, S29: the binary is told the root that served it; CG_WORKSPACE passes
// through; a PATH program is told neither root nor session worktree.
func TestExternalCommandWorkspaceEnvironment(t *testing.T) {
	t.Setenv(envWorkspace, "/session")
	t.Setenv(envExtensionWorkspace, "/stale")
	t.Setenv(envSessionWorktree, "/stale-wt")
	served := (&externalRun{path: "/x/cg-live", command: "live", root: "/ws"}).cmd()
	if v, _ := envOf(served.Env, envExtensionWorkspace); v != "/ws" {
		t.Errorf("served CG_EXTENSION_WORKSPACE = %q, want /ws", v)
	}
	if v, _ := envOf(served.Env, envWorkspace); v != "/session" {
		t.Errorf("served CG_WORKSPACE = %q, want it passed through", v)
	}
	fallback := (&externalRun{path: "/x/cg-greet"}).cmd()
	for _, k := range []string{envExtensionWorkspace, envSessionWorktree, envCommand} {
		if _, ok := envOf(fallback.Env, k); ok {
			t.Errorf("a PATH program must not see %s", k)
		}
	}
	if v, _ := envOf(fallback.Env, envWorkspace); v != "/session" {
		t.Errorf("fallback CG_WORKSPACE = %q, want it passed through", v)
	}
}

// S31: cg start sets CG_WORKSPACE to the absolute root it resolved (a worktree
// when started in one) and removes the per-run variables, for the agent and
// for a launcher.
func TestStartSetsSessionWorkspace(t *testing.T) {
	requireGit(t)
	for _, withLauncher := range []bool{false, true} {
		t.Run(map[bool]string{false: "agent", true: "launcher"}[withLauncher], func(t *testing.T) {
			ws, _ := launcherWorkspace(t)
			if withLauncher {
				installLauncher(t, ws, "tmux", "cg-tmux", "cg-tmux", nil, true)
			}
			gitCommitAll(t, ws)
			wt := addBranchWorktree(t, ws, "start")
			t.Chdir(wt)
			t.Setenv(envWorkspace, "/elsewhere")
			for _, k := range []string{envCommand, envExtensionWorkspace, envSessionWorktree} {
				t.Setenv(k, "stale")
			}
			s := newStubStart(t, true)
			if err := startCmd.RunE(startCmd, nil); err != nil {
				t.Fatal(err)
			}
			if v, _ := envOf(s.env, envWorkspace); !filepath.IsAbs(v) || !samePath(v, wt) {
				t.Errorf("CG_WORKSPACE = %q, want the worktree %s", v, wt)
			}
			for _, k := range []string{envCommand, envExtensionWorkspace, envSessionWorktree} {
				if _, ok := envOf(s.env, k); ok {
					t.Errorf("%s must be removed from the session's environment", k)
				}
			}
			if n := countEnv(s.env, envWorkspace); n != 1 {
				t.Errorf("CG_WORKSPACE appears %d times", n)
			}
		})
	}
}

func countEnv(env []string, key string) int {
	n := 0
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); envKeyEqual(k, key) {
			n++
		}
	}
	return n
}

// The per-run variables never steer which workspace serves (S30), now that
// more than one root can be consulted.
func TestConsultedRootsIgnorePerRunVariables(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome)
	other := otherWorkspace(t, cfgHome)
	unsetEnv(t, envWorkspace)
	for _, k := range []string{envExtensionWorkspace, envSessionWorktree} {
		t.Setenv(k, session)
	}
	wantRoots(t, other, other)
}

var errUnknownForTest = errors.New(`unknown command "frob" for "cg"`)

// samePath compares the directories themselves: a different spelling of the
// same directory (case on a case-insensitive filesystem, a symlink) matches.
func TestSamePathSameDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "CaseDir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !samePath(dir, dir+string(filepath.Separator)) {
		t.Error("a trailing separator must not matter")
	}
	upper := filepath.Join(filepath.Dir(dir), "CASEDIR")
	if _, err := os.Stat(upper); err == nil && !samePath(dir, upper) {
		t.Errorf("case-insensitive filesystem: %s and %s are one directory", dir, upper)
	}
	if samePath(dir, filepath.Join(filepath.Dir(dir), "nope")) {
		t.Error("a missing directory matches nothing")
	}
}

// A listed worktree path now holding another repository (the worktree was
// deleted without `git worktree remove` and something else put there) is not
// the session's worktree.
func TestConsultedRootsReplacedWorktree(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome)
	wt := addBranchWorktree(t, session, "gone")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&workspace.Config{Type: workspace.TypeConsigliere, Version: "1.3.0"}).Save(wt); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, wt)
	t.Setenv(envWorkspace, session)
	wantRoots(t, wt, session, wt)

	// Planted instead: the .git file of another, live worktree of the session
	// repo, copied to where the deleted one was.
	other := addBranchWorktree(t, session, "live")
	planted := addBranchWorktree(t, session, "planted")
	if err := os.RemoveAll(planted); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&workspace.Config{Type: workspace.TypeConsigliere, Version: "1.3.0"}).Save(planted); err != nil {
		t.Fatal(err)
	}
	gitFile, err := os.ReadFile(filepath.Join(other, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, ".git"), gitFile, 0o644); err != nil { //nolint:gosec // a test fixture in its own temp dir
		t.Fatal(err)
	}
	wantRoots(t, planted, session, planted)
}

// A worktree path ending in a space is matched exactly.
func TestConsultedRootsWorktreePathWithTrailingSpace(t *testing.T) {
	requireGit(t)
	if isWindowsOS {
		t.Skip("Windows paths can't end in a space")
	}
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome)
	wt := filepath.Join(t.TempDir(), "spaced ")
	if _, err := gitx.Run(context.Background(), session, "worktree", "add", "--quiet", "-b", "spaced", wt); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, session)
	wantRoots(t, wt, wt)
}

// An unreadable .cg.json in the current directory's tree doesn't stop a
// command the session's workspace declares; it stops one the session doesn't
// decide.
func TestUnreadableCwdConfigWithSession(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	session := sessionWorkspace(t, cfgHome, []string{"alpha", "shop"})
	ext := t.TempDir()
	if err := os.WriteFile(filepath.Join(ext, workspace.ConfigFile), []byte(`{"version": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWorkspace, session)
	t.Chdir(ext)
	if run := mustDispatch(t, "shop"); run.root != session {
		t.Errorf("shop = %+v, want the session's", run)
	}
	dispatchErr(t, []string{"pr"}, filepath.Join(ext, workspace.ConfigFile))
}

// A workspace found by its folder markers alone has no .cg.json for dispatch
// to use: cg start doesn't put it in CG_WORKSPACE, and removes an inherited
// one.
func TestStartLegacyWorkspaceSetsNoSessionWorkspace(t *testing.T) {
	ws := t.TempDir()
	for _, f := range []string{"CLAUDE.md", filepath.Join("areas", "INDEX.md"), filepath.Join("projects", "TODO.md")} {
		p := filepath.Join(ws, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(ws)
	t.Setenv(envWorkspace, "/elsewhere")
	// The default agent, `claude`, as a copy of the test binary on PATH.
	bin := t.TempDir()
	name := workspace.DefaultAgentCommand
	if isWindowsOS {
		name += ".exe"
	}
	data, err := os.ReadFile(testAgent(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0o755); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	s := newStubStart(t, true)
	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatalf("legacy workspace start: %v", err)
	}
	if v, ok := envOf(s.env, envWorkspace); ok {
		t.Errorf("CG_WORKSPACE=%q set for a workspace without .cg.json", v)
	}
}

// The hint tells an empty CG_WORKSPACE from an unset one.
func TestHintEmptySessionWorkspace(t *testing.T) {
	t.Setenv(envWorkspace, "")
	if h := unknownCommandHint(errUnknownForTest); !strings.Contains(h, "ignored") || !strings.Contains(h, "empty") {
		t.Errorf("hint %q", h)
	}
}
