// Package githooks installs the git hooks that extensions contribute.
//
// Contributed scripts live in the workspace, versioned, under
// .cg/git-hooks/<hook>.d/. What runs them is a small dispatcher at
// .git/hooks/<hook>, which git does not version — so a fresh clone has the
// scripts but no dispatcher. Ensure reconciles the two: it installs a
// dispatcher for every hook that has scripts and removes it from every hook
// that has none. It is idempotent and cheap when there is nothing to do, so
// callers run it whenever the workspace may have changed (extension install,
// update and remove, cg init, cg sync, session start).
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
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// DirRel is the workspace-relative directory holding contributed scripts, one
// subdirectory per hook: .cg/git-hooks/<hook>.d/.
const DirRel = ".cg/git-hooks"

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

// dispatcher runs <hook>.pre-cg, then every executable script in
// <root>/.cg/git-hooks/<hook>.d/, each with the hook's arguments and its
// stdin (buffered once, since several scripts read it). It exits non-zero if
// any of them did, which only matters for pre-* hooks.
const dispatcher = `#!/bin/sh
` + marker + `
# Installed by cg: runs the scripts extensions contribute in
# .cg/git-hooks/<hook>.d/. cg rewrites or removes this file; a hook that was
# here before cg is kept as <hook>.pre-cg and runs first.
hook=$(basename "$0")
common=$(git rev-parse --path-format=absolute --git-common-dir) || exit 0
dir="$(dirname "$common")/.cg/git-hooks/$hook.d"
input=$(mktemp) || exit 1
trap 'rm -f "$input"' EXIT
cat > "$input"
status=0
if [ -x "$0.pre-cg" ]; then
	"$0.pre-cg" "$@" < "$input" || status=$?
fi
for script in "$dir"/*; do
	[ -f "$script" ] && [ -x "$script" ] || continue
	"$script" "$@" < "$input" || status=$?
done
exit $status
`

// Ensure reconciles the dispatchers in the repository at root with the
// scripts under root/.cg/git-hooks. It returns ErrHooksPath (and changes
// nothing) when core.hooksPath is set and there are scripts to run.
func Ensure(ctx context.Context, root string) error {
	wanted := map[string]bool{}
	for _, h := range Hooks {
		if hasScripts(filepath.Join(root, filepath.FromSlash(DirRel), h+".d")) {
			wanted[h] = true
		}
	}
	hooksDir, err := hooksDir(ctx, root)
	if err != nil {
		if len(wanted) == 0 {
			return nil // not a git repository, and nothing to install
		}
		return err
	}
	if hp, _ := gitx.Run(ctx, root, "config", "--get", "core.hooksPath"); hp != "" {
		if len(wanted) > 0 {
			return ErrHooksPath
		}
		return nil
	}
	for _, h := range Hooks {
		path := filepath.Join(hooksDir, h)
		if wanted[h] {
			if err := install(path); err != nil {
				return fmt.Errorf("installing %s hook: %w", h, err)
			}
		} else if err := uninstall(path); err != nil {
			return fmt.Errorf("removing %s hook: %w", h, err)
		}
	}
	return nil
}

func hooksDir(ctx context.Context, root string) (string, error) {
	return gitx.Run(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
}

// hasScripts reports whether dir holds at least one regular file.
func hasScripts(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			return true
		}
	}
	return false
}

func isDispatcher(path string) bool {
	b, err := os.ReadFile(path) //nolint:gosec // path is under the repository's hooks dir
	return err == nil && strings.Contains(string(b), marker)
}

// install writes the dispatcher at path, moving a foreign hook aside first.
func install(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !isDispatcher(path) {
		pre := path + preSuffix
		if _, perr := os.Stat(pre); perr == nil {
			return fmt.Errorf("both %s and %s exist; move one of them away", path, pre)
		}
		if err := os.Rename(path, pre); err != nil {
			return err
		}
	}
	if b, err := os.ReadFile(path); err == nil && string(b) == dispatcher { //nolint:gosec // see isDispatcher
		return os.Chmod(path, 0o755) //nolint:gosec // hooks must be executable
	}
	tmp := path + ".cg-tmp"
	if err := os.WriteFile(tmp, []byte(dispatcher), 0o755); err != nil { //nolint:gosec // hooks must be executable
		return err
	}
	return os.Rename(tmp, path)
}

// uninstall removes a dispatcher at path and restores a hook moved aside.
// A hook cg did not write is left alone.
func uninstall(path string) error {
	if _, err := os.Stat(path); err == nil {
		if !isDispatcher(path) {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	pre := path + preSuffix
	if _, err := os.Stat(pre); err == nil {
		return os.Rename(pre, path)
	}
	return nil
}
