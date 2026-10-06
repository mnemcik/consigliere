// Package githooks installs the git hooks that extensions contribute.
//
// Contributed scripts live in the workspace, versioned, under
// .cg/git-hooks/<hook>.d/. What runs them is a small dispatcher at
// .git/hooks/<hook>, which git does not version — so a fresh clone has the
// scripts but no dispatcher. Ensure reconciles the two: it installs a
// dispatcher for every hook that has an approved script and removes it from
// every hook that has none. It is idempotent and cheap when there is nothing to
// do, so callers run it whenever the workspace may have changed (extension
// install, update and remove, cg init, cg sync, session start).
//
// Trust: git never versions hooks, so that a pull or checkout cannot make code
// run. Versioned scripts would undo that, so a script runs only when approved:
// its content must match the same script in the machine-local clone of an
// extension installed in this workspace — code the user chose to install on
// this machine. Ensure writes the approved scripts (path and blob id) to
// cg-hooks.allow in the git dir, which is not versioned; the dispatcher runs
// only scripts listed there with matching content and warns about any other.
//
// A hook file that was there before cg is kept as <hook>.pre-cg; the
// dispatcher runs it first, and Ensure puts it back when the dispatcher goes.
package githooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// DirRel is the workspace-relative directory holding contributed scripts, one
// subdirectory per hook: .cg/git-hooks/<hook>.d/.
const DirRel = ".cg/git-hooks"

// AllowFile is the name of the approved-scripts list in the git common dir.
const AllowFile = "cg-hooks.allow"

// marker identifies a dispatcher cg installed.
const marker = "# cg-git-hook-dispatcher"

// preSuffix names a pre-existing hook moved aside by Ensure.
const preSuffix = ".pre-cg"

// Hooks are the client-side git hooks an extension may contribute to.
var Hooks = []string{
	"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit",
	"post-checkout", "post-merge", "post-rewrite", "pre-push", "pre-rebase",
}

// Valid reports whether hook is one of Hooks.
func Valid(hook string) bool {
	for _, h := range Hooks {
		if h == hook {
			return true
		}
	}
	return false
}

// ErrHooksPath reports that core.hooksPath is set. cg then installs nothing,
// rather than write into a directory that may be versioned or shared.
var ErrHooksPath = errors.New("core.hooksPath is set")

// dispatcher runs <hook>.pre-cg, then every approved script in the
// workspace's .cg/git-hooks/<hook>.d/, each with the hook's arguments and its
// stdin (buffered once, since several scripts read it). The workspace root and
// the approved scripts come from cg-hooks.allow. A script that is not listed,
// or whose content changed, is skipped with a warning. A script without the
// execute bit (committed from Windows, or with core.fileMode=false) runs
// through sh. The dispatcher exits non-zero if any script failed, which only
// matters for pre-* hooks.
const dispatcher = `#!/bin/sh
` + marker + `
# Installed by cg: runs the approved scripts extensions contribute in
# .cg/git-hooks/<hook>.d/. cg rewrites or removes this file; a hook that was
# here before cg is kept as <hook>.pre-cg and runs first.
hook=$(basename "$0")
input=$(mktemp) || exit 1
trap 'rm -f "$input"' EXIT
cat > "$input"
status=0
if [ -x "$0.pre-cg" ]; then
	"$0.pre-cg" "$@" < "$input" || status=$?
fi
common=$(git rev-parse --path-format=absolute --git-common-dir) || exit $status
allow="$common/` + AllowFile + `"
[ -f "$allow" ] || exit $status
root=$(sed -n 's/^root //p' "$allow")
for script in "$root/.cg/git-hooks/$hook.d"/*; do
	[ -f "$script" ] || continue
	rel=".cg/git-hooks/$hook.d/$(basename "$script")"
	oid=$(git hash-object --no-filters -- "$script") || continue
	if ! grep -qxF "$oid $rel" "$allow"; then
		echo "cg: not running $rel: it is not an approved extension script (cg extension install or update approves it)" >&2
		continue
	fi
	if [ -x "$script" ]; then
		"$script" "$@" < "$input" || status=$?
	else
		sh "$script" "$@" < "$input" || status=$?
	fi
done
exit $status
`

// Result reports what Ensure found.
type Result struct {
	// Unapproved lists the workspace-relative scripts that will not run because
	// they do not match an installed extension's clone.
	Unapproved []string
}

// BlobID returns git's object id for the content of file, as the dispatcher
// computes it (git hash-object --no-filters), using the object format of the
// repository at dir.
func BlobID(ctx context.Context, dir, file string) (string, error) {
	return gitx.Run(ctx, dir, "hash-object", "--no-filters", "--", file)
}

// MainWorktree returns the main worktree of the repository containing dir —
// the first entry git lists, or for a separate git dir (git init
// --separate-git-dir), where git lists the git dir itself, dir's own toplevel.
func MainWorktree(ctx context.Context, dir string) (string, error) {
	trees, err := gitx.WorktreeList(ctx, dir)
	if err != nil {
		return "", err
	}
	if len(trees) == 0 || trees[0].Path == "" {
		return "", errors.New("git worktree list returned no main worktree")
	}
	// With a separate git dir, git lists the git dir itself as the main
	// worktree; the real one is then only known from inside it.
	if common, err := gitx.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil &&
		filepath.Clean(common) == filepath.Clean(trees[0].Path) {
		return gitx.Run(ctx, dir, "rev-parse", "--show-toplevel")
	}
	return trees[0].Path, nil
}

