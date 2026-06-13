package session

import (
	"context"
	"fmt"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// pullFetchTimeout bounds the network fetch so the SessionStart hook stays well
// under Claude Code's overall hook budget (the shell hook used 25s).
const pullFetchTimeout = 25 * time.Second

// PullResult carries an optional message for Claude Code to surface as a
// systemMessage. An empty SystemMessage means "say nothing" (the common
// already-up-to-date path).
type PullResult struct {
	SystemMessage string
}

// PullLatest keeps the main worktree's landing branch fresh from origin so any
// worktree the session creates branches off the latest tip. It targets the main
// worktree root (resolved from dir), and skips quietly unless that root is on
// the landing branch with a clean tree. Ports pull-latest-main.sh. It never
// returns an error for an operational problem — those are surfaced as a
// systemMessage so the hook can't fail the session.
func PullLatest(ctx context.Context, dir, landingBranch string) PullResult {
	root, err := gitx.CommonRoot(ctx, dir)
	if err != nil {
		return PullResult{} // not in a git repo — nothing to do
	}

	branch, err := gitx.SymbolicRefShort(ctx, root)
	if err != nil || branch != landingBranch {
		return PullResult{} // detached or not on the landing branch
	}
	if !gitx.IsClean(ctx, root) {
		return PullResult{} // dirty tree — leave it alone
	}

	fetchCtx, cancel := context.WithTimeout(ctx, pullFetchTimeout)
	defer cancel()
	if err := gitx.Fetch(fetchCtx, root, "origin", landingBranch); err != nil {
		return PullResult{SystemMessage: fmt.Sprintf(
			"Auto-pull: `git fetch` failed (%v). Run `git -C %s fetch origin` manually.", err, root)}
	}

	remoteRef := "origin/" + landingBranch
	local, lerr := gitx.RevParse(ctx, root, "HEAD")
	remote, rerr := gitx.RevParse(ctx, root, remoteRef)
	if lerr != nil || rerr != nil || local == "" || remote == "" {
		return PullResult{}
	}
	if local == remote {
		return PullResult{} // already up to date
	}

	switch {
	case gitx.IsAncestor(ctx, root, local, remote):
		// Clean fast-forward available.
		if err := gitx.MergeFFOnly(ctx, root, remoteRef); err != nil {
			return PullResult{SystemMessage: fmt.Sprintf(
				"Auto-pull: fast-forward of %s in %s failed (%v). Resolve manually.", landingBranch, root, err)}
		}
		newSHA, _ := gitx.RevParse(ctx, root, "HEAD")
		return PullResult{SystemMessage: fmt.Sprintf("Pulled %s into %s → %s", remoteRef, root, short(newSHA))}
	case gitx.IsAncestor(ctx, root, remote, local):
		return PullResult{} // local is ahead — nothing to pull
	default:
		return PullResult{SystemMessage: fmt.Sprintf(
			"WARNING: %s in %s has diverged from %s — auto-pull skipped. Resolve manually.", landingBranch, root, remoteRef)}
	}
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}
