package gitx

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

// PathState says whether git keeps a path's current content in history.
type PathState int

const (
	// PathUncommitted: git status reports a change for the path (staged,
	// unstaged or untracked), or git can't vouch for it (assume-unchanged,
	// skip-worktree, no commit yet, not a repository).
	PathUncommitted PathState = iota
	// PathCommitted: git status reports nothing for the path.
	PathCommitted
	// PathIgnored: git ignores the path, so it can't keep a change to it.
	PathIgnored
)

// argBudget bounds the bytes of paths passed to one git command, below the
// Windows command-line limit (32767 characters) with room for the rest.
const argBudget = 24 << 10

// PathStates reports, for each path (relative to dir, slash-separated), whether
// its content in dir's checkout is committed: `git status` reports nothing
// for it, with line endings and filters as git applies them. A path git
// ignores is PathIgnored. A path flagged assume-unchanged or skip-worktree, a
// checkout whose HEAD has no commit yet, and a dir that isn't in a git work
// tree are PathUncommitted. Long path lists are split into several git calls.
func PathStates(ctx context.Context, dir string, paths []string) (map[string]PathState, error) {
	out := make(map[string]PathState, len(paths))
	for _, p := range paths {
		out[p] = PathUncommitted
	}
	if len(paths) == 0 {
		return out, nil
	}
	if inside, err := RunClean(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil || inside != "true" {
		return out, nil
	}
	if _, err := RunClean(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return out, nil // unborn HEAD
	}
	prefix, err := RunCleanRaw(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	pre := strings.TrimRight(string(prefix), "\r\n")
	for _, p := range paths {
		out[p] = PathCommitted
	}
	for _, batch := range batches(paths) {
		args := append([]string{"--literal-pathspecs", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching", "--"}, batch...)
		raw, err := RunCleanRaw(ctx, dir, args...)
		if err != nil {
			return nil, err
		}
		for _, e := range parseStatusZ(raw) {
			rel, ok := strings.CutPrefix(e.path, pre)
			if !ok {
				continue
			}
			state := PathUncommitted
			if e.ignored {
				state = PathIgnored
			}
			for _, p := range batch {
				if samePathspec(p, rel) || (strings.HasSuffix(rel, "/") && hasPathPrefix(p, rel)) {
					if out[p] != PathUncommitted {
						out[p] = state
					}
				}
			}
		}
		ls, err := RunCleanRaw(ctx, dir, append([]string{"--literal-pathspecs", "ls-files", "-v", "-z", "--"}, batch...)...)
		if err != nil {
			return nil, err
		}
		for _, rec := range bytes.Split(ls, []byte{0}) {
			if len(rec) < 3 {
				continue
			}
			tag, p := rec[0], string(rec[2:])
			if (tag >= 'a' && tag <= 'z') || tag == 'S' {
				for _, q := range batch {
					if samePathspec(q, p) {
						out[q] = PathUncommitted
					}
				}
			}
		}
	}
	return out, nil
}

// foldsCase says paths compare without regard to case here, as git does
// with core.ignorecase on the usual macOS and Windows file systems. Folding
// where it isn't needed only makes more paths count as changed, the safe side.
var foldsCase = runtime.GOOS == "darwin" || filepathFoldsOnWindows()

// filepathFoldsOnWindows reports whether this is Windows, whose file systems
// ignore case.
func filepathFoldsOnWindows() bool { return filepath.Separator == '\\' }

func samePathspec(a, b string) bool {
	return a == b || (foldsCase && strings.EqualFold(a, b))
}

func hasPathPrefix(p, prefix string) bool {
	return strings.HasPrefix(p, prefix) || (foldsCase && len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix))
}

// statusEntry is one path git status -z reports.
type statusEntry struct {
	path    string
	ignored bool
}

// parseStatusZ reads `git status --porcelain=v1 -z` output. A rename or copy
// entry carries a second path (the source); both are returned, so a path on
// either side counts as changed.
func parseStatusZ(raw []byte) []statusEntry {
	var out []statusEntry
	recs := bytes.Split(raw, []byte{0})
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if len(rec) < 4 {
			continue
		}
		x, y, p := rec[0], rec[1], string(rec[3:])
		out = append(out, statusEntry{path: p, ignored: x == '!'})
		if (x == 'R' || x == 'C' || y == 'R' || y == 'C') && i+1 < len(recs) {
			i++
			out = append(out, statusEntry{path: string(recs[i])})
		}
	}
	return out
}

// batches splits paths into groups whose total length stays within argBudget.
func batches(paths []string) [][]string {
	var out [][]string
	cur := make([]string, 0, len(paths))
	size := 0
	for _, p := range paths {
		if len(cur) > 0 && size+len(p)+1 > argBudget {
			out = append(out, cur)
			cur, size = make([]string, 0, len(paths)), 0
		}
		cur = append(cur, p)
		size += len(p) + 1
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
