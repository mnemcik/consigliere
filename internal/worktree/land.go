package worktree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// defaultMaxRetries is the number of non-fast-forward rebase+retry cycles the
// direct-to-main strategy attempts before giving up (matches the bash helper).
const defaultMaxRetries = 2

// LandOptions configures a land operation.
type LandOptions struct {
	// WorktreeDir is the session worktree the land runs from (its HEAD is what
	// gets landed). Defaults to the caller's cwd when empty.
	WorktreeDir string
	// BranchPrefix is the expected session-branch prefix (e.g. "session/").
	BranchPrefix string
	// LandingBranch is the branch to land onto (e.g. "main").
	LandingBranch string
	// Strategy is "direct-to-main" or "pr".
	Strategy string
	// TargetSHA, when set, must be reachable from HEAD (a caller-compat guard).
	TargetSHA string
	// MaxRetries bounds the non-ff rebase+retry loop (0 → defaultMaxRetries).
	MaxRetries int
}

// LandResult reports the outcome of a land.
type LandResult struct {
	Strategy string
	SHA      string // direct-to-main: the landed commit
	PRURL    string // pr: the created/opened pull request URL
}

// Land lands the session worktree's HEAD onto the landing branch.
//
// direct-to-main: pushes HEAD → origin/<landingBranch>; on a non-ff rejection
// it fetches, rebases onto origin/<landingBranch>, and retries (up to
// MaxRetries). A rebase conflict leaves the rebase in progress and returns
// ExitConflict. A detached HEAD is lazy-converted to "<branchPrefix><slug>"
// first. Ports land-worktree-commit.sh and preserves its exit-code contract.
//
// pr: pushes the session branch to origin and opens a pull request via `gh`
// (no rebase-onto-main); returns the PR URL.
func Land(ctx context.Context, opt *LandOptions, logw io.Writer) (LandResult, error) {
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(logw, format, a...) }

	dir := opt.WorktreeDir
	branchPrefix := opt.BranchPrefix
	if branchPrefix == "" {
		branchPrefix = workspace.DefaultBranchPrefix
	}
	landingBranch := opt.LandingBranch
	if landingBranch == "" {
		landingBranch = workspace.DefaultLandingBranch
	}
	strategy := opt.Strategy
	if strategy == "" {
		strategy = workspace.DefaultLandingStrategy
	}

	// Resolve the current branch, lazy-converting a detached HEAD.
	branch, err := ensureSessionBranch(ctx, dir, branchPrefix, logf)
	if err != nil {
		return LandResult{}, err
	}

	// Optional caller-compat guard: the named SHA must be part of this branch.
	//
	// Slug tolerance: callers sometimes pass the session slug here by analogy
	// with `worktree create <slug>` / `remove <slug>`. The arg is redundant for
	// land — it always lands THIS worktree's HEAD — so when it names this
	// worktree (the slug or the full session branch) accept it as the no-arg
	// case instead of failing, and proceed as if no SHA was given.
	if opt.TargetSHA != "" {
		slug := strings.TrimPrefix(branch, branchPrefix)
		switch {
		case opt.TargetSHA == slug || opt.TargetSHA == branch:
			logf("note: %q is this worktree's slug, not a commit; landing HEAD (the <sha> arg is optional — run with no args)\n", opt.TargetSHA)
		case !gitx.CommitishExists(ctx, dir, opt.TargetSHA):
			return LandResult{}, cgerr.New(cgerr.ExitUsage,
				"%q is neither a commit reachable from HEAD nor this worktree's slug (%s); "+
					"land takes an optional <sha> — usually run it with no args from the worktree",
				opt.TargetSHA, slug)
		case !gitx.IsAncestor(ctx, dir, opt.TargetSHA, "HEAD"):
			return LandResult{}, cgerr.New(cgerr.ExitAssertFail,
				"%s is not reachable from HEAD (%s)", opt.TargetSHA, branch)
		}
	}

	retries := opt.MaxRetries
	if retries <= 0 {
		retries = defaultMaxRetries
	}

	switch strategy {
	case workspace.StrategyDirectToMain:
		return landDirect(ctx, dir, branch, landingBranch, retries, logf)
	case workspace.StrategyPR:
		return landPR(ctx, dir, branch, landingBranch, logf)
	default:
		return LandResult{}, cgerr.New(cgerr.ExitUsage,
			"unknown landing strategy %q (want %q or %q)",
			strategy, workspace.StrategyDirectToMain, workspace.StrategyPR)
	}
}

