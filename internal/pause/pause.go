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
// slash-separated) that differ from the landing ref. A cursor that is on the
// landing ref with the same content was landed by mistake; it does not mark rev
// as paused, so it cannot switch the unlanded-commit guard off for every
// branch. A new pause over such a file changes its content, so it still
// counts. Errors read as "no cursor", which keeps the guard.
func OnRev(ctx context.Context, dir, rev, landing string) []string {
	out, err := gitx.Run(ctx, dir, "ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return nil
	}
	var found []string
	for _, name := range cursors(out) {
		if blob, ok := blobAt(ctx, dir, rev, name); !ok || !sameOn(ctx, dir, landing, name, blob) {
			found = append(found, name)
		}
	}
	return found
}

// InWorktree returns the pause cursors present in the working tree of the
// worktree at dir (tracked or untracked, as absolute paths) that are not on
// the landing ref with the same content. dir must be the worktree's top level.
func InWorktree(ctx context.Context, dir, landing string) []string {
	out, err := gitx.Run(ctx, dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil
	}
	var found []string
	for _, rel := range cursors(out) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if !fileExists(abs) {
			continue
		}
		blob, err := gitx.Run(ctx, dir, "hash-object", "--", rel)
		if err != nil || !sameOn(ctx, dir, landing, rel, strings.TrimSpace(blob)) {
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

// blobAt returns the object id of name on rev.
func blobAt(ctx context.Context, dir, rev, name string) (string, bool) {
	out, err := gitx.Run(ctx, dir, "rev-parse", "--verify", "--quiet", rev+":"+name)
	if err != nil || out == "" {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// sameOn reports whether name exists on ref with the object id blob.
func sameOn(ctx context.Context, dir, ref, name, blob string) bool {
	onRef, ok := blobAt(ctx, dir, ref, name)
	return ok && onRef == blob
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
