package worktree

import (
	"context"
	"fmt"
	"io"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// Remove deletes the session worktree for slug and its local branch, refusing
// (ExitDirty) when the branch has commits not yet landed on the landing branch
// unless opt.Force. It will not run from inside the worktree being removed.
// Ports remove-session-worktree.sh. Status goes to logw.
func Remove(ctx context.Context, slug string, opt Options, logw io.Writer) error {
	if !ValidSlug(slug) {
		return cgerr.New(cgerr.ExitUsage, "invalid slug %q — must match [a-z0-9][a-z0-9._-]*", slug)
	}
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(logw, format, a...) }

	worktreePath := opt.worktreePath(slug)
	branch := opt.branch(slug)
	branchRef := "refs/heads/" + branch
	landing := opt.landingRef()

	// Refuse to remove the worktree we're standing in.
	if top := gitx.ShowToplevel(ctx, "."); top != "" && resolve(top) == resolve(worktreePath) {
		return cgerr.New(cgerr.ExitUsage,
			"cwd is inside the worktree to be removed (%s) — cd elsewhere first", worktreePath)
	}

	if err := gitx.Fetch(ctx, opt.Root, "origin", opt.LandingBranch); err != nil {
		return err
	}

	// Safety: block on unlanded commits unless forced.
	if gitx.RefExists(ctx, opt.Root, branchRef) && !gitx.IsAncestor(ctx, opt.Root, branchRef, landing) {
		if !opt.Force {
			unlanded, err := gitx.LogOneline(ctx, opt.Root, landing+".."+branchRef)
			if err != nil {
				return err
			}
			logf("branch %s has unlanded commits:\n%s\n", branch, unlanded)
			logf("land them first (cg worktree land) or re-run with --force to discard\n")
			return cgerr.New(cgerr.ExitDirty, "unlanded commits on branch %s", branch)
		}
		logf("WARNING: discarding unlanded commits on %s (--force)\n", branch)
	}

	// Remove the worktree if it's still registered.
	paths, err := gitx.WorktreePaths(ctx, opt.Root)
	if err != nil {
		return err
	}
	if containsPath(paths, worktreePath) {
		logf("removing worktree %s\n", worktreePath)
		if err := gitx.WorktreeRemove(ctx, opt.Root, worktreePath, opt.Force); err != nil {
			return err
		}
	} else {
		logf("no worktree at %s (already removed or never created)\n", worktreePath)
	}

	// Delete the branch if present. Always -D: the real safety check (merged to
	// the landing ref) already ran above; -d would spuriously refuse when the
	// local landing branch lags origin (observed 2026-04-24).
	if gitx.RefExists(ctx, opt.Root, branchRef) {
		logf("deleting branch %s\n", branch)
		if err := gitx.BranchDelete(ctx, opt.Root, branch); err != nil {
			return err
		}
	} else {
		logf("no branch %s (already deleted or never created)\n", branch)
	}

	logf("done\n")
	return nil
}
