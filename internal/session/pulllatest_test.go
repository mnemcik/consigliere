package session

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

func gitRun(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(ctx, dir, args...); err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
}

// twoClones sets up a bare origin with a committed main branch and two working
// clones tracking it. Returns (ctx, ws, ws2).
func twoClones(t *testing.T) (ctx context.Context, ws, ws2 string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available")
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
	gitRun(t, ctx, "", "init", "--bare", "--initial-branch=main", origin)

	ws = filepath.Join(base, "ws")
	gitRun(t, ctx, "", "clone", "--quiet", origin, ws)
	configClone(t, ctx, ws)
	gitRun(t, ctx, ws, "commit", "--allow-empty", "-m", "c1")
	gitRun(t, ctx, ws, "push", "--quiet", "-u", "origin", "main")

	ws2 = filepath.Join(base, "ws2")
	gitRun(t, ctx, "", "clone", "--quiet", origin, ws2)
	configClone(t, ctx, ws2)
	return ctx, ws, ws2
}

func configClone(t *testing.T, ctx context.Context, dir string) {
	gitRun(t, ctx, dir, "config", "user.email", "t@e.com")
	gitRun(t, ctx, dir, "config", "user.name", "T")
	gitRun(t, ctx, dir, "config", "commit.gpgsign", "false")
}

func TestPullLatestUpToDate(t *testing.T) {
	ctx, ws, _ := twoClones(t)
	if res := PullLatest(ctx, ws, "main"); res.SystemMessage != "" {
		t.Errorf("up-to-date should be silent, got %q", res.SystemMessage)
	}
}

func TestPullLatestFastForward(t *testing.T) {
	ctx, ws, ws2 := twoClones(t)
	// Advance origin/main from ws2.
	gitRun(t, ctx, ws2, "commit", "--allow-empty", "-m", "c2")
	gitRun(t, ctx, ws2, "push", "--quiet", "origin", "main")

	res := PullLatest(ctx, ws, "main")
	if !strings.Contains(res.SystemMessage, "Pulled") {
		t.Errorf("expected a Pulled message, got %q", res.SystemMessage)
	}
	// ws should now be at origin/main.
	if !gitx.IsAncestor(ctx, ws, "origin/main", "HEAD") {
		t.Error("ws HEAD should include origin/main after fast-forward")
	}
}

func TestPullLatestNotOnLandingBranch(t *testing.T) {
	ctx, ws, _ := twoClones(t)
	gitRun(t, ctx, ws, "checkout", "-q", "-b", "feature")
	if res := PullLatest(ctx, ws, "main"); res.SystemMessage != "" {
		t.Errorf("off-branch should be silent, got %q", res.SystemMessage)
	}
}

func TestPullLatestDiverged(t *testing.T) {
	ctx, ws, ws2 := twoClones(t)
	// ws commits locally (not pushed); origin advances differently via ws2.
	gitRun(t, ctx, ws, "commit", "--allow-empty", "-m", "local-only")
	gitRun(t, ctx, ws2, "commit", "--allow-empty", "-m", "remote-only")
	gitRun(t, ctx, ws2, "push", "--quiet", "origin", "main")

	res := PullLatest(ctx, ws, "main")
	if !strings.Contains(res.SystemMessage, "diverged") {
		t.Errorf("expected a diverged warning, got %q", res.SystemMessage)
	}
}
