// Package worktree implements the per-session git-worktree mechanics promoted
// from the personal-workspace bash helpers (create-session-worktree.sh and
// friends). Each session works on an ephemeral branch "<prefix><slug>" in a
// sibling worktree directory "<root>--<slug>", landing onto the landing branch.
package worktree

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// slugRe mirrors the bash validation: a slug starts with an alphanumeric and
// then allows lowercase alphanumerics, dot, underscore, and dash.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidSlug reports whether slug is an acceptable session slug.
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

// Options configures the worktree operations.
type Options struct {
	// Root is the main workspace root (the parent of the shared git dir).
	Root string
	// Prefix is the path prefix for the worktree directory; the worktree lives
	// at "<Prefix>--<slug>". Empty means use Root.
	Prefix string
	// BranchPrefix prepends the slug to form the branch name (e.g. "session/").
	BranchPrefix string
	// LandingBranch is the branch sessions land onto (e.g. "main"). The remote
	// tracking ref "origin/<LandingBranch>" is the "is it landed?" reference,
	// or the local branch itself under the local strategy.
	LandingBranch string
	// Local is set when the effective landing strategy (see EffectiveStrategy)
	// is StrategyLocal: no fetch, and the local landing branch is the
	// reference.
	Local bool
	// Force proceeds even when the branch/worktree has unlanded commits.
	Force bool
}

func (o Options) worktreePath(slug string) string {
	prefix := o.Prefix
	switch {
	case prefix == "":
		prefix = o.Root
	case !filepath.IsAbs(prefix):
		// A relative configured prefix is resolved against the workspace root, so
		// the worktree path stays absolute and matches the paths git reports.
		prefix = filepath.Join(o.Root, prefix)
	}
	return prefix + "--" + slug
}

func (o Options) branch(slug string) string { return o.BranchPrefix + slug }

func (o Options) landingRef() string {
	if o.Local {
		return "refs/heads/" + o.LandingBranch
	}
	return "origin/" + o.LandingBranch
}

// fetchLanding refreshes origin/<landing> before a landed-check. Under the
// local strategy there is nothing to fetch. Without an origin it fails with a
// usage error that names the fix, instead of git's "does not appear to be a
// git repository".
func (o Options) fetchLanding(ctx context.Context) error {
	if o.Local {
		return nil
	}
	if err := requireOrigin(ctx, o.Root, "a remote strategy"); err != nil {
		return err
	}
	return gitx.Fetch(ctx, o.Root, "origin", o.LandingBranch)
}

// EffectiveStrategy resolves the landing strategy for the repository at root.
// An explicit value (the --strategy flag, else worktree.landingStrategy in
// .cg.json) always wins. When none is set, a repository without an origin
// remote lands locally, and one with an origin uses the default.
func EffectiveStrategy(ctx context.Context, root, configured string) string {
	if configured != "" {
		return configured
	}
	if !gitx.HasRemote(ctx, root, "origin") {
		return workspace.StrategyLocal
	}
	return workspace.DefaultLandingStrategy
}

// requireOrigin fails with a usage error when a remote strategy is set
// explicitly on a repository that has no origin remote. what names the
// strategy in the message.
func requireOrigin(ctx context.Context, dir, what string) error {
	if gitx.HasRemote(ctx, dir, "origin") {
		return nil
	}
	return cgerr.New(cgerr.ExitUsage,
		"landing strategy %s needs an origin remote, and this repository has none — "+
			"set worktree.landingStrategy to %q in %s (or remove the setting) to land locally",
		what, workspace.StrategyLocal, workspace.ConfigFile)
}

// Create creates or reuses a session worktree for slug, returning its path.
// Status and warnings are written to logw; the caller prints the returned path
// to stdout. It mirrors the four scenarios of create-session-worktree.sh:
//
//  1. Worktree already exists, clean → reuse, exit 0.
//  2. Worktree exists with unlanded commits → ExitDirty unless Force.
//  3. Orphan branch without a worktree → attach (ExitDirty if ahead and !Force).
//  4. Fresh → create branch + worktree off the landing ref.
func Create(ctx context.Context, slug string, opt Options, logw io.Writer) (string, error) {
	if !ValidSlug(slug) {
		return "", cgerr.New(cgerr.ExitUsage, "invalid slug %q — must match [a-z0-9][a-z0-9._-]*", slug)
	}

	// logf writes a diagnostic line to logw, ignoring write errors (diagnostics
	// must never fail the operation).
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(logw, format, a...) }

	worktreePath := opt.worktreePath(slug)
	branch := opt.branch(slug)
	landing := opt.landingRef()

	if err := opt.fetchLanding(ctx); err != nil {
		return "", err
	}

	// Scenario 1 / 2: worktree exists at the expected path.
	paths, err := gitx.WorktreePaths(ctx, opt.Root)
	if err != nil {
		return "", err
	}
	if containsPath(paths, worktreePath) {
		// A failed rev-walk must not be misread as "clean" — that would bypass
		// the unlanded-commit guard. Surface the error instead.
		unlanded, err := gitx.LogOneline(ctx, worktreePath, landing+"..HEAD")
		if err != nil {
			return "", err
		}
		if unlanded != "" && !opt.Force {
			logf("worktree exists at %s with unlanded commits:\n%s\n", worktreePath, unlanded)
			logf("land them (cd %s && cg worktree land) or re-run with --force to reuse as-is\n", worktreePath)
			return "", cgerr.New(cgerr.ExitDirty, "unlanded commits in existing worktree %s", worktreePath)
		}
		logf("reusing existing worktree at %s\n", worktreePath)
		return worktreePath, nil
	}

	// Scenario 3: orphan branch without a worktree.
	branchRef := "refs/heads/" + branch
	if gitx.RefExists(ctx, opt.Root, branchRef) {
		if !gitx.IsAncestor(ctx, opt.Root, branchRef, landing) {
			if !opt.Force {
				unlanded, err := gitx.LogOneline(ctx, opt.Root, landing+".."+branchRef)
				if err != nil {
					return "", err
				}
				logf("orphan branch %s has unlanded commits:\n%s\n", branch, unlanded)
				logf("delete the branch (git branch -D %s) or re-run with --force to attach anyway\n", branch)
				return "", cgerr.New(cgerr.ExitDirty, "unlanded commits on orphan branch %s", branch)
			}
			logf("attaching worktree to branch %s, which is ahead of %s (--force)\n", branch, landing)
		} else {
			logf("attaching worktree to existing branch %s (at/behind %s)\n", branch, landing)
		}
		if err := gitx.WorktreeAdd(ctx, opt.Root, worktreePath, branch); err != nil {
			return "", err
		}
		return worktreePath, nil
	}

	// Scenario 4: fresh.
	logf("creating fresh worktree + branch %s at %s\n", branch, worktreePath)
	if err := gitx.WorktreeAddNew(ctx, opt.Root, worktreePath, branch, landing); err != nil {
		return "", err
	}
	return worktreePath, nil
}

func containsPath(paths []string, target string) bool {
	target = resolve(target)
	for _, p := range paths {
		if resolve(p) == target {
			return true
		}
	}
	return false
}

// resolve canonicalizes a path's existing symlinks, falling back to a cleaned
// path when it doesn't exist (e.g. the not-yet-created target worktree).
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
