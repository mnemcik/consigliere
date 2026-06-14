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

// Clone clones url into dest (which must not already exist). When ref is
// non-empty it is checked out after the clone — a branch or a tag. A local
// filesystem path is a valid url, which the extension installer relies on for
// fixture-based tests.
func Clone(ctx context.Context, url, dest, ref string) error {
	if _, err := Run(ctx, "", "clone", "--quiet", url, dest); err != nil {
		return err
	}
	if ref != "" {
		return Checkout(ctx, dest, ref)
	}
	return nil
}

// Checkout checks out ref (a branch or tag) in dir.
func Checkout(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "checkout", "--quiet", ref)
	return err
}

// LatestTag returns the most recent tag reachable in dir, or "" if there are no
// tags (a clean state, not an error).
func LatestTag(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "describe", "--tags", "--abbrev=0")
	if err != nil {
		return ""
	}
	return out
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

// Worktree is one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Branch   string // short branch name; empty when detached
	Head     string // commit object id
	Detached bool
}

// WorktreeList returns every worktree registered for the repository containing
// dir, with its checked-out branch (or detached HEAD), parsed from
// `git worktree list --porcelain`.
func WorktreeList(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var (
		list []Worktree
		cur  *Worktree
	)
	flush := func() {
		if cur != nil {
			list = append(list, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
		case cur == nil:
			// ignore lines before the first worktree block
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			cur.Detached = true
		}
	}
	flush()
	return list, nil
}

// WorktreeRemove removes the worktree at path (with --force when force is set).
func WorktreeRemove(ctx context.Context, dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := Run(ctx, dir, args...)
	return err
}

// BranchDelete force-deletes the local branch (git branch -D).
func BranchDelete(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "branch", "-D", branch)
	return err
}

// ShowToplevel returns the top-level directory of the working tree containing
// dir, or "" when dir is not inside a git repository.
func ShowToplevel(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return out
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

// SymbolicRefShort returns the short symbolic ref of HEAD (the current branch
// name). It errors when HEAD is detached.
func SymbolicRefShort(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "symbolic-ref", "--short", "HEAD")
}

// IsDetached reports whether HEAD is detached (not on a branch) in dir.
func IsDetached(ctx context.Context, dir string) bool {
	return !ok(ctx, dir, "symbolic-ref", "-q", "HEAD")
}

// RemoteURL returns the configured URL of the named remote in dir.
func RemoteURL(ctx context.Context, dir, remote string) (string, error) {
	return Run(ctx, dir, "remote", "get-url", remote)
}

// DefaultBranch returns the remote's default branch (origin/HEAD with the
// "origin/" prefix stripped), falling back to "main" when it can't be resolved.
func DefaultBranch(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil || out == "" {
		return "main"
	}
	return strings.TrimPrefix(out, "origin/")
}

// CheckoutNewBranch creates branch at HEAD and checks it out.
func CheckoutNewBranch(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "checkout", "-b", branch)
	return err
}

// IsClean reports whether dir's working tree has no unstaged or staged changes.
func IsClean(ctx context.Context, dir string) bool {
	return ok(ctx, dir, "diff", "--quiet") && ok(ctx, dir, "diff", "--cached", "--quiet")
}

// MergeFFOnly fast-forward-merges ref into the current branch of dir, failing
// (rather than creating a merge commit) when a fast-forward isn't possible.
func MergeFFOnly(ctx context.Context, dir, ref string) error {
	_, err := Run(ctx, dir, "merge", "--ff-only", "--quiet", ref)
	return err
}

// CommitishExists reports whether rev resolves to a commit object in dir.
func CommitishExists(ctx context.Context, dir, rev string) bool {
	return ok(ctx, dir, "cat-file", "-e", rev+"^{commit}")
}

// Push runs `git push <remote> <refspec> --quiet` in dir.
func Push(ctx context.Context, dir, remote, refspec string) error {
	_, err := Run(ctx, dir, "push", remote, refspec, "--quiet")
	return err
}

// Rebase runs `git rebase <onto>` in dir.
func Rebase(ctx context.Context, dir, onto string) error {
	_, err := Run(ctx, dir, "rebase", onto)
	return err
}

// ConflictedFiles returns the paths with unresolved merge conflicts in dir.
func ConflictedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// RevParse returns the resolved object id of rev in dir.
func RevParse(ctx context.Context, dir, rev string) (string, error) {
	return Run(ctx, dir, "rev-parse", rev)
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
