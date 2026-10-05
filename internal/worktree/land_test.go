package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

func landOpts(wt string) *LandOptions {
	return &LandOptions{WorktreeDir: wt, BranchPrefix: "session/", LandingBranch: "main"}
}

func headSHA(t *testing.T, ctx context.Context, dir string) string {
	t.Helper()
	sha, err := gitx.RevParse(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return sha
}

// commitFile writes content to name under dir and commits it.
func commitFile(t *testing.T, ctx context.Context, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, dir, "add", name)
	mustGit(t, ctx, dir, "commit", "-m", msg)
}

func TestLandDirectFresh(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "land1", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	want := headSHA(t, ctx, wt)

	res, err := Land(ctx, landOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if res.SHA != want {
		t.Errorf("landed SHA = %s, want %s", res.SHA, want)
	}
	// origin/main must now point at the landed commit.
	if err := gitx.Fetch(ctx, root, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	originMain, err := gitx.RevParse(ctx, root, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if originMain != want {
		t.Errorf("origin/main = %s, want %s", originMain, want)
	}
}

func TestLandNonFastForwardRebase(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "land2", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	commitFile(t, ctx, wt, "feature.txt", "feature\n", "feature work")

	// Advance origin/main from the main workspace so the push is non-ff.
	commitFile(t, ctx, root, "other.txt", "other\n", "concurrent landing")
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")

	res, err := Land(ctx, landOpts(wt), &log)
	if err != nil {
		t.Fatalf("Land (expected rebase+retry success): %v\nlog: %s", err, log.String())
	}
	// The rebased HEAD is the landed SHA and is on origin/main.
	if err := gitx.Fetch(ctx, root, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if !gitx.IsAncestor(ctx, root, res.SHA, "origin/main") {
		t.Errorf("landed SHA %s is not on origin/main", res.SHA)
	}
	// Both files exist on the landed tip → rebase preserved the feature commit.
	if !gitx.IsAncestor(ctx, wt, "HEAD", "origin/main") {
		t.Error("worktree HEAD should be on origin/main after rebase+land")
	}
}

func TestLandConflictExits3(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer

	wt, err := Create(ctx, "land3", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Same file, divergent content → rebase conflict.
	commitFile(t, ctx, wt, "clash.txt", "from-session\n", "session edit")
	commitFile(t, ctx, root, "clash.txt", "from-main\n", "main edit")
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")

	_, err = Land(ctx, landOpts(wt), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitConflict {
		t.Fatalf("expected ExitConflict (3), got %v", err)
	}
}

func TestLandRefusesNonSessionBranch(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	// root is on "main", which does not match the "session/" prefix.
	_, err := Land(ctx, landOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Fatalf("expected ExitUsage refusing non-session branch, got %v", err)
	}
}

func TestLandUnknownStrategy(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "land4", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "x")

	opt := landOpts(wt)
	opt.Strategy = "bogus"
	_, err = Land(ctx, opt, &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Fatalf("expected ExitUsage for unknown strategy, got %v", err)
	}
}

func TestLandToleratesSlugArg(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "land6", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	want := headSHA(t, ctx, wt)

	// Passing the worktree's own slug or its full session branch is a no-op
	// equivalent to no-arg (lands HEAD) rather than a "commit not found" error.
	for _, arg := range []string{"land6", "session/land6"} {
		opt := landOpts(wt)
		opt.TargetSHA = arg
		res, err := Land(ctx, opt, &log)
		if err != nil {
			t.Fatalf("Land with slug arg %q: %v\nlog: %s", arg, err, log.String())
		}
		if res.SHA != want {
			t.Errorf("arg %q: landed SHA = %s, want %s", arg, res.SHA, want)
		}
	}
}

func TestLandRejectsUnknownArg(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "land7", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "x")

	// An arg that is neither a known commit nor this worktree's slug is a usage
	// error (not silently tolerated).
	opt := landOpts(wt)
	opt.TargetSHA = "not-a-sha-or-slug"
	_, err = Land(ctx, opt, &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Fatalf("expected ExitUsage for unknown arg, got %v", err)
	}
}

func TestLandTargetSHANotReachable(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "land5", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "x")

	// A commit that exists in the repo but is not an ancestor of HEAD: make one
	// on a detached side-commit in the main workspace.
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "side")
	side := headSHA(t, ctx, root)

	opt := landOpts(wt)
	opt.TargetSHA = side
	_, err = Land(ctx, opt, &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitAssertFail {
		t.Fatalf("expected ExitAssertFail for unreachable target SHA, got %v", err)
	}
}

// consigliere#146: after a land, the checkout holding the landing branch (the
// main worktree here) is at the landed commit, not left behind origin.
func TestLandFastForwardsMainCheckout(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "ff1", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	commitFile(t, ctx, wt, "landed.md", "new\n", "feature work")
	want := headSHA(t, ctx, wt)

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if got := headSHA(t, ctx, root); got != want {
		t.Errorf("main checkout HEAD = %s, want the landed %s\nlog: %s", got, want, log.String())
	}
	if _, err := os.Stat(filepath.Join(root, "landed.md")); err != nil {
		t.Errorf("landed file not in the main checkout: %v", err)
	}
}

// Another session's uncommitted edit to a file the land touches blocks the
// fast-forward. The edit must survive, the land must still succeed, and the
// log must say the checkout was not updated.
func TestLandLeavesDirtyMainCheckoutAndWarns(t *testing.T) {
	ctx, root := setupWorkspace(t)
	commitFile(t, ctx, root, "shared.md", "base\n", "shared")
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")
	var log bytes.Buffer
	wt, err := Create(ctx, "ff2", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	commitFile(t, ctx, wt, "shared.md", "from the session\n", "session edit")
	before := headSHA(t, ctx, root)
	if err := os.WriteFile(filepath.Join(root, "shared.md"), []byte("uncommitted in main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land must still succeed: %v\nlog: %s", err, log.String())
	}
	if got, _ := os.ReadFile(filepath.Join(root, "shared.md")); string(got) != "uncommitted in main\n" {
		t.Errorf("uncommitted edit in main was overwritten: %q", got)
	}
	if got := headSHA(t, ctx, root); got != before {
		t.Errorf("main checkout moved despite the conflicting edit")
	}
	if !bytes.Contains(log.Bytes(), []byte("was not updated")) {
		t.Errorf("no warning in the log:\n%s", log.String())
	}
}

// With the landing branch checked out nowhere, the local branch ref still
// advances, so the next checkout of it starts current.
func TestLandAdvancesLandingRefWhenNotCheckedOut(t *testing.T) {
	ctx, root := setupWorkspace(t)
	mustGit(t, ctx, root, "checkout", "--quiet", "--detach")
	var log bytes.Buffer
	wt, err := Create(ctx, "ff3", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	want := headSHA(t, ctx, wt)

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	got, err := gitx.RevParse(ctx, root, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("local main = %s, want %s", got, want)
	}
}

// An uncommitted edit that does not overlap the landed files would let git
// fast-forward, but the checkout belongs to a session mid-edit: leave it.
func TestLandLeavesUnrelatedDirtyMainCheckout(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "ff4", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	commitFile(t, ctx, wt, "landed.md", "new\n", "feature work")
	before := headSHA(t, ctx, root)
	if err := os.WriteFile(filepath.Join(root, "unrelated.md"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, root, "add", "unrelated.md")

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if got := headSHA(t, ctx, root); got != before {
		t.Errorf("HEAD moved under a dirty checkout")
	}
	if !bytes.Contains(log.Bytes(), []byte("uncommitted changes")) {
		t.Errorf("log does not say why:\n%s", log.String())
	}
}

// Local commits on main that are not on origin: say it diverged, rather than
// suggesting a pull --ff-only that would fail too.
func TestLandReportsDivergedMainCheckout(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "ff5", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "local only")
	before := headSHA(t, ctx, root)

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	if got := headSHA(t, ctx, root); got != before {
		t.Errorf("diverged main was moved")
	}
	if !bytes.Contains(log.Bytes(), []byte("commits that are not on origin/main")) {
		t.Errorf("log does not report the divergence:\n%s", log.String())
	}
}

// A worktree mid-rebase of main lists as detached. The branch must not be
// moved underneath the rebase.
func TestLandDoesNotMoveMainDuringARebase(t *testing.T) {
	ctx, root := setupWorkspace(t)
	mustGit(t, ctx, root, "commit", "--allow-empty", "-m", "second")
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")
	before := headSHA(t, ctx, root)
	// Stop a rebase of main on an --exec step that fails.
	if _, err := gitx.RunEnv(ctx, root, []string{"GIT_SEQUENCE_EDITOR=true"}, "rebase", "-i", "--exec", "false", "HEAD~1"); err == nil {
		t.Fatal("expected the rebase to stop")
	}
	var log bytes.Buffer
	wt, err := Create(ctx, "ff6", defaultOpts(root), &log)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustGit(t, ctx, wt, "commit", "--allow-empty", "-m", "feature work")

	if _, err := Land(ctx, landOpts(wt), &log); err != nil {
		t.Fatalf("Land: %v\nlog: %s", err, log.String())
	}
	got, err := gitx.RevParse(ctx, root, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if got != before {
		t.Errorf("main moved during a rebase: %s, want %s\nlog: %s", got, before, log.String())
	}
	mustGit(t, ctx, root, "rebase", "--abort")
}
