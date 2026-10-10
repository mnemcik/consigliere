package githooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// TestMain isolates the tests from the developer's git config: a global
// core.hooksPath or init.templateDir would change what they see.
func TestMain(m *testing.M) {
	_ = os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func repo(t *testing.T) (ctx context.Context, root string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks are not exercised on windows")
	}
	ctx = context.Background()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	root = filepath.Join(dir, "ws")
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", root},
		{"-C", root, "config", "user.email", "t@example.com"},
		{"-C", root, "config", "user.name", "T"},
		{"-C", root, "config", "commit.gpgsign", "false"},
		{"-C", root, "commit", "--quiet", "--allow-empty", "-m", "init"},
	} {
		if _, err := gitx.Run(ctx, "", args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	return ctx, root
}

func git(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(ctx, dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

// script writes an executable shell script that appends its name, its
// arguments and its stdin to log.
func script(t *testing.T, root, hook, name, log string) {
	t.Helper()
	dir := filepath.Join(root, DirRel, hook+".d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\n{ echo \"" + name + " $*\"; cat; } >> \"" + log + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path) //nolint:gosec // test file
	return string(b)
}

func hookPath(t *testing.T, ctx context.Context, root, hook string) string {
	t.Helper()
	dir, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, hook)
}

// trustAll approves every script currently under root/.cg/git-hooks, as if
// each matched an installed extension's clone.
func trustAll(t *testing.T, ctx context.Context, root string) map[string]string {
	t.Helper()
	trusted := map[string]string{}
	for _, h := range Hooks {
		rels, err := scripts(root, h)
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range rels {
			oid, err := BlobID(ctx, root, filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			trusted[rel] = oid
		}
	}
	return trusted
}

// ensure runs Ensure trusting every current script and fails on error.
func ensure(t *testing.T, ctx context.Context, root string) Result {
	t.Helper()
	res, err := Ensure(ctx, root, trustAll(t, ctx, root))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestEnsureNothingToDo(t *testing.T) {
	ctx, root := repo(t)
	ensure(t, ctx, root)
	if _, err := os.Stat(hookPath(t, ctx, root, "post-commit")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dispatcher installed with no scripts: %v", err)
	}
}

// A contributed post-commit script runs on a commit in the main checkout and
// in a linked worktree; removing it removes the dispatcher.
func TestEnsureRunsScriptsFromEveryWorktree(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-commit", "a", log)
	ensure(t, ctx, root)
	ensure(t, ctx, root) // idempotent
	git(t, ctx, root, "commit", "--quiet", "--allow-empty", "-m", "one")
	wt := filepath.Join(filepath.Dir(root), "ws--x")
	git(t, ctx, root, "worktree", "add", "--quiet", "-b", "session/x", wt)
	git(t, ctx, wt, "commit", "--quiet", "--allow-empty", "-m", "two")
	if got := strings.Count(read(t, log), "a "); got != 2 {
		t.Errorf("script ran %d times, want 2:\n%s", got, read(t, log))
	}

	if err := os.RemoveAll(filepath.Join(root, DirRel)); err != nil {
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	if _, err := os.Stat(hookPath(t, ctx, root, "post-commit")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dispatcher left behind: %v", err)
	}
}

// Every script gets the hook's stdin, not just the first one to read it.
func TestDispatcherGivesStdinToEveryScript(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-rewrite", "a", log)
	script(t, root, "post-rewrite", "b", log)
	ensure(t, ctx, root)
	old, _ := gitx.RevParse(ctx, root, "HEAD")
	git(t, ctx, root, "commit", "--quiet", "--amend", "--allow-empty", "-m", "amended")
	got := read(t, log)
	if strings.Count(got, old) != 2 || !strings.Contains(got, "a amend") || !strings.Contains(got, "b amend") {
		t.Errorf("not every script saw the arguments and stdin:\n%s", got)
	}
}

// A hook that was there first is kept, runs before the scripts, and comes
// back when the dispatcher goes.
func TestEnsureChainsAndRestoresForeignHook(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	own := hookPath(t, ctx, root, "post-commit")
	body := "#!/bin/sh\necho mine >> \"" + log + "\"\n"
	if err := os.WriteFile(own, []byte(body), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}
	script(t, root, "post-commit", "a", log)
	ensure(t, ctx, root)
	git(t, ctx, root, "commit", "--quiet", "--allow-empty", "-m", "one")
	if got := read(t, log); !strings.HasPrefix(got, "mine\na ") {
		t.Errorf("foreign hook did not run first:\n%s", got)
	}

	if err := os.RemoveAll(filepath.Join(root, DirRel)); err != nil {
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	if read(t, own) != body {
		t.Errorf("foreign hook not restored: %q", read(t, own))
	}
	if _, err := os.Stat(own + preSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s left behind", preSuffix)
	}
}

// A failing pre-commit script stops the commit.
func TestDispatcherPropagatesFailure(t *testing.T) {
	ctx, root := repo(t)
	dir := filepath.Join(root, DirRel, "pre-commit.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "no"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	if _, err := gitx.Run(ctx, root, "commit", "--allow-empty", "-m", "blocked"); err == nil {
		t.Error("commit succeeded despite a failing pre-commit script")
	}
}

func TestEnsureRefusesWithHooksPath(t *testing.T) {
	ctx, root := repo(t)
	git(t, ctx, root, "config", "core.hooksPath", filepath.Join(root, "myhooks"))
	if _, err := Ensure(ctx, root, nil); err != nil {
		t.Errorf("no scripts: want nil, got %v", err)
	}
	script(t, root, "post-commit", "a", filepath.Join(t.TempDir(), "log"))
	if _, err := Ensure(ctx, root, trustAll(t, ctx, root)); !errors.Is(err, ErrHooksPath) {
		t.Errorf("want ErrHooksPath, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "myhooks")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote into core.hooksPath")
	}
}

// A script that does not match an installed extension never runs: Ensure
// reports it and installs no dispatcher for it, and one that arrives later
// (by pull or checkout) under a hook that has a dispatcher is skipped with a
// warning, as is an approved script whose content changed.
func TestUnapprovedScriptsDoNotRun(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-merge", "evil", log)
	res, err := Ensure(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unapproved) != 1 {
		t.Errorf("unapproved = %v, want the one script", res.Unapproved)
	}
	if exists(hookPath(t, ctx, root, "post-merge")) {
		t.Error("dispatcher installed for an unapproved script")
	}

	_ = os.Remove(filepath.Join(root, DirRel, "post-merge.d", "evil"))
	script(t, root, "post-commit", "good", log)
	ensure(t, ctx, root)
	script(t, root, "post-commit", "arrived", log) // after approval
	good := filepath.Join(root, DirRel, "post-commit.d", "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\necho tampered >> \""+log+"\"\n"), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}
	out, err := gitx.Run(ctx, root, "commit", "--allow-empty", "-m", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, log); got != "" {
		t.Errorf("an unapproved or changed script ran:\n%s", got)
	}
	_ = out
}

// Several sessions starting at once run Ensure concurrently; a user's own
// hook must survive every interleaving.
func TestConcurrentEnsureKeepsForeignHook(t *testing.T) {
	ctx, root := repo(t)
	own := hookPath(t, ctx, root, "post-commit")
	body := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(own, []byte(body), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}
	script(t, root, "post-commit", "a", filepath.Join(t.TempDir(), "log"))
	trusted := trustAll(t, ctx, root)
	for round := 0; round < 25; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = Ensure(ctx, root, trusted)
			}()
		}
		wg.Wait()
		if read(t, own+preSuffix) != body {
			t.Fatalf("round %d: user hook lost; %s holds %q", round, preSuffix, read(t, own+preSuffix))
		}
		if !isDispatcher(own) {
			t.Fatalf("round %d: no dispatcher installed", round)
		}
	}
}

// Ensure run from a linked worktree whose branch lacks the scripts works on
// the main worktree, so it keeps the dispatcher main needs.
func TestEnsureFromWorktreeUsesMain(t *testing.T) {
	ctx, root := repo(t)
	script(t, root, "post-commit", "a", filepath.Join(t.TempDir(), "log"))
	trusted := trustAll(t, ctx, root)
	wt := filepath.Join(filepath.Dir(root), "ws--y")
	git(t, ctx, root, "worktree", "add", "--quiet", "-b", "session/y", wt)
	if _, err := Ensure(ctx, wt, trusted); err != nil {
		t.Fatal(err)
	}
	if !isDispatcher(hookPath(t, ctx, root, "post-commit")) {
		t.Error("Ensure from a worktree removed main's dispatcher")
	}
}

// A script without the execute bit still runs, through sh.
func TestNonExecutableScriptRuns(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-commit", "a", log)
	if err := os.Chmod(filepath.Join(root, DirRel, "post-commit.d", "a"), 0o644); err != nil { //nolint:gosec // a readable, non-executable script is the case under test
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	git(t, ctx, root, "commit", "--quiet", "--allow-empty", "-m", "x")
	if !strings.Contains(read(t, log), "a ") {
		t.Errorf("non-executable script did not run")
	}
}

// A dangling symlink hook is kept and restored, not overwritten.
func TestDanglingSymlinkHookKept(t *testing.T) {
	ctx, root := repo(t)
	own := hookPath(t, ctx, root, "post-commit")
	if err := os.Symlink("/nonexistent/hook", own); err != nil {
		t.Fatal(err)
	}
	script(t, root, "post-commit", "a", filepath.Join(t.TempDir(), "log"))
	ensure(t, ctx, root)
	if target, err := os.Readlink(own + preSuffix); err != nil || target != "/nonexistent/hook" {
		t.Errorf("symlink hook not moved aside: %q %v", target, err)
	}
	if err := os.RemoveAll(filepath.Join(root, DirRel)); err != nil {
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	if target, err := os.Readlink(own); err != nil || target != "/nonexistent/hook" {
		t.Errorf("symlink hook not restored: %q %v", target, err)
	}
}

// With the git dir outside the workspace (git init --separate-git-dir), the
// dispatcher still finds the scripts: the root comes from the allow file.
func TestSeparateGitDir(t *testing.T) {
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("git shell hooks not available")
	}
	ctx := context.Background()
	base := t.TempDir()
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	root := filepath.Join(base, "ws")
	gd := filepath.Join(base, "elsewhere.git")
	git(t, ctx, "", "init", "--quiet", "--initial-branch=main", "--separate-git-dir", gd, root)
	git(t, ctx, root, "config", "user.email", "t@example.com")
	git(t, ctx, root, "config", "user.name", "T")
	git(t, ctx, root, "config", "commit.gpgsign", "false")
	log := filepath.Join(base, "log")
	script(t, root, "post-commit", "a", log)
	if _, err := Ensure(ctx, root, trustAll(t, ctx, root)); err != nil {
		t.Fatal(err)
	}
	git(t, ctx, root, "commit", "--quiet", "--allow-empty", "-m", "x")
	if !strings.Contains(read(t, log), "a ") {
		t.Errorf("script did not run with a separate git dir")
	}
}

// The main worktree moved since Ensure wrote the allow file: the dispatcher
// finds the scripts at the new location instead of skipping them.
func TestDispatcherFollowsMovedWorktree(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-commit", "a", log)
	ensure(t, ctx, root)
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	git(t, ctx, moved, "commit", "--quiet", "--allow-empty", "-m", "after move")
	if !strings.Contains(read(t, log), "a ") {
		t.Errorf("script did not run after the worktree moved")
	}
}

// Ensure runs at every session start, so an unchanged approved list is not
// rewritten; a changed one is.
func TestEnsureLeavesUnchangedAllowFile(t *testing.T) {
	ctx, root := repo(t)
	log := filepath.Join(t.TempDir(), "log")
	script(t, root, "post-commit", "a", log)
	ensure(t, ctx, root)
	common, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	allow := filepath.Join(common, AllowFile)
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(allow, past, past); err != nil {
		t.Fatal(err)
	}
	ensure(t, ctx, root)
	if info, err := os.Stat(allow); err != nil || !info.ModTime().Equal(past) {
		t.Errorf("unchanged allow file was rewritten: %v", err)
	}
	script(t, root, "post-commit", "b", log)
	ensure(t, ctx, root)
	if !strings.Contains(read(t, allow), ".cg/git-hooks/post-commit.d/b") {
		t.Errorf("allow file not updated for a new script:\n%s", read(t, allow))
	}
}
