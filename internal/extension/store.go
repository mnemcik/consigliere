package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// The extension store is machine-shared and keyed by source and commit, so one
// workspace's install or update can never change what another workspace reads:
//
//	extensions/mirrors/<key>.git      one bare mirror per repo URL, for git operations
//	extensions/trees/<key>@<commit>/  one exported tree per commit, never modified
//
// <key> is derived from the repo URL (see repoKey). A workspace records the
// commit it installed in .cg.json (ExtensionRef.Commit) and reads the extension
// from <tree>/<path>. Entries without a commit predate the store and are read
// from the legacy name-keyed clone (CloneDir) until their next install or update.

// commitRe matches a full SHA-1 or SHA-256 object name.
var commitRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// ValidCommit reports whether c is a full commit object name. Commits read from
// .cg.json are checked before they become part of a path.
func ValidCommit(c string) bool {
	return commitRe.MatchString(c)
}

// repoKey names a repo in the store: its last path segment, made filename-safe,
// plus 12 hex characters of the URL's SHA-256. The readable part is for people;
// the hash makes keys collision-safe and free of path traversal.
func repoKey(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	base := strings.TrimSuffix(path.Base(strings.TrimRight(filepath.ToSlash(repo), "/")), ".git")
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "repo"
	}
	return name + "-" + hex.EncodeToString(sum[:6])
}

// MirrorDir is the bare mirror of repo.
func MirrorDir(repo string) string {
	return filepath.Join(ExtensionsDir(), "mirrors", repoKey(repo)+".git")
}

// TreeDir is the exported tree of repo at commit. commit must satisfy
// ValidCommit.
func TreeDir(repo, commit string) string {
	return filepath.Join(ExtensionsDir(), "trees", repoKey(repo)+"@"+commit)
}

// SourceRoot is the root of the extension source a workspace entry reads: the
// tree at its recorded commit, or the legacy clone for an entry that predates
// the store. The manifest is at <SourceRoot>/<ref.Path>.
func SourceRoot(ref *workspace.ExtensionRef) string {
	if ValidCommit(ref.Commit) {
		return TreeDir(ref.Repo, ref.Commit)
	}
	return CloneDir(ref.Name)
}

