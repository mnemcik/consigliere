package worktree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// englishGit is the environment for git commands whose messages gitError
// parses: LC_ALL=C keeps them in English.
var englishGit = []string{"LC_ALL=C"}

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
	// Strategy is "direct-to-main", "pr" or "local". Callers resolve an unset
	// value with EffectiveStrategy; empty here means the default.
	Strategy string
	// TargetSHA, when set, asserts that commit is reachable from HEAD before
	// landing — an optional manual reachability guard. No caller requires it
	// (the wrap skill, ≥1.1.0, invokes land bare); retained as a manual convenience.
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
// local: no remote. Rebases onto the local landing branch when needed and
// fast-forwards it to HEAD, together with the checkout that holds it (see
// landLocal).
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

	// Optional manual reachability guard: the named SHA must be part of this branch.
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
	case workspace.StrategyDirectToMain, workspace.StrategyPR:
		if err := requireOrigin(ctx, dir, fmt.Sprintf("%q", strategy)); err != nil {
			return LandResult{}, err
		}
		if strategy == workspace.StrategyPR {
			return landPR(ctx, dir, branch, landingBranch, logf)
		}
		return landDirect(ctx, dir, branch, landingBranch, retries, logf)
	case workspace.StrategyLocal:
		return landLocal(ctx, dir, branch, landingBranch, retries, logf)
	default:
		return LandResult{}, cgerr.New(cgerr.ExitUsage,
			"unknown landing strategy %q (want %q, %q or %q)",
			strategy, workspace.StrategyDirectToMain, workspace.StrategyPR, workspace.StrategyLocal)
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
	syncLandingCheckout(ctx, dir, landingBranch, logf)
	return LandResult{Strategy: workspace.StrategyDirectToMain, SHA: landed}, nil
}

// syncLandingCheckout brings the local landing branch up to the commit just
// pushed. The push moves only origin/<landing>; without this the checkout that
// holds the branch, usually the main worktree, silently falls a commit further
// behind with every land, and anything reading files there reads stale ones
// (consigliere#146).
//
// That checkout may belong to another session, so this is as conservative as
// PullLatest: it only fast-forwards, and only a clean tree. A dirty tree is
// left alone even when git would allow the fast-forward, because moving HEAD
// and files under a session mid-edit is the same staleness in the other
// direction. When it cannot update, the land has still succeeded, so it says
// why and how to catch up rather than failing. The fast-forward briefly holds
// that checkout's index.lock, so a git command another session runs at the
// same instant can fail once with "index.lock exists".
func syncLandingCheckout(ctx context.Context, dir, landingBranch string, logf func(string, ...any)) {
	landingRef := "origin/" + landingBranch
	branchRef := "refs/heads/" + landingBranch
	trees, err := gitx.WorktreeList(ctx, dir)
	if err != nil {
		logf("note: could not list worktrees to update %s: %v\n", landingBranch, err)
		return
	}
	pull := "run 'git pull --ff-only' there before reading its files"
	skip := func(path, why, advice string) {
		logf("note: %s (%s) was not updated to %s: %s\n  %s\n", path, landingBranch, landingRef, why, advice)
	}
	for _, w := range trees {
		if w.Branch != landingBranch {
			continue
		}
		if _, serr := os.Stat(w.Path); serr != nil {
			// A worktree whose directory was deleted still lists, as prunable.
			skip(w.Path, "its directory is missing", "run 'git worktree prune' to drop the stale entry")
			return
		}
		local, remote := w.Head, ""
		if r, rerr := gitx.RevParse(ctx, dir, landingRef); rerr == nil {
			remote = r
		}
		switch {
		case local == "" || remote == "" || local == remote:
			return // nothing to do, or nothing safe to compare
		case !gitx.IsAncestor(ctx, dir, local, remote):
			skip(w.Path, "it has commits that are not on "+landingRef,
				"push or rebase those commits; a pull --ff-only would fail too")
			return
		case !gitx.IsClean(ctx, w.Path):
			skip(w.Path, "it has uncommitted changes", pull+" once they are committed or stashed")
			return
		}
		// LC_ALL=C keeps git's messages in English, which gitError parses.
		if _, err := gitx.RunEnv(ctx, w.Path, englishGit, "merge", "--ff-only", "--quiet", landingRef); err != nil {
			skip(w.Path, gitError(err), pull)
			return
		}
		logf("updated %s to %s\n", w.Path, landingRef)
		return
	}
	// Checked out nowhere, or only mid-rebase (a rebasing worktree lists as
	// detached). Move the branch with `git branch -f`, which refuses while any
	// worktree has it checked out or is rebasing it, and only when the move is
	// a fast-forward.
	if !gitx.RefExists(ctx, dir, branchRef) || !gitx.IsAncestor(ctx, dir, branchRef, landingRef) {
		return
	}
	if _, err := gitx.RunEnv(ctx, dir, englishGit, "branch", "-f", landingBranch, landingRef); err != nil {
		logf("note: local %s was not moved to %s: %s\n", landingBranch, landingRef, gitError(err))
		return
	}
	logf("moved local %s to %s\n", landingBranch, landingRef)
}

