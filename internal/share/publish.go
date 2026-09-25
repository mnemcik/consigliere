package share

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// PublishTrailer marks a commit as written by `cg share publish`. A branch
// whose tip lacks it has been changed by someone else, and publishing refuses
// to overwrite that.
const PublishTrailer = "Cg-Share-Publish: 1"

// PublishOptions describes one audience's publish.
type PublishOptions struct {
	Audience  string
	Repo      string
	Branch    string
	Owner     string
	Generator string // e.g. "cg 1.19.0"
	// Exports holds the clean, rendered export of every project the audience
	// shares, keyed by slug. The caller guarantees none has open findings.
	Exports map[string]*Result
	// WorkspaceRoot is the owner's workspace, used to refuse a share repo
	// that shares history with it.
	WorkspaceRoot string
	// AuthorName and AuthorEmail, when set, author the publish commit.
	AuthorName  string
	AuthorEmail string
}

// ProjectChange summarises what a publish does to one project folder.
type ProjectChange struct {
	First   bool // not in the share repo before
	Added   []string
	Changed []string
	Removed []string
}

// PublishPlan is a prepared publish: the share branch cloned, the new tree
// written and staged, nothing committed or pushed. Close removes the clone.
type PublishPlan struct {
	Audience string
	Repo     string
	Branch   string
	// Dir is the staged clone, for the owner to review before confirming.
	Dir string
	// Old is the manifest on the branch before this publish (nil if none).
	Old *Manifest
	// Projects maps each shared slug to its change; unchanged projects have
	// an empty change.
	Projects map[string]ProjectChange
	// Removed lists projects published before and no longer shared.
	Removed []string
	// Foreign lists files on the branch that cg did not write (a repo created
	// with a README, say). Publishing replaces them.
	Foreign []string
	// Changed reports whether the new tree differs from the branch at all.
	Changed bool

	opts *PublishOptions
	tmp  string
}

// FirstPublish reports whether the plan publishes a project the audience has
// never had, or replaces content cg did not write. Either needs the owner's
// review at a terminal.
func (p *PublishPlan) FirstPublish() bool {
	if len(p.Foreign) > 0 {
		return true
	}
	for _, c := range p.Projects {
		if c.First {
			return true
		}
	}
	return false
}

// Close removes the staged clone.
func (p *PublishPlan) Close() {
	if p.tmp != "" {
		_ = os.RemoveAll(p.tmp)
	}
}

