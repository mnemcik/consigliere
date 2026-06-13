package worktree

import (
	"context"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// ListEntry describes one worktree for `cg worktree list`.
type ListEntry struct {
	Slug     string // "" when the worktree is not a session worktree
	Branch   string // short branch name; "" when detached
	Path     string
	Detached bool
	Ahead    int // commits ahead of the local landing ref; -1 when unknown
}

// IsSession reports whether this entry is a session worktree (its branch
// carries the configured prefix).
func (e ListEntry) IsSession() bool { return e.Slug != "" }

// List returns every worktree of the repository at opt.Root, annotating session
// worktrees (branch with opt.BranchPrefix) with their slug and how many commits
// they are ahead of the local landing ref. It does not hit the network — Ahead
// is computed against the local remote-tracking ref and is -1 when that ref is
// unavailable.
func List(ctx context.Context, opt Options) ([]ListEntry, error) {
	worktrees, err := gitx.WorktreeList(ctx, opt.Root)
	if err != nil {
		return nil, err
	}
	landing := opt.landingRef()

	entries := make([]ListEntry, 0, len(worktrees))
	for _, wt := range worktrees {
		e := ListEntry{
			Branch:   wt.Branch,
			Path:     wt.Path,
			Detached: wt.Detached,
			Ahead:    -1,
		}
		if !wt.Detached && opt.BranchPrefix != "" && strings.HasPrefix(wt.Branch, opt.BranchPrefix) {
			e.Slug = strings.TrimPrefix(wt.Branch, opt.BranchPrefix)
			if out, lerr := gitx.LogOneline(ctx, opt.Root, landing+"..refs/heads/"+wt.Branch); lerr == nil {
				e.Ahead = countLines(out)
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
