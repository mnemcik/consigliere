package gitx

import (
	"context"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("skipping git-integration test on windows")
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
