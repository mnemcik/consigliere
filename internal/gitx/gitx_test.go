package gitx

import (
	"context"
	"path/filepath"
	"testing"
)

// initRepo creates a git repo at a temp dir with one commit on branch "main"
// and returns its (symlink-resolved) path. It skips the test if git is missing
// or the platform is one we don't exercise git integration on.
func initRepo(t *testing.T) (ctx context.Context, dir string) {
	t.Helper()
	if !Available() {
		t.Skip("git not available on PATH")
	}
	ctx = context.Background()
	dir = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	mustRun(t, ctx, dir, "init", "--initial-branch=main")
	mustRun(t, ctx, dir, "config", "user.email", "test@example.com")
	mustRun(t, ctx, dir, "config", "user.name", "Test")
	mustRun(t, ctx, dir, "config", "commit.gpgsign", "false")
	mustRun(t, ctx, dir, "commit", "--allow-empty", "-m", "init")
	return ctx, dir
}

func mustRun(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	out, err := Run(ctx, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestRunError(t *testing.T) {
	if !Available() {
		t.Skip("git not available")
	}
	if _, err := Run(context.Background(), t.TempDir(), "rev-parse", "HEAD"); err == nil {
		t.Error("expected error running git in a non-repo dir")
	}
}

func TestCommonRoot(t *testing.T) {
	ctx, dir := initRepo(t)
	root, err := CommonRoot(ctx, dir)
	if err != nil {
		t.Fatalf("CommonRoot: %v", err)
	}
	if resolved, _ := filepath.EvalSymlinks(root); resolved != dir {
		t.Errorf("CommonRoot = %q, want %q", root, dir)
	}
}

func TestRefExistsAndAncestor(t *testing.T) {
	ctx, dir := initRepo(t)
	if !RefExists(ctx, dir, "refs/heads/main") {
		t.Error("expected refs/heads/main to exist")
	}
	if RefExists(ctx, dir, "refs/heads/nope") {
		t.Error("did not expect refs/heads/nope to exist")
	}
	if !IsAncestor(ctx, dir, "HEAD", "HEAD") {
		t.Error("HEAD should be an ancestor of itself")
	}
}

func TestWorktreePaths(t *testing.T) {
	ctx, dir := initRepo(t)
	paths, err := WorktreePaths(ctx, dir)
	if err != nil {
		t.Fatalf("WorktreePaths: %v", err)
	}
	found := false
	for _, p := range paths {
		if resolved, _ := filepath.EvalSymlinks(p); resolved == dir {
			found = true
		}
	}
	if !found {
		t.Errorf("WorktreePaths %v did not include the main worktree %q", paths, dir)
	}
}

func TestWorktreeAddNew(t *testing.T) {
	ctx, dir := initRepo(t)
	wt := dir + "--feature"
	if err := WorktreeAddNew(ctx, dir, wt, "session/feature", "HEAD"); err != nil {
		t.Fatalf("WorktreeAddNew: %v", err)
	}
	if !RefExists(ctx, dir, "refs/heads/session/feature") {
		t.Error("expected session/feature branch to exist after add")
	}
	// Confirm git sees the new worktree.
	paths, err := WorktreePaths(ctx, dir)
	if err != nil {
		t.Fatalf("WorktreePaths: %v", err)
	}
	if len(paths) < 2 {
		t.Errorf("expected at least 2 worktrees after add, got %v", paths)
	}
}

// RunClean ignores the caller's GIT_DIR and friends: it answers for the
// repository at dir.
func TestRunCleanIgnoresRepoVariables(t *testing.T) {
	ctx, repo := initRepo(t)
	_, other := initRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	got, err := RunClean(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := filepath.EvalSymlinks(OSPath(got)); g != repo {
		t.Errorf("RunClean toplevel = %q, want %q", got, repo)
	}
	// Run, by contrast, follows them.
	if got, err := Run(ctx, repo, "rev-parse", "--show-toplevel"); err == nil {
		if g, _ := filepath.EvalSymlinks(OSPath(got)); g == repo {
			t.Error("Run ignored GIT_WORK_TREE; the test proves nothing")
		}
	}
}

// RunClean removes every variable git lists, not only a fixed few:
// GIT_OBJECT_DIRECTORY is one only git's own list names.
func TestRunCleanUsesGitsList(t *testing.T) {
	ctx, repo := initRepo(t)
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(t.TempDir(), "no-objects"))
	if _, err := RunClean(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		t.Errorf("RunClean followed GIT_OBJECT_DIRECTORY: %v", err)
	}
	if _, err := Run(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}"); err == nil {
		t.Error("Run ignored GIT_OBJECT_DIRECTORY; the test proves nothing")
	}
}
