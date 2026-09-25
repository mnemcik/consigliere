package share

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publishFixture is a committed workspace (pilot + sibling), an exported
// pilot, and an empty bare share repo.
type publishFixture struct {
	ctx  context.Context
	root string
	bare string
}

func newPublishFixture(t *testing.T) *publishFixture {
	t.Helper()
	ctx, root := initWorkspace(t)
	bare := filepath.Join(t.TempDir(), "share.git")
	git(t, ctx, "", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	return &publishFixture{ctx: ctx, root: root, bare: bare}
}

func (f *publishFixture) exports(t *testing.T) map[string]*Result {
	t.Helper()
	res, err := Export(f.ctx, pilotOptions(f.root))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*Result{"pilot": res}
}

func (f *publishFixture) opts(t *testing.T, exports map[string]*Result) *PublishOptions {
	t.Helper()
	return &PublishOptions{
		Audience: "team", Repo: f.bare, Branch: "main", Owner: "Ada",
		Generator: "cg test", Exports: exports, WorkspaceRoot: f.root,
		AuthorName: "Publisher", AuthorEmail: "pub@example.com",
	}
}

func (f *publishFixture) publish(t *testing.T, exports map[string]*Result) *PublishPlan {
	t.Helper()
	plan, err := PreparePublish(f.ctx, f.opts(t, exports))
	if err != nil {
		t.Fatalf("PreparePublish: %v", err)
	}
	t.Cleanup(plan.Close)
	if plan.Changed {
		if _, err := plan.Commit(f.ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}
	return plan
}

// pushForeign adds a commit cg did not make to the share repo's main.
func (f *publishFixture) pushForeign(t *testing.T, file string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "foreign")
	git(t, f.ctx, "", "clone", "--quiet", f.bare, work)
	git(t, f.ctx, work, "config", "user.email", "x@example.com")
	git(t, f.ctx, work, "config", "user.name", "X")
	git(t, f.ctx, work, "config", "commit.gpgsign", "false")
	write(t, work, file, "hand edit\n")
	git(t, f.ctx, work, "add", ".")
	git(t, f.ctx, work, "commit", "--quiet", "-m", "hand edit")
	git(t, f.ctx, work, "push", "--quiet", "origin", "HEAD:main")
}

func TestPublishFirstThenUnchanged(t *testing.T) {
	f := newPublishFixture(t)
	exports := f.exports(t)
	plan := f.publish(t, exports)
	if !plan.FirstPublish() || !plan.Changed || !plan.Projects["pilot"].First {
		t.Fatalf("first publish to an empty repo: %+v", plan)
	}

	m, err := ReadPublished(f.ctx, f.bare, "main")
	if err != nil || m == nil {
		t.Fatalf("ReadPublished after publish: %v %v", m, err)
	}
	if m.Projects["pilot"].ContentHash != ContentHash(exports["pilot"].Files) || m.Generator != "cg test" || m.Audience != "team" {
		t.Errorf("manifest does not describe the export: %+v", m)
	}
	log := git(t, f.ctx, f.bare, "log", "-1", "--format=%an <%ae>%n%B", "main")
	if !strings.Contains(log, "Publisher <pub@example.com>") || !strings.Contains(log, PublishTrailer) {
		t.Errorf("publish commit needs the configured author and the trailer:\n%s", log)
	}
	tree := git(t, f.ctx, f.bare, "ls-tree", "-r", "--name-only", "main")
	for _, want := range []string{"README.md", ManifestFile, "pilot/README.md", "pilot/decisions.md"} {
		if !strings.Contains(tree, want) {
			t.Errorf("share tree missing %s:\n%s", want, tree)
		}
	}

	again := f.publish(t, f.exports(t))
	if again.Changed || again.FirstPublish() {
		t.Errorf("republishing an unchanged export must be a no-op: %+v", again)
	}
}

func TestPublishReportsChangesAndRemovals(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))

	write(t, f.root, "projects/pilot/decisions.md", "# Decisions\n\n### DEC-001 — New\n")
	git(t, f.ctx, f.root, "commit", "-qam", "decision")
	plan := f.publish(t, f.exports(t))
	c := plan.Projects["pilot"]
	// Every file's mirror header names the source commit, so README.md
	// changes too; decisions.md must be among the changes.
	if plan.FirstPublish() || !strings.Contains(strings.Join(c.Changed, ","), "decisions.md") || len(c.Added)+len(c.Removed) != 0 {
		t.Errorf("want a republish changing decisions.md, got %+v", c)
	}

	empty := f.publish(t, map[string]*Result{})
	if strings.Join(empty.Removed, ",") != "pilot" {
		t.Errorf("an unshared project must be reported removed, got %v", empty.Removed)
	}
	if tree := git(t, f.ctx, f.bare, "ls-tree", "-r", "--name-only", "main"); strings.Contains(tree, "pilot/") {
		t.Errorf("removed project still in the share tree:\n%s", tree)
	}
}