// gitError reduces a git error to what a reader needs: from the first
// "fatal:" or "error:" line on, so a leading hint is skipped but the detail
// after it (the files an untracked overwrite would clobber) is kept, joined
// onto one line.
func gitError(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	for i, l := range lines {
		for _, p := range []string{"fatal: ", "error: "} {
			if j := strings.Index(l, p); j >= 0 {
				rest := []string{strings.TrimSpace(l[j:])}
				for _, more := range lines[i+1:] {
					more = strings.TrimSpace(more)
					if more == "" || strings.HasPrefix(more, "hint:") || strings.HasPrefix(more, "Please ") || strings.HasPrefix(more, "Aborting") {
						continue
					}
					rest = append(rest, more)
				}
				return strings.Join(rest, " ")
			}
		}
	}
	return strings.TrimSpace(lines[0])
}

// errLandRaced reports that the landing branch moved between the rebase and
// the fast-forward (another session landed first), or that its checkout's
// index was locked for a moment. Both clear on a retry.
var errLandRaced = errors.New("landing branch moved or its checkout was busy")

// landRetryDelay is the pause before retrying a raced local land. A variable
// so tests can shorten it.
var landRetryDelay = 200 * time.Millisecond

// landLocal lands HEAD onto the local landing branch, for a workspace with no
// remote. The local branch is the integration point every session reads, so
// unlike landDirect's best-effort syncLandingCheckout, moving it IS the land:
// when the checkout that holds it cannot fast-forward, the land fails
// (ExitLandingBlocked) rather than reporting success over a stale checkout.
//
// Lands are serialised by acquireLandLock. Each attempt rebases onto the
// landing branch if HEAD does not already contain it, then advances the
// branch. Something outside cg moving the branch, or holding the checkout's
// index.lock, shows up as a rejected fast-forward; it backs off, rebases and
// retries, up to maxRetries.
func landLocal(ctx context.Context, dir, branch, landingBranch string, maxRetries int, logf func(string, ...any)) (LandResult, error) {
	branchRef := "refs/heads/" + landingBranch
	if !gitx.RefExists(ctx, dir, branchRef) {
		return LandResult{}, cgerr.New(cgerr.ExitUsage, "landing branch %s does not exist", landingBranch)
	}
	release, err := acquireLandLock(ctx, dir, logf)
	if err != nil {
		return LandResult{}, err
	}
	defer release()

	for attempt := 1; ; attempt++ {
		if !gitx.IsAncestor(ctx, dir, branchRef, "HEAD") {
			logf("rebasing %s onto %s\n", branch, landingBranch)
			if err := gitx.Rebase(ctx, dir, branchRef); err != nil {
				conflicts, _ := gitx.ConflictedFiles(ctx, dir)
				where := strings.Join(conflicts, " ")
				if where == "" {
					where = "<see git status>"
				}
				return LandResult{}, cgerr.New(cgerr.ExitConflict,
					"rebase conflict on: %s — resolve with 'git rebase --continue' then re-run", where)
			}
		}
		head, err := gitx.RevParse(ctx, dir, "HEAD")
		if err != nil {
			return LandResult{}, err
		}
		logf("attempt %d: moving %s to %s\n", attempt, landingBranch, short(head))
		err = advanceLanding(ctx, dir, landingBranch, head, logf)
		if err == nil {
			break
		}
		if !errors.Is(err, errLandRaced) {
			return LandResult{}, err
		}
		if attempt > maxRetries {
			hint := ""
			if strings.Contains(err.Error(), ".lock") {
				hint = " — if no git process is running, delete the .lock file it names and re-run"
			}
			return LandResult{}, cgerr.New(cgerr.ExitPushFail,
				"local land failed after %d retries: %v%s", maxRetries, err, hint)
		}
		logf("%v; retrying\n", err)
		time.Sleep(time.Duration(attempt) * landRetryDelay)
	}

	if !gitx.IsAncestor(ctx, dir, "HEAD", branchRef) {
		return LandResult{}, cgerr.New(cgerr.ExitAssertFail,
			"post-land assertion failed: HEAD is NOT on %s", landingBranch)
	}
	landed, err := gitx.RevParse(ctx, dir, "HEAD")
	if err != nil {
		return LandResult{}, err
	}
	logf("assertion passed: %s is on %s\n", landed, landingBranch)
	return LandResult{Strategy: workspace.StrategyLocal, SHA: landed}, nil
}

