// Package gitx is a thin, cross-platform wrapper around the git CLI. It runs
// git via exec.CommandContext (so callers control timeouts/cancellation) and
// replaces the shell helpers' reliance on `git -C`, awk/grep parsing, and
// platform-specific tooling. It shells out to the system git binary rather than
// linking a git library — git is already a hard dependency of every cg workflow.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Run executes `git <args...>` in dir (or the current directory if dir is "")
// and returns trimmed stdout. On failure the error wraps git's stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	// #nosec G204 -- "git" is a fixed binary; args originate internally (never
	// from a shell string), and this is the single controlled choke point for
	// every git invocation in cg.
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		stderr := strings.TrimSpace(errb.String())
		if stderr != "" {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String()), nil
}

// ok reports whether `git <args...>` exits zero in dir. It distinguishes a
// clean non-zero exit (the predicate is false) from git being unavailable.
func ok(ctx context.Context, dir string, args ...string) bool {
	// #nosec G204 -- see Run: fixed "git" binary, internal args.
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run() == nil
}

// Available reports whether a git binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Fetch runs `git fetch <remote> [refs...] --quiet` in dir.
func Fetch(ctx context.Context, dir, remote string, refs ...string) error {
	args := append([]string{"fetch", remote}, refs...)
	args = append(args, "--quiet")
	_, err := Run(ctx, dir, args...)
	return err
}

// RefExists reports whether ref resolves in dir (e.g. "refs/heads/session/x").
func RefExists(ctx context.Context, dir, ref string) bool {
	return ok(ctx, dir, "rev-parse", "--verify", "--quiet", ref)
}

// IsAncestor reports whether commit a is an ancestor of commit b in dir.
func IsAncestor(ctx context.Context, dir, a, b string) bool {
	return ok(ctx, dir, "merge-base", "--is-ancestor", a, b)
}

// LogOneline returns `git log --oneline <revRange>` output (may be empty).
func LogOneline(ctx context.Context, dir, revRange string) (string, error) {
	return Run(ctx, dir, "log", "--oneline", revRange)
}

// WorktreePaths returns the absolute paths of every worktree registered for the
// repository containing dir, parsed from `git worktree list --porcelain`.
func WorktreePaths(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if rest, found := strings.CutPrefix(line, "worktree "); found {
			paths = append(paths, strings.TrimSpace(rest))
		}
	}
	return paths, nil
}

// WorktreeAdd attaches a new worktree at path checked out to an existing branch.
func WorktreeAdd(ctx context.Context, dir, path, branch string) error {
	_, err := Run(ctx, dir, "worktree", "add", path, branch)
	return err
}

// WorktreeAddNew creates a new branch off startPoint and a worktree at path.
func WorktreeAddNew(ctx context.Context, dir, path, branch, startPoint string) error {
	_, err := Run(ctx, dir, "worktree", "add", "-b", branch, path, startPoint)
	return err
}

// CommonRoot returns the main worktree's root directory for the repository
// containing dir — i.e. the parent of the shared git common directory. This is
// the directory the shell helpers referred to as WORKSPACE_ROOT, resolved from
// any worktree rather than hardcoded.
func CommonRoot(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", errors.New("git rev-parse --git-common-dir returned empty output")
	}
	gitDir := out
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	root := filepath.Dir(filepath.Clean(gitDir))
	// Canonicalize symlinks so the root matches the paths git records for its
	// worktrees (git stores resolved paths; on macOS /var -> /private/var).
	// Without this, worktree-path comparisons miss. EvalSymlinks needs the path
	// to exist — the repo root always does — so fall back to root on error.
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil {
		root = resolved
	}
	return root, nil
}
