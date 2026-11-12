// Package pause recognises the pause cursor a /wrap pause leaves behind:
// projects/<slug>/resume.md, written in the session worktree and committed to
// the session branch, which is not landed while the work is paused. The
// worktree and landing code use it to tell a paused branch, whose unlanded WIP
// commits are the pause, from work someone forgot to land, and to keep the
// cursor off the landing branch.
package pause

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// CursorName is the pause cursor's file name.
const CursorName = "resume.md"

// IsCursorPath reports whether the slash-separated, repo-relative path name is
// a pause cursor: <any dirs>/projects/<slug>/resume.md. Any depth is accepted,
// so a workspace nested below the git top level is recognised too.
func IsCursorPath(name string) bool {
	parts := strings.Split(strings.TrimSpace(name), "/")
	n := len(parts)
	return n >= 3 && parts[n-1] == CursorName && parts[n-3] == "projects" && parts[n-2] != ""
}

// OnRev returns the pause cursors committed on rev (repo-relative,
// slash-separated) that are not on the landing ref. A cursor that is also on
// the landing ref was landed by mistake; it does not mark rev as paused, so it
// cannot switch the unlanded-commit guard off for every branch. Errors read as
// "no cursor", which keeps the guard.
func OnRev(ctx context.Context, dir, rev, landing string) []string {
	out, err := gitx.Run(ctx, dir, "ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return nil
	}
	return notOn(ctx, dir, landing, cursors(out))
}

// InWorktree returns the pause cursors present in the working tree of the
// worktree at dir (tracked or untracked, as absolute paths) that are not on
// the landing ref. dir must be the worktree's top level.
func InWorktree(ctx context.Context, dir, landing string) []string {
	out, err := gitx.Run(ctx, dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil
	}
	var found []string
	for _, rel := range notOn(ctx, dir, landing, cursors(out)) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if fileExists(abs) {
			found = append(found, abs)
		}
	}
	return found
}

// cursors filters NUL-separated git path output down to pause cursors,
// de-duplicated (ls-files lists a modified tracked file once per state).
func cursors(out string) []string {
	var found []string
	seen := map[string]bool{}
	for _, name := range strings.Split(out, "\x00") {
		name = strings.TrimSpace(name)
		if IsCursorPath(name) && !seen[name] {
			seen[name] = true
			found = append(found, name)
		}
	}
	return found
}

// notOn drops the names that exist on ref.
func notOn(ctx context.Context, dir, ref string, names []string) []string {
	var keep []string
	for _, name := range names {
		if _, err := gitx.Run(ctx, dir, "cat-file", "-e", ref+":"+name); err != nil {
			keep = append(keep, name)
		}
	}
	return keep
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
