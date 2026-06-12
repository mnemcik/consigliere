package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

func TestValidSlug(t *testing.T) {
	good := []string{"a", "a1", "my-slug", "feat.x", "x_y-1"}
	bad := []string{"", "-x", ".x", "_x", "Upper", "has space", "a/b"}
	for _, s := range good {
		if !ValidSlug(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range bad {
		if ValidSlug(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name string
		opt  Options
		slug string
		want string
	}{
		{"default prefix = root", Options{Root: "/ws"}, "x", "/ws--x"},
		{"absolute prefix", Options{Root: "/ws", Prefix: "/other"}, "x", "/other--x"},
		{"relative prefix resolved against root", Options{Root: "/ws", Prefix: "wt"}, "x", "/ws/wt--x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opt.worktreePath(tc.slug); got != tc.want {
				t.Errorf("worktreePath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCreateInvalidSlug(t *testing.T) {
	// No git needed: validation happens before any git call.
	_, err := Create(context.Background(), "Bad Slug", Options{}, &bytes.Buffer{})
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Fatalf("expected ExitUsage coded error, got %v", err)
	}
}

// setupWorkspace builds a bare "origin" with a main branch and a clone acting as
// the main workspace root, returning (ctx, root). Skips when git is unavailable.
func setupWorkspace(t *testing.T) (ctx context.Context, root string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping git-integration test on windows")
	}
	ctx = context.Background()
	base := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}

	origin := filepath.Join(base, "origin.git")
	mustGit(t, ctx, "", "init", "--bare", "--initial-branch=main", origin)

	root = filepath.Join(base, "ws")
	mustGit(t, ctx, "", "clone", "--quiet", origin, root)
	mustGit(t, ctx, root, "config", "user.email", "test@example.com")
	mustGit(t, ctx, root, "config", "user.name", "Test")
	mustGit(t, ctx, root, "config", "commit.gpgsign", "false")
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "init")
	mustGit(t, ctx, root, "push", "--quiet", "-u", "origin", "main")
	return ctx, root
}

func mustGit(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(ctx, dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func defaultOpts(root string) Options {
	return Options{Root: root, BranchPrefix: "session/", LandingBranch: "main"}
}

func TestCreateFresh(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	path, err := Create(ctx, "feature", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v\nlog: %s", err, log.String())
	}
	want := root + "--feature"
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Errorf("expected worktree dir at %q", path)
	}
	if !gitx.RefExists(ctx, root, "refs/heads/session/feature") {
		t.Error("expected branch session/feature to exist")
	}
}

func TestCreateReuseClean(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	if _, err := Create(ctx, "reuse", defaultOpts(root), &log); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	// Second call on a clean worktree should reuse, not error.
	path, err := Create(ctx, "reuse", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if path != root+"--reuse" {
		t.Errorf("path = %q, want %q", path, root+"--reuse")
	}
}

func TestCreateUnlandedBlocksWithoutForce(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "wip", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Make an unlanded commit in the worktree.
	mustGit(t, ctx, path, "commit", "--allow-empty", "-m", "wip work")

	// Re-create without force → ExitDirty.
	_, err = Create(ctx, "wip", defaultOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("expected ExitDirty, got %v", err)
	}

	// With force → reuse succeeds.
	opts := defaultOpts(root)
	opts.Force = true
	if _, err := Create(ctx, "wip", opts, &log); err != nil {
		t.Fatalf("Create --force: %v", err)
	}
}