// PreparePublish clones the audience's share branch, checks it is safe to
// publish to, and stages the new tree. It never commits or pushes.
func PreparePublish(ctx context.Context, opts *PublishOptions) (plan *PublishPlan, err error) {
	if strings.HasPrefix(opts.Repo, "-") {
		return nil, fmt.Errorf("share repo %q: a repo URL cannot start with '-'", opts.Repo)
	}
	for slug, res := range opts.Exports {
		if len(res.Findings) > 0 {
			return nil, fmt.Errorf("project %s has %d open scan finding(s); nothing can be published until they are resolved", slug, len(res.Findings))
		}
	}

	tmp, err := os.MkdirTemp("", "cg-share-publish-")
	if err != nil {
		return nil, err
	}
	staged := &PublishPlan{Audience: opts.Audience, Repo: opts.Repo, Branch: opts.Branch, Dir: filepath.Join(tmp, "repo"), opts: opts, tmp: tmp}
	// Remove the clone on any error. The plan is held in a local, because a
	// `return nil, err` has already cleared the named result when this runs.
	defer func() {
		if err != nil {
			staged.Close()
		}
	}()
	plan = staged

	branchExists, err := cloneForPublish(ctx, opts.Repo, opts.Branch, plan.Dir)
	if err != nil {
		return nil, err
	}
	if err := refuseWorkspaceHistory(ctx, plan.Dir, opts.WorkspaceRoot); err != nil {
		return nil, err
	}
	if branchExists {
		if err := plan.readExisting(ctx); err != nil {
			return nil, err
		}
	}
	if err := plan.stage(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

// cloneForPublish clones branch of repo into dest, or, when the branch does
// not exist yet (an empty repo, or a new branch), prepares an unborn branch.
func cloneForPublish(ctx context.Context, repo, branch, dest string) (branchExists bool, err error) {
	env := readEnv(ctx)
	heads, err := gitx.RunEnv(ctx, "", env, "ls-remote", "--heads", repo, "refs/heads/"+branch)
	if err != nil {
		return false, fmt.Errorf("reading share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	if strings.TrimSpace(heads) != "" {
		if _, err := gitx.RunEnv(ctx, "", env, "clone", "--quiet", "--single-branch", "--branch", branch, "--", repo, dest); err != nil {
			return false, fmt.Errorf("cloning share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
		}
		return true, nil
	}
	if _, err := gitx.RunEnv(ctx, "", env, "clone", "--quiet", "--", repo, dest); err != nil {
		return false, fmt.Errorf("cloning share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	if _, err := gitx.Run(ctx, dest, "checkout", "--quiet", "--orphan", branch); err != nil {
		return false, err
	}
	// An orphan branch starts with the previous HEAD's files staged; start
	// from nothing. On an empty repo there is nothing to remove.
	_, _ = gitx.Run(ctx, dest, "rm", "-r", "-q", "--cached", "--ignore-unmatch", ".")
	return false, nil
}

// refuseWorkspaceHistory refuses a share repo that shares a root commit with
// the workspace: it is the workspace itself (or a fork of it), whatever its
// URL looks like. This is the check URL comparison cannot make reliable.
func refuseWorkspaceHistory(ctx context.Context, shareDir, workspaceRoot string) error {
	if workspaceRoot == "" {
		return nil
	}
	shareRoots := rootCommits(ctx, shareDir, "--all")
	if len(shareRoots) == 0 {
		return nil
	}
	for r := range rootCommits(ctx, workspaceRoot, "--all") {
		if shareRoots[r] {
			return fmt.Errorf("the share repo shares history with this workspace (root commit %.12s); a share repo must be a separate repo", r)
		}
	}
	return nil
}

func rootCommits(ctx context.Context, dir, rev string) map[string]bool {
	out, err := gitx.Run(ctx, dir, "rev-list", "--max-parents=0", rev)
	roots := map[string]bool{}
	if err != nil {
		return roots
	}
	for _, r := range strings.Fields(out) {
		roots[r] = true
	}
	return roots
}

// readExisting loads the manifest on the branch and checks the branch tip was
// written by cg.
func (p *PublishPlan) readExisting(ctx context.Context) error {
	if data, err := gitx.Run(ctx, p.Dir, "show", "HEAD:"+ManifestFile); err == nil {
		var m Manifest
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return fmt.Errorf("share repo: %s is not valid: %w", ManifestFile, err)
		}
		switch {
		case m.Version < 1:
			return fmt.Errorf("share repo: %s has no schema version; it was not written by cg", ManifestFile)
		case m.Version > ManifestVersion:
			return fmt.Errorf("share repo: manifest version %d is newer than this cg understands (%d); update cg", m.Version, ManifestVersion)
		case m.Audience != p.Audience:
			return fmt.Errorf("share repo holds audience %q, not %q; refusing to publish over another audience", m.Audience, p.Audience)
		}
		p.Old = &m
	}

	msg, err := gitx.Run(ctx, p.Dir, "log", "-1", "--format=%B")
	if err != nil {
		return err
	}
	if strings.Contains(msg, PublishTrailer) {
		return nil
	}
	if p.Old != nil {
		return fmt.Errorf("the share repo has commits cg did not make on top of its last publish; resolve them in the share repo first (cg never force-pushes)")
	}
	// Never published, but not empty: e.g. created with a README. Publishing
	// replaces that content, which the first-publish review shows.
	files, err := gitx.Run(ctx, p.Dir, "ls-files")
	if err != nil {
		return err
	}
	p.Foreign = strings.Fields(files)
	return nil
}

// stage replaces the working tree with the new content and stages it.
func (p *PublishPlan) stage(ctx context.Context) error {
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(p.Dir, e.Name())); err != nil {
			return err
		}
	}

	files := map[string]string{}
	manifest := Manifest{
		Version: ManifestVersion, Generator: p.opts.Generator,
		Owner: p.opts.Owner, Audience: p.Audience,
		Projects: map[string]PublishedProject{},
	}
	for slug, res := range p.opts.Exports {
		for path, body := range res.Files {
			files[path] = body
		}
		manifest.Projects[slug] = PublishedEntry(res)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	files[ManifestFile] = string(data) + "\n"
	files["README.md"] = indexReadme(&manifest)
	if err := writeTree(p.Dir, files); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, p.Dir, "add", "-A"); err != nil {
		return err
	}
	return p.diff(ctx, &manifest)
}

// diff fills Projects, Removed and Changed from the staged tree.
func (p *PublishPlan) diff(ctx context.Context, next *Manifest) error {
	status, err := gitx.Run(ctx, p.Dir, "status", "--porcelain", "--untracked-files=all", "--no-renames")
	if err != nil {
		return err
	}
	p.Changed = strings.TrimSpace(status) != ""

	p.Projects = map[string]ProjectChange{}
	for slug := range next.Projects {
		c := ProjectChange{First: p.Old == nil || !hasProject(p.Old, slug)}
		p.Projects[slug] = c
	}
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := strings.TrimSpace(line[:2]), line[3:]
		slug, name, ok := strings.Cut(path, "/")
		if !ok {
			continue // README.md / manifest at the root
		}
		c, shared := p.Projects[slug]
		if !shared {
			continue
		}
		switch code {
		case "A", "??":
			c.Added = append(c.Added, name)
		case "D":
			c.Removed = append(c.Removed, name)
		default:
			c.Changed = append(c.Changed, name)
		}
		p.Projects[slug] = c
	}
	if p.Old != nil {
		for slug := range p.Old.Projects {
			if _, ok := next.Projects[slug]; !ok {
				p.Removed = append(p.Removed, slug)
			}
		}
		sort.Strings(p.Removed)
	}
	return nil
}

func hasProject(m *Manifest, slug string) bool {
	_, ok := m.Projects[slug]
	return ok
}

// Commit commits the staged tree and pushes it, fast-forward only. It returns
// the pushed commit. A push the remote rejects (someone published meanwhile)
// is an error; cg never force-pushes.
func (p *PublishPlan) Commit(ctx context.Context) (string, error) {
	slugs := make([]string, 0, len(p.Projects))
	for s := range p.Projects {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	msg := fmt.Sprintf("publish: %s\n\n%s\n", strings.Join(slugs, ", "), PublishTrailer)

	args := []string{}
	if p.opts.AuthorName != "" {
		args = append(args, "-c", "user.name="+p.opts.AuthorName)
	}
	if p.opts.AuthorEmail != "" {
		args = append(args, "-c", "user.email="+p.opts.AuthorEmail)
	}
	args = append(args, "commit", "--quiet", "--no-verify", "-m", msg)
	if _, err := gitx.Run(ctx, p.Dir, args...); err != nil {
		return "", err
	}
	sha, err := gitx.Run(ctx, p.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := gitx.RunEnv(ctx, p.Dir, readEnv(ctx), "push", "--quiet", "origin", "HEAD:refs/heads/"+p.Branch); err != nil {
		return "", fmt.Errorf("pushing to %s: %s (someone may have published meanwhile; run cg share status and retry)", RedactURL(p.Repo), RedactURL(err.Error()))
	}
	return sha, nil
}

// indexReadme is the share repo's generated front page. It is deterministic,
// so an unchanged audience republishes byte-identically.
func indexReadme(m *Manifest) string {
	slugs := make([]string, 0, len(m.Projects))
	for s := range m.Projects {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "# Shared projects: %s\n\n", m.Audience)
	_, _ = fmt.Fprintf(&b, "Read-only copies published by %s from their Consigliere workspace. "+
		"This repository is generated; edits here are overwritten by the next publish, so contact the owner instead.\n\n", m.Owner)
	b.WriteString("| Project | Source commit | As of |\n|---|---|---|\n")
	for _, s := range slugs {
		p := m.Projects[s]
		_, _ = fmt.Fprintf(&b, "| [%s](%s/README.md) | `%.12s` | %s |\n", s, s, p.SourceCommit, p.SourceDate)
	}
	return b.String()
}