func TestPublishRefusesForeignCommits(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))
	f.pushForeign(t, "pilot/README.md")
	if _, err := PreparePublish(f.ctx, f.opts(t, f.exports(t))); err == nil || !strings.Contains(err.Error(), "did not make") {
		t.Fatalf("want a foreign-commit refusal, got %v", err)
	}
}

func TestPublishReplacesNonCgContentOnlyAsFirstPublish(t *testing.T) {
	f := newPublishFixture(t)
	f.pushForeign(t, "README.md") // a repo created with a README
	plan, err := PreparePublish(f.ctx, f.opts(t, f.exports(t)))
	if err != nil {
		t.Fatalf("PreparePublish: %v", err)
	}
	defer plan.Close()
	if strings.Join(plan.Foreign, ",") != "README.md" || !plan.FirstPublish() {
		t.Errorf("want README.md reported as foreign and a first publish, got %+v", plan)
	}
}

func TestPublishRefusesWorkspaceHistory(t *testing.T) {
	f := newPublishFixture(t)
	fork := filepath.Join(t.TempDir(), "fork.git")
	git(t, f.ctx, "", "clone", "--quiet", "--bare", f.root, fork)
	o := f.opts(t, f.exports(t))
	o.Repo = fork
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "a separate repo") {
		t.Fatalf("want a not-a-separate-repo refusal, got %v", err)
	}
}

func TestPublishRefusesFindingsAndOtherAudiences(t *testing.T) {
	f := newPublishFixture(t)
	dirty := f.exports(t)
	dirty["pilot"].Findings = []Finding{{Rule: RuleToken}}
	if _, err := PreparePublish(f.ctx, f.opts(t, dirty)); err == nil || !strings.Contains(err.Error(), "open scan finding") {
		t.Errorf("want a findings refusal, got %v", err)
	}

	f.publish(t, f.exports(t))
	o := f.opts(t, f.exports(t))
	o.Audience = "other"
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "another audience") {
		t.Errorf("want an other-audience refusal, got %v", err)
	}
}

