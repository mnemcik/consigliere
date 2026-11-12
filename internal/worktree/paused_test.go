package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
)

// pauseIn writes projects/p/resume.md in dir and commits it with a WIP commit,
// as the pause-mode wrap does.
func pauseIn(t *testing.T, ctx context.Context, dir string) {
	t.Helper()
	p := filepath.Join(dir, "projects", "p", "resume.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, dir, "add", "projects")
	mustGit(t, ctx, dir, "commit", "--quiet", "-m", "WIP pause: half done")
}

func TestCreateReusesPausedWorktree(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseIn(t, ctx, path)

	log.Reset()
	got, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil || got != path {
		t.Fatalf("Create on a paused worktree = (%q, %v), want reuse of %q", got, err, path)
	}
	if out := log.String(); !strings.Contains(out, "resuming paused worktree") || strings.Contains(out, "cg worktree land") {
		t.Errorf("log = %q, want the paused reuse and no landing hint", out)
	}
}

func TestCreateAttachesPausedBranch(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseIn(t, ctx, path)
	mustGit(t, ctx, root, "worktree", "remove", path)

	log.Reset()
	got, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil || got != path {
		t.Fatalf("Create on a paused branch = (%q, %v), want attach at %q", got, err, path)
	}
	if out := log.String(); !strings.Contains(out, "paused branch") || strings.Contains(out, "branch -D") {
		t.Errorf("log = %q, want the paused attach and no branch-delete hint", out)
	}
}

// A resume.md that was landed on main by mistake must not switch the
// unlanded-commit guard off for every worktree and branch.
func TestCreateLandedResumeKeepsGuard(t *testing.T) {
	ctx, root := setupWorkspace(t)
	pauseIn(t, ctx, root)
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")

	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, path, "commit", "--allow-empty", "-m", "wip work")

	_, err = Create(ctx, "p", defaultOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("worktree: expected ExitDirty, got %v", err)
	}

	mustGit(t, ctx, root, "worktree", "remove", path)
	_, err = Create(ctx, "p", defaultOpts(root), &log)
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("orphan branch: expected ExitDirty, got %v", err)
	}
}