// EnsureMirror makes sure repo's mirror exists, creating it, and fetches into
// an existing one when fetch is true. It returns the mirror's path.
//
// The mirror holds only branches and tags. A full mirror clone would also copy
// refs such as GitHub's refs/pull/*, which carry commits from unmerged forks:
// a commit recorded in a pulled .cg.json could then select fork code under a
// trusted repo URL.
func EnsureMirror(ctx context.Context, repo string, fetch bool) (string, error) {
	dir := MirrorDir(repo)
	if dirExists(dir) {
		if fetch {
			if err := fetchMirror(ctx, dir); err != nil {
				return "", err
			}
		}
		return dir, nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil { //nolint:gosec // user config dir
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staging) }() // no-op once renamed
	const config = "config"
	for _, args := range [][]string{
		{"init", "--quiet", "--bare"},
		{config, "remote.origin.url", repo},
		{config, "remote.origin.fetch", "+refs/heads/*:refs/heads/*"},
		{config, "--add", "remote.origin.fetch", "+refs/tags/*:refs/tags/*"},
	} {
		if _, err := gitx.Run(ctx, staging, args...); err != nil {
			return "", err
		}
	}
	if err := fetchMirror(ctx, staging); err != nil {
		return "", err
	}
	if err := os.Rename(staging, dir); err != nil {
		// A concurrent install created it first; use theirs.
		if dirExists(dir) {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

// fetchMirror fetches the mirror's branches and tags, pruning deleted ones, and
// points its HEAD at the remote's default branch, so "" keeps resolving to the
// default branch after an upstream rename.
func fetchMirror(ctx context.Context, dir string) error {
	if _, err := gitx.Run(ctx, dir, "fetch", "--quiet", "--prune", "origin"); err != nil {
		return err
	}
	out, err := gitx.Run(ctx, dir, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return nil // HEAD is a convenience; the fetch itself succeeded
	}
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "ref: "); ok {
			if target, _, ok := strings.Cut(rest, "\t"); ok && strings.HasPrefix(target, "refs/heads/") {
				_, _ = gitx.Run(ctx, dir, "symbolic-ref", "HEAD", target)
			}
			break
		}
	}
	return nil
}

// ResolveCommit resolves ref (a tag, branch or commit; "" means the default
// branch) to a full commit in the mirror.
func ResolveCommit(ctx context.Context, mirror, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	out, err := gitx.Run(ctx, mirror, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || !ValidCommit(out) {
		return "", fmt.Errorf("ref %q not found", ref)
	}
	if !Reachable(ctx, mirror, out) {
		return "", fmt.Errorf("commit %s is not on any branch or tag", out[:12])
	}
	return out, nil
}

// Reachable reports whether commit is on a branch or tag of the mirror. Only
// such commits are installed: a commit the mirror still holds after upstream
// dropped it (a force push, a deleted branch) is no longer published code.
func Reachable(ctx context.Context, mirror, commit string) bool {
	out, err := gitx.Run(ctx, mirror, "for-each-ref", "--count=1", "--format=%(refname)", "--contains", commit, "refs/heads", "refs/tags")
	return err == nil && out != ""
}

// HasCommit reports whether the mirror holds commit.
func HasCommit(ctx context.Context, mirror, commit string) bool {
	_, err := gitx.Run(ctx, mirror, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

// EnsureTree exports repo at commit into the store, unless it is there already,
// and returns the tree's path. The tree is checked out into a staging directory
// through a private index, then renamed into place, so a reader never sees a
// partial tree and the mirror is left untouched.
func EnsureTree(ctx context.Context, repo, mirror, commit string) (string, error) {
	if !ValidCommit(commit) {
		return "", fmt.Errorf("invalid commit %q", commit)
	}
	dir := TreeDir(repo, commit)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil { //nolint:gosec // user config dir
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staging) }() // no-op once renamed
	// A private index beside the staging dir, so the tree can hold any file
	// name and concurrent exports never share an index.
	index := staging + ".index"
	defer func() { _ = os.Remove(index) }()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := gitx.RunEnv(ctx, mirror, env, "read-tree", commit); err != nil {
		return "", err
	}
	// The tree holds the repo's own bytes on every machine: the user's
	// core.autocrlf (on by default with Git for Windows) would otherwise write
	// CRLF, which breaks shell scripts and makes the tree differ per machine.
	// The extension's own .gitattributes still apply.
	if _, err := gitx.RunEnv(ctx, mirror, env, "-c", "core.autocrlf=false", "-c", "core.eol=lf",
		"--work-tree="+staging, "checkout-index", "--all"); err != nil {
		return "", err
	}
	if err := os.Rename(staging, dir); err != nil {
		if _, serr := os.Stat(dir); serr == nil {
			return dir, nil // a concurrent install exported it first
		}
		return "", err
	}
	return dir, nil
}

// FetchTree returns the tree of repo at commit, creating the mirror and
// fetching only when the commit is not available locally.
func FetchTree(ctx context.Context, repo, commit string) (string, error) {
	if !ValidCommit(commit) {
		return "", fmt.Errorf("invalid commit %q", commit)
	}
	if dir := TreeDir(repo, commit); dirExists(dir) {
		return dir, nil
	}
	mirror, err := EnsureMirror(ctx, repo, false)
	if err != nil {
		return "", err
	}
	if !HasCommit(ctx, mirror, commit) || !Reachable(ctx, mirror, commit) {
		if _, err := EnsureMirror(ctx, repo, true); err != nil {
			return "", err
		}
		if !HasCommit(ctx, mirror, commit) || !Reachable(ctx, mirror, commit) {
			return "", errors.New("commit " + commit[:12] + " is not on any branch or tag of " + repo)
		}
	}
	return EnsureTree(ctx, repo, mirror, commit)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