func TestPublishPushIsFastForwardOnly(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))
	write(t, f.root, "projects/pilot/decisions.md", "# Decisions\n\nchanged\n")
	git(t, f.ctx, f.root, "commit", "-qam", "change")

	first, err := PreparePublish(f.ctx, f.opts(t, f.exports(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// A concurrent publish with different content (identical content would
	// produce the identical commit, which is a harmless no-op push).
	other := f.exports(t)
	other["pilot"].Files["pilot/decisions.md"] += "\nconcurrent edit\n"
	second, err := PreparePublish(f.ctx, f.opts(t, other))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.Commit(f.ctx); err != nil {
		t.Fatalf("first push: %v", err)
	}
	if _, err := second.Commit(f.ctx); err == nil || !strings.Contains(err.Error(), "published meanwhile") {
		t.Fatalf("a stale publish must be rejected, not forced; got %v", err)
	}
}

func TestPublishToNewBranchOfExistingRepo(t *testing.T) {
	f := newPublishFixture(t)
	f.pushForeign(t, "unrelated.md") // main has someone else's content
	o := f.opts(t, f.exports(t))
	o.Branch = "team-share"
	plan, err := PreparePublish(f.ctx, o)
	if err != nil {
		t.Fatalf("PreparePublish: %v", err)
	}
	defer plan.Close()
	if len(plan.Foreign) != 0 {
		t.Errorf("a new branch starts empty, so nothing is foreign: %v", plan.Foreign)
	}
	if _, err := plan.Commit(f.ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if tree := git(t, f.ctx, f.bare, "ls-tree", "-r", "--name-only", "team-share"); strings.Contains(tree, "unrelated.md") || !strings.Contains(tree, "pilot/README.md") {
		t.Errorf("new branch must hold only the published tree:\n%s", tree)
	}
	if _, err := os.Stat(plan.Dir); err != nil {
		t.Errorf("the staged clone must exist until Close: %v", err)
	}
}

// The reviewer's bypass: the share repo IS the workspace (a bare copy of it),
// and publishing targets an orphan branch whose own root is unrelated. Every
// branch must be checked, not just the target.
func TestPublishRefusesWorkspaceHiddenBehindOrphanBranch(t *testing.T) {
	f := newPublishFixture(t)
	copyRepo := filepath.Join(t.TempDir(), "ws.git")
	git(t, f.ctx, "", "clone", "--quiet", "--bare", f.root, copyRepo)
	work := filepath.Join(t.TempDir(), "orphan")
	git(t, f.ctx, "", "clone", "--quiet", copyRepo, work)
	git(t, f.ctx, work, "config", "user.email", "x@example.com")
	git(t, f.ctx, work, "config", "user.name", "X")
	git(t, f.ctx, work, "config", "commit.gpgsign", "false")
	git(t, f.ctx, work, "checkout", "--quiet", "--orphan", "share")
	git(t, f.ctx, work, "rm", "-rq", "--cached", ".")
	write(t, work, "x.md", "x\n")
	git(t, f.ctx, work, "add", "x.md")
	git(t, f.ctx, work, "commit", "--quiet", "-m", "orphan")
	git(t, f.ctx, work, "push", "--quiet", "origin", "share")

	o := f.opts(t, f.exports(t))
	o.Repo, o.Branch = copyRepo, "share"
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "a separate repo") {
		t.Fatalf("a share repo holding the workspace on another branch must be refused, got %v", err)
	}
}

func TestPublishRefusesWorkspaceHistoryOnAnotherBranch(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))
	git(t, f.ctx, f.root, "push", "--quiet", f.bare, "HEAD:refs/heads/backup")
	if _, err := PreparePublish(f.ctx, f.opts(t, f.exports(t))); err == nil || !strings.Contains(err.Error(), "a separate repo") {
		t.Fatalf("workspace history on another branch of the share repo must be refused, got %v", err)
	}
}

func TestPublishRefusesShallowWorkspace(t *testing.T) {
	f := newPublishFixture(t)
	shallow := filepath.Join(t.TempDir(), "shallow")
	git(t, f.ctx, "", "clone", "--quiet", "--depth", "1", "file://"+f.root, shallow)
	o := f.opts(t, f.exports(t))
	o.WorkspaceRoot = shallow
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "shallow") {
		t.Fatalf("a shallow workspace must be refused, got %v", err)
	}
}

func TestPublishCountsQuotedFileNames(t *testing.T) {
	f := newPublishFixture(t)
	exports := f.exports(t)
	exports["pilot"].Files["pilot/my file.md"] = "a\n"
	exports["pilot"].Files["pilot/café.md"] = "b\n"
	plan, err := PreparePublish(f.ctx, f.opts(t, exports))
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if got := len(plan.Projects["pilot"].Added); got != 4 {
		t.Errorf("names with spaces or non-ASCII must be counted: got %d added (%v)", got, plan.Projects["pilot"].Added)
	}
}