// advanceLanding fast-forwards the local landing branch to sha.
//
// Checked out in a worktree (normally the main one): `merge --ff-only` there,
// so the branch and the files move together and git's post-merge hook runs.
// git refuses when an uncommitted change or untracked file is in the way, and
// that refusal blocks the land. An uncommitted change the merge does not
// touch is left as it is, the same as a `git pull` would.
//
// Checked out nowhere: a compare-and-swap update-ref, refused while any
// worktree is mid-rebase of the branch. A checked-out branch is never moved
// with update-ref, which would leave its checkout silently stale.
func advanceLanding(ctx context.Context, dir, landingBranch, sha string, logf func(string, ...any)) error {
	branchRef := "refs/heads/" + landingBranch
	trees, err := gitx.WorktreeList(ctx, dir)
	if err != nil {
		return err
	}
	blocked := func(path, why, advice string) error {
		return cgerr.New(cgerr.ExitLandingBlocked,
			"%s (%s) cannot move to the landed commit: %s — %s, then re-run 'cg worktree land'",
			path, landingBranch, why, advice)
	}
	for _, w := range trees {
		if w.Branch == landingBranch {
			if _, serr := os.Stat(w.Path); serr != nil {
				return blocked(w.Path, "its directory is missing", "run 'git worktree prune' to drop the stale entry")
			}
			if gitx.RebasingBranch(ctx, w.Path) == branchRef {
				return blocked(w.Path, "a rebase of it is in progress", "finish or abort the rebase there")
			}
			if !gitx.IsAncestor(ctx, dir, w.Head, sha) {
				return fmt.Errorf("%w: %s moved", errLandRaced, landingBranch)
			}
			// LC_ALL=C keeps git's messages in English, which gitError parses.
			if _, err := gitx.RunEnv(ctx, w.Path, englishGit, "merge", "--ff-only", "--quiet", sha); err != nil {
				msg := gitError(err)
				if strings.Contains(msg, "index.lock") || strings.Contains(msg, "cannot lock ref") || strings.Contains(err.Error(), "Not possible to fast-forward") {
					return fmt.Errorf("%w: %s", errLandRaced, msg)
				}
				return blocked(w.Path, msg, "commit, stash or move the files in the way")
			}
			logf("fast-forwarded %s in %s\n", landingBranch, w.Path)
			return nil
		}
		if w.Detached && gitx.RebasingBranch(ctx, w.Path) == branchRef {
			return blocked(w.Path, "a rebase of it is in progress", "finish or abort the rebase there")
		}
	}
	old, err := gitx.RevParse(ctx, dir, branchRef)
	if err != nil {
		return err
	}
	if !gitx.IsAncestor(ctx, dir, old, sha) {
		return fmt.Errorf("%w: %s moved", errLandRaced, landingBranch)
	}
	if _, err := gitx.Run(ctx, dir, "update-ref", "-m", "cg worktree land", branchRef, sha, old); err != nil {
		return fmt.Errorf("%w: %s", errLandRaced, gitError(err))
	}
	logf("moved local %s to %s\n", landingBranch, short(sha))
	return nil
}

// short abbreviates an object id for log lines.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
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