// ensureSessionBranch returns the current branch, converting a detached HEAD to
// "<branchPrefix><slug>" (slug derived from the worktree dir name), and refuses
// to land from a non-session branch.
func ensureSessionBranch(ctx context.Context, dir, branchPrefix string, logf func(string, ...any)) (string, error) {
	if gitx.IsDetached(ctx, dir) {
		slug := slugFromWorktree(ctx, dir)
		branch := branchPrefix + slug
		logf("detached HEAD detected; creating %s at HEAD\n", branch)
		if err := gitx.CheckoutNewBranch(ctx, dir, branch); err != nil {
			return "", err
		}
		return branch, nil
	}
	branch, err := gitx.SymbolicRefShort(ctx, dir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(branch, branchPrefix) {
		return "", cgerr.New(cgerr.ExitUsage,
			"HEAD is on %q, not %s* — refusing", branch, branchPrefix)
	}
	return branch, nil
}

// slugFromWorktree derives the session slug from the worktree directory name by
// stripping the "<main-workspace-basename>--" prefix (the worktree naming
// convention). Falls back to the bare basename if the prefix is absent.
func slugFromWorktree(ctx context.Context, dir string) string {
	base := filepath.Base(resolve(dir))
	if root, err := gitx.CommonRoot(ctx, dir); err == nil {
		if slug, ok := strings.CutPrefix(base, filepath.Base(root)+"--"); ok {
			return slug
		}
	}
	return base
}

func landDirect(ctx context.Context, dir, branch, landingBranch string, maxRetries int, logf func(string, ...any)) (LandResult, error) {
	landingRef := "origin/" + landingBranch
	refspec := "HEAD:" + landingBranch

	if err := gitx.Fetch(ctx, dir, "origin", landingBranch); err != nil {
		return LandResult{}, err
	}

	pushed := false
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		logf("attempt %d: pushing %s → %s\n", attempt, branch, landingRef)
		if err := gitx.Push(ctx, dir, "origin", refspec); err == nil {
			logf("push succeeded\n")
			pushed = true
			break
		}
		if attempt >= maxRetries+1 {
			return LandResult{}, cgerr.New(cgerr.ExitPushFail,
				"push failed after %d retries", maxRetries)
		}
		logf("push rejected (non-ff); fetching and rebasing onto %s\n", landingRef)
		if err := gitx.Fetch(ctx, dir, "origin", landingBranch); err != nil {
			return LandResult{}, err
		}
		if err := gitx.Rebase(ctx, dir, landingRef); err != nil {
			conflicts, _ := gitx.ConflictedFiles(ctx, dir)
			where := strings.Join(conflicts, " ")
			if where == "" {
				where = "<see git status>"
			}
			return LandResult{}, cgerr.New(cgerr.ExitConflict,
				"rebase conflict on: %s — resolve with 'git rebase --continue' then re-run", where)
		}
	}
	if !pushed { // defensive; the loop either breaks on success or returns above
		return LandResult{}, cgerr.New(cgerr.ExitPushFail, "push did not complete")
	}

	// Defense-in-depth: confirm HEAD actually landed on the remote branch.
	if err := gitx.Fetch(ctx, dir, "origin", landingBranch); err != nil {
		return LandResult{}, err
	}
	if !gitx.IsAncestor(ctx, dir, "HEAD", landingRef) {
		return LandResult{}, cgerr.New(cgerr.ExitAssertFail,
			"post-push assertion failed: HEAD is NOT on %s", landingRef)
	}
	landed, err := gitx.RevParse(ctx, dir, "HEAD")
	if err != nil {
		return LandResult{}, err
	}
	logf("assertion passed: %s is on %s\n", landed, landingRef)
	return LandResult{Strategy: workspace.StrategyDirectToMain, SHA: landed}, nil
}

func landPR(ctx context.Context, dir, branch, landingBranch string, logf func(string, ...any)) (LandResult, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return LandResult{}, cgerr.New(cgerr.ExitUsage,
			"pr landing strategy requires the GitHub CLI (gh) on PATH: %v", err)
	}
	// Publish the session branch so the PR has a head to track.
	logf("pushing %s → origin/%s\n", branch, branch)
	if err := gitx.Push(ctx, dir, "origin", "HEAD:refs/heads/"+branch); err != nil {
		return LandResult{}, cgerr.New(cgerr.ExitPushFail, "pushing session branch: %v", err)
	}
	// Open (or surface the existing) PR. `gh pr create` prints the URL; if a PR
	// already exists for this head it errors, so fall back to `gh pr view`.
	url, err := ghPRCreateOrView(ctx, dir, branch, landingBranch)
	if err != nil {
		return LandResult{}, cgerr.New(cgerr.ExitPushFail, "opening pull request: %v", err)
	}
	logf("pull request ready: %s\n", url)
	return LandResult{Strategy: workspace.StrategyPR, PRURL: url}, nil
}

// ghPRCreateOrView opens a PR for branch against landingBranch and returns its
// URL, falling back to the existing PR's URL when one is already open.
func ghPRCreateOrView(ctx context.Context, dir, branch, landingBranch string) (string, error) {
	// #nosec G204 -- "gh" is a fixed binary; branch/landingBranch are internal.
	create := exec.CommandContext(ctx, "gh", "pr", "create",
		"--base", landingBranch, "--head", branch, "--fill")
	create.Dir = dir
	out, createErr := create.Output()
	if createErr == nil {
		return strings.TrimSpace(string(out)), nil
	}
	// create failed — most often because a PR already exists for this head, but
	// possibly auth/network/rate-limit. Try to surface the existing PR; if that
	// also fails, report gh's own stderr from create so the cause isn't lost.
	// #nosec G204 -- see above.
	view := exec.CommandContext(ctx, "gh", "pr", "view", branch, "--json", "url", "-q", ".url")
	view.Dir = dir
	viewOut, viewErr := view.Output()
	if viewErr != nil {
		return "", fmt.Errorf("gh pr create failed: %s; no existing PR for %s: %w",
			ghStderr(createErr), branch, viewErr)
	}
	return strings.TrimSpace(string(viewOut)), nil
}

// ghStderr extracts the captured stderr from an exec failure (gh writes its
// human-readable error there), falling back to the error's own string.
func ghStderr(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return msg
		}
	}
	return err.Error()
}
