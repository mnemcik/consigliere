package githooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

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
	dir, err := hooksDir(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, hook)
}

func TestEnsureNothingToDo(t *testing.T) {
	ctx, root := repo(t)
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
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
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(ctx, root); err != nil { // idempotent
		t.Fatal(err)
	}
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
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
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
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
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
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
	git(t, ctx, root, "commit", "--quiet", "--allow-empty", "-m", "one")
	if got := read(t, log); !strings.HasPrefix(got, "mine\na ") {
		t.Errorf("foreign hook did not run first:\n%s", got)
	}

	if err := os.RemoveAll(filepath.Join(root, DirRel)); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
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
	if err := Ensure(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, root, "commit", "--allow-empty", "-m", "blocked"); err == nil {
		t.Error("commit succeeded despite a failing pre-commit script")
	}
}

func TestEnsureRefusesWithHooksPath(t *testing.T) {
	ctx, root := repo(t)
	git(t, ctx, root, "config", "core.hooksPath", filepath.Join(root, "myhooks"))
	if err := Ensure(ctx, root); err != nil {
		t.Errorf("no scripts: want nil, got %v", err)
	}
	script(t, root, "post-commit", "a", filepath.Join(t.TempDir(), "log"))
	if err := Ensure(ctx, root); !errors.Is(err, ErrHooksPath) {
		t.Errorf("want ErrHooksPath, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "myhooks")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wrote into core.hooksPath")
	}
}