// Ensure reconciles the dispatchers of the repository containing dir with the
// scripts under its main worktree's .cg/git-hooks. trusted maps each
// workspace-relative script path an installed extension contributes to the
// blob id of that script in the extension's clone; only scripts whose content
// matches are approved. It returns ErrHooksPath (and changes nothing) when
// core.hooksPath is set and there are approved scripts to run.
//
// It always works on the main worktree, which is where the dispatcher reads
// scripts from: a script added in a session worktree takes effect once landed.
func Ensure(ctx context.Context, dir string, trusted map[string]string) (Result, error) {
	var res Result
	root, err := MainWorktree(ctx, dir)
	if err != nil {
		return res, nil // not a git repository: nothing to run hooks for
	}
	approved := map[string]string{}
	wanted := map[string]bool{}
	for _, h := range Hooks {
		rels, err := scripts(root, h)
		if err != nil {
			return res, err
		}
		for _, rel := range rels {
			oid, herr := BlobID(ctx, root, filepath.Join(root, filepath.FromSlash(rel)))
			if herr == nil && oid != "" && trusted[rel] == oid {
				approved[rel] = oid
				wanted[h] = true
			} else {
				res.Unapproved = append(res.Unapproved, rel)
			}
		}
	}
	if hp, _ := gitx.Run(ctx, root, "config", "--get", "core.hooksPath"); hp != "" {
		if len(wanted) > 0 {
			return res, ErrHooksPath
		}
		return res, nil
	}
	common, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return res, err
	}
	if err := writeAllow(filepath.Join(common, AllowFile), root, approved); err != nil {
		return res, err
	}
	hooksDir, err := gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		return res, err
	}
	for _, h := range Hooks {
		path := filepath.Join(hooksDir, h)
		if wanted[h] {
			if err := install(path); err != nil {
				return res, fmt.Errorf("installing %s hook: %w", h, err)
			}
		} else if err := uninstall(path); err != nil {
			return res, fmt.Errorf("removing %s hook: %w", h, err)
		}
	}
	return res, nil
}

// scripts returns the workspace-relative paths of the regular files in
// root/.cg/git-hooks/<hook>.d/.
func scripts(root, hook string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(DirRel), hook+".d"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rels []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			rels = append(rels, DirRel+"/"+hook+".d/"+e.Name())
		}
	}
	return rels, nil
}

// writeAllow writes the approved-scripts list: the workspace root, then one
// "<blob id> <path>" line per approved script.
func writeAllow(path, root string, approved map[string]string) error {
	lines := make([]string, 0, 1+len(approved))
	lines = append(lines, "root "+root)
	for rel, oid := range approved {
		lines = append(lines, oid+" "+rel)
	}
	sort.Strings(lines[1:])
	return writeAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// writeAtomic writes data to path through a uniquely named temp file in the
// same directory, so concurrent writers never see each other's partial file.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".cg-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, mode)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
	}
	return werr
}

func isDispatcher(path string) bool {
	b, err := os.ReadFile(path) //nolint:gosec // path is under the repository's hooks dir
	return err == nil && strings.Contains(string(b), marker)
}

// exists reports whether path exists, without following a symlink: a
// dangling symlink hook still counts as a hook to keep.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// install writes the dispatcher at path, moving a foreign hook aside first.
// The move never overwrites: os.Link fails when <hook>.pre-cg exists, so two
// concurrent runs cannot clobber a user's hook with a dispatcher.
func install(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if exists(path) && !isDispatcher(path) {
		pre := path + preSuffix
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// A hard link to a symlink is not portable; rename, refusing
			// to overwrite.
			if exists(pre) {
				return fmt.Errorf("both %s and %s exist; move one of them away", path, pre)
			}
			if err := os.Rename(path, pre); err != nil {
				return err
			}
		} else if err := os.Link(path, pre); err != nil {
			if exists(pre) {
				return fmt.Errorf("both %s and %s exist; move one of them away", path, pre)
			}
			return err
		} else if err := os.Remove(path); err != nil {
			return err
		}
	}
	if b, err := os.ReadFile(path); err == nil && string(b) == dispatcher { //nolint:gosec // see isDispatcher
		return os.Chmod(path, 0o755) //nolint:gosec // hooks must be executable
	}
	return writeAtomic(path, []byte(dispatcher), 0o755)
}

// uninstall removes a dispatcher at path and restores a hook moved aside.
// A hook cg did not write is left alone.
func uninstall(path string) error {
	if exists(path) {
		if !isDispatcher(path) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	pre := path + preSuffix
	if !exists(pre) {
		return nil
	}
	if info, err := os.Lstat(pre); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if exists(path) {
			return nil
		}
		return os.Rename(pre, path)
	}
	// Link, not rename: never overwrite a hook another run just wrote.
	if err := os.Link(pre, path); err != nil {
		if exists(path) {
			return nil
		}
		return err
	}
	return os.Remove(pre)
}