// globalGitConfig points git at a throwaway global config for the test.
func globalGitConfig(t *testing.T, body string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestPublishIgnoresGlobalGitignore(t *testing.T) {
	f := newPublishFixture(t)
	ignore := filepath.Join(t.TempDir(), "ignore")
	write(t, filepath.Dir(ignore), "ignore", "decisions.md\n")
	globalGitConfig(t, "[core]\n\texcludesFile = "+ignore+"\n")
	f.publish(t, f.exports(t))
	if tree := git(t, f.ctx, f.bare, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(tree, "pilot/decisions.md") {
		t.Errorf("a global gitignore must not drop a published file:\n%s", tree)
	}
}

func TestPublishIgnoresGlobalHooks(t *testing.T) {
	f := newPublishFixture(t)
	hooks := t.TempDir()
	write(t, hooks, "prepare-commit-msg", "#!/bin/sh\necho 'hijacked' > \"$1\"\n")
	write(t, hooks, "pre-push", "#!/bin/sh\nexit 1\n")
	for _, h := range []string{"prepare-commit-msg", "pre-push"} {
		if err := os.Chmod(filepath.Join(hooks, h), 0o755); err != nil { //nolint:gosec // G302: a test hook must be executable
			t.Fatal(err)
		}
	}
	globalGitConfig(t, "[core]\n\thooksPath = "+hooks+"\n")
	f.publish(t, f.exports(t))
	if log := git(t, f.ctx, f.bare, "log", "-1", "--format=%B", "main"); !strings.Contains(log, PublishTrailer) {
		t.Errorf("global hooks must not run in cg's clone; commit message:\n%s", log)
	}
}

func TestPublishNewProjectInPublishedAudienceIsFirst(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))
	exports := f.exports(t)
	sib := &Result{Files: map[string]string{"sibling/README.md": "# S\n"}, Stamp: Stamp{SHA: "abc", Date: "2026-09-25", Owner: "Ada"}}
	exports["sibling"] = sib
	plan, err := PreparePublish(f.ctx, f.opts(t, exports))
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if !plan.FirstPublish() || !plan.Projects["sibling"].First || plan.Projects["pilot"].First {
		t.Errorf("adding a project must make it a first publish, and only it: %+v", plan.Projects)
	}
}

// If the workspace's history cannot be listed, the check has not been made
// and must not pass.
func TestPublishFailsClosedWhenHistoryUnreadable(t *testing.T) {
	f := newPublishFixture(t)
	o := f.opts(t, f.exports(t))
	o.WorkspaceRoot = t.TempDir() // not a git repository
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "root commits") {
		t.Fatalf("an unreadable workspace history must refuse the publish, got %v", err)
	}
}

func TestPublishAddedFileNeedsReview(t *testing.T) {
	f := newPublishFixture(t)
	f.publish(t, f.exports(t))

	changed := f.exports(t)
	changed["pilot"].Files["pilot/decisions.md"] += "\nedited\n"
	plan, err := PreparePublish(f.ctx, f.opts(t, changed))
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.AddsFiles() || plan.NeedsReview() {
		t.Errorf("a change-only republish must not need the terminal review: %+v", plan.Projects)
	}

	added := f.exports(t)
	added["pilot"].Files["pilot/log.md"] = "# Log\n"
	plan2, err := PreparePublish(f.ctx, f.opts(t, added))
	if err != nil {
		t.Fatal(err)
	}
	defer plan2.Close()
	if plan2.FirstPublish() || !plan2.AddsFiles() || !plan2.NeedsReview() {
		t.Errorf("adding a file to a published project must need the terminal review: %+v", plan2.Projects)
	}
}
