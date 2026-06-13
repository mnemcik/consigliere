package worktree

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// TestFullCycle exercises the whole worktree lifecycle end to end —
// create → commit → land → remove — and asserts the commit reaches the landing
// branch and the worktree/branch are gone afterwards. This is the integration
// counterpart of the personal-workspace test-land-worktree-commit.sh smoke test.
func TestFullCycle(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "cycle", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	commitFile(t, ctx, wt, "feature.txt", "shipped\n", "cycle feature")
	want := headSHA(t, ctx, wt)

	res, err := Land(ctx, landOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if res.SHA != want {
		t.Errorf("landed SHA = %s, want %s", res.SHA, want)
	}
	if err := gitx.Fetch(ctx, root, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if origin, _ := gitx.RevParse(ctx, root, "origin/main"); origin != want {
		t.Errorf("origin/main = %s, want landed %s", origin, want)
	}

	// After landing, the branch is an ancestor of origin/main → remove succeeds
	// without --force.
	if err := Remove(ctx, "cycle", defaultOpts(root), &log); err != nil {
		t.Fatalf("Remove after land: %v\nlog: %s", err, log.String())
	}
	if gitx.RefExists(ctx, root, "refs/heads/session/cycle") {
		t.Error("branch session/cycle should be gone after remove")
	}
	paths, err := gitx.WorktreePaths(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if containsPath(paths, wt) {
		t.Errorf("worktree %s should be unregistered after remove", wt)
	}
}

// TestFullCycleListReflectsState confirms `list` sees the session worktree mid
// cycle and stops listing it after removal.
func TestFullCycleListReflectsState(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	if _, err := Create(ctx, "seen", defaultOpts(root), &log); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !listed(ctx, t, root, "seen") {
		t.Fatal("expected 'seen' to be listed after create")
	}

	if err := Remove(ctx, "seen", defaultOpts(root), &log); err != nil {
		var coded *cgerr.CodedError
		if errors.As(err, &coded) {
			t.Fatalf("Remove returned coded error %d: %v", coded.ExitCode(), err)
		}
		t.Fatalf("Remove: %v", err)
	}
	if listed(ctx, t, root, "seen") {
		t.Error("expected 'seen' to be gone from list after remove")
	}
}

func listed(ctx context.Context, t *testing.T, root, slug string) bool {
	t.Helper()
	entries, err := List(ctx, defaultOpts(root))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range entries {
		if e.Slug == slug {
			return true
		}
	}
	return false
}
