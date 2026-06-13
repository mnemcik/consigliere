package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

func TestRemoveInvalidSlug(t *testing.T) {
	err := Remove(context.Background(), "Bad Slug", Options{}, &bytes.Buffer{})
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Fatalf("expected ExitUsage, got %v", err)
	}
}

func TestRemoveLanded(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "rm1", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Fresh branch sits at origin/main (no unlanded commits) → removable.
	if err := Remove(ctx, "rm1", defaultOpts(root), &log); err != nil {
		t.Fatalf("Remove: %v\nlog: %s", err, log.String())
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Errorf("worktree dir %s should be gone", wt)
	}
	if gitx.RefExists(ctx, root, "refs/heads/session/rm1") {
		t.Error("branch session/rm1 should be deleted")
	}
}

func TestRemoveUnlandedBlocksThenForce(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "rm2", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	commitFile(t, ctx, wt, "wip.txt", "wip\n", "unlanded work")

	// Without --force → ExitDirty.
	err = Remove(ctx, "rm2", defaultOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("expected ExitDirty, got %v", err)
	}
	// Worktree + branch must still exist.
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Error("worktree should still exist after refused remove")
	}

	// With --force → removed despite unlanded commits.
	opt := defaultOpts(root)
	opt.Force = true
	if err := Remove(ctx, "rm2", opt, &log); err != nil {
		t.Fatalf("Remove --force: %v\nlog: %s", err, log.String())
	}
	if gitx.RefExists(ctx, root, "refs/heads/session/rm2") {
		t.Error("branch session/rm2 should be deleted after --force")
	}
}

func TestRemoveMissingIsNoOp(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	// Never created — remove should succeed (nothing to do), not error.
	if err := Remove(ctx, "ghost", defaultOpts(root), &log); err != nil {
		t.Fatalf("Remove of nonexistent worktree should be a no-op, got %v", err)
	}
}
