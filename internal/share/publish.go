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

// AddsFiles reports whether the plan adds a file to a project the audience
// already has — a newly included log.md, say. Unlike a changed file, which
// keeps its previous published form for comparison, an added file is content
// the audience has never seen, so it gets the same review as a first publish.
func (p *PublishPlan) AddsFiles() bool {
	for _, c := range p.Projects {
		if !c.First && len(c.Added) > 0 {
			return true
		}
	}
	return false
}

// NeedsReview reports whether the owner must review the staged content at an
// interactive terminal before publishing: a first publish, or new files in an
// already-published project.
func (p *PublishPlan) NeedsReview() bool {
	return p.FirstPublish() || p.AddsFiles()
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

	branchExists, tips, err := cloneForPublish(ctx, opts.Repo, opts.Branch, plan.Dir)
	if err != nil {
		return nil, err
	}
	if err := refuseWorkspaceHistory(ctx, plan.Dir, opts.WorkspaceRoot, tips); err != nil {
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

// cloneForPublish clones the share repo into dest with every branch and tag
// (commits, no file contents until checkout), so the history check sees the
// whole repo, then checks out branch, or prepares it as an unborn branch when
// it does not exist yet (an empty repo, or a new branch). It returns the tip
// of every branch and tag the remote advertises.
func cloneForPublish(ctx context.Context, repo, branch, dest string) (branchExists bool, tips []string, err error) {
	env := readEnv(ctx)
	refs, err := gitx.RunEnv(ctx, "", env, "ls-remote", "--heads", "--tags", repo)
	if err != nil {
		return false, nil, fmt.Errorf("reading share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	for _, line := range strings.Split(refs, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		tips = append(tips, sha)
		if ref == "refs/heads/"+branch {
			branchExists = true
		}
	}

	if _, err := gitx.RunEnv(ctx, "", env, "clone", "--quiet", "--no-checkout", "--filter=blob:none", "--", repo, dest); err != nil {
		return false, nil, fmt.Errorf("cloning share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	if branchExists {
		if _, err := gitx.RunEnv(ctx, dest, env, "checkout", "--quiet", "-B", branch, "origin/"+branch); err != nil {
			return false, nil, fmt.Errorf("checking out %s: %s", branch, RedactURL(err.Error()))
		}
		return true, tips, nil
	}
	if _, err := gitx.Run(ctx, dest, "checkout", "--quiet", "--orphan", branch); err != nil {
		return false, nil, err
	}
	// Start from nothing: an orphan branch may inherit the previous HEAD's
	// index. On an empty repo there is nothing to remove.
	_, _ = gitx.Run(ctx, dest, "rm", "-r", "-q", "--cached", "--ignore-unmatch", ".")
	return false, tips, nil
}

// refuseWorkspaceHistory refuses a share repo that is the workspace itself, or
// a fork of it, whatever its URL looks like — the check URL comparison cannot
// make reliable. Two independent signals, over every branch and tag of the
// share repo: a root commit shared with the workspace, or a branch or tag tip
// the workspace already has. A shallow workspace cannot show its real roots,
// so it is refused rather than half-checked.
func refuseWorkspaceHistory(ctx context.Context, shareDir, workspaceRoot string, tips []string) error {
	if workspaceRoot == "" {
		return nil
	}
	if shallow, _ := gitx.Run(ctx, workspaceRoot, "rev-parse", "--is-shallow-repository"); shallow == "true" {
		return fmt.Errorf("this workspace is a shallow clone, so cg cannot verify the share repo is separate from it; run git fetch --unshallow first")
	}
	for _, sha := range tips {
		if gitx.CommitishExists(ctx, workspaceRoot, sha) {
			return fmt.Errorf("the share repo has a branch or tag at commit %.12s, which is in this workspace's history; a share repo must be a separate repo", sha)
		}
	}
	// A failed enumeration must not read as "no roots": that would pass the
	// check without having made it.
	shareRoots, err := rootCommits(ctx, shareDir, "--all")
	if err != nil {
		return fmt.Errorf("listing the share repo's root commits: %w", err)
	}
	workspaceRoots, err := rootCommits(ctx, workspaceRoot, "--all")
	if err != nil {
		return fmt.Errorf("listing this workspace's root commits: %w", err)
	}
	for r := range workspaceRoots {
		if shareRoots[r] {
			return fmt.Errorf("the share repo shares history with this workspace (root commit %.12s); a share repo must be a separate repo", r)
		}
	}
	return nil
}

func rootCommits(ctx context.Context, dir, rev string) (map[string]bool, error) {
	out, err := gitx.Run(ctx, dir, "rev-list", "--max-parents=0", rev)
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	for _, r := range strings.Fields(out) {
		roots[r] = true
	}
	return roots, nil
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
	files, err := lsFiles(ctx, p.Dir)
	if err != nil {
		return err
	}
	p.Foreign = files
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
	// --force: a user's global gitignore must not silently drop a published
	// file while the manifest still lists it.
	if _, err := gitx.Run(ctx, p.Dir, "add", "-A", "--force"); err != nil {
		return err
	}
	staged, err := lsFiles(ctx, p.Dir)
	if err != nil {
		return err
	}
	if len(staged) != len(files) {
		return fmt.Errorf("staged %d file(s) for %d rendered; refusing to publish an incomplete tree", len(staged), len(files))
	}
	for _, f := range staged {
		if _, ok := files[f]; !ok {
			return fmt.Errorf("unexpected file %q staged; refusing to publish", f)
		}
	}
	return p.diff(ctx, &manifest)
}

// diff fills Projects, Removed and Changed from the staged tree.
func (p *PublishPlan) diff(ctx context.Context, next *Manifest) error {
	// -z: paths are NUL-terminated and never quoted, so names with spaces or
	// non-ASCII characters parse like any other.
	status, err := gitx.Run(ctx, p.Dir, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return err
	}
	p.Changed = strings.Trim(status, "\x00 ") != ""

	p.Projects = map[string]ProjectChange{}
	for slug := range next.Projects {
		c := ProjectChange{First: p.Old == nil || !hasProject(p.Old, slug)}
		p.Projects[slug] = c
	}
	for _, line := range strings.Split(status, "\x00") {
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

	// A user's global hooks and signing must not run in cg's clone: a
	// prepare-commit-msg hook can drop the trailer (locking the owner out of
	// the next publish), a pre-push hook can block the push, and signing can
	// prompt. hooksPath points at an empty directory, which works everywhere.
	noHooks := filepath.Join(p.tmp, "no-hooks")
	if err := os.MkdirAll(noHooks, 0o700); err != nil {
		return "", err
	}
	args := []string{"-c", "core.hooksPath=" + noHooks, "-c", "commit.gpgsign=false"}
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
	if body, err := gitx.Run(ctx, p.Dir, "log", "-1", "--format=%B"); err != nil || !strings.Contains(body, PublishTrailer) {
		return "", fmt.Errorf("the publish commit lost its %q trailer; not pushing", PublishTrailer)
	}
	sha, err := gitx.Run(ctx, p.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := gitx.RunEnv(ctx, p.Dir, readEnv(ctx), "-c", "core.hooksPath="+noHooks, "push", "--quiet", "origin", "HEAD:refs/heads/"+p.Branch); err != nil {
		msg := RedactURL(err.Error())
		if strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "fetch first") || strings.Contains(msg, "[rejected]") {
			return "", fmt.Errorf("pushing to %s was rejected: the share repo changed since it was read (someone published meanwhile?); run cg share status and retry", RedactURL(p.Repo))
		}
		return "", fmt.Errorf("pushing to %s: %s", RedactURL(p.Repo), msg)
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

// lsFiles lists the index, NUL-separated so any file name parses.
func lsFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := gitx.Run(ctx, dir, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}
