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
	if _, err := PreparePublish(f.ctx, o); err == nil || !strings.Contains(err.Error(), "shares history") {
		t.Fatalf("want a shared-history refusal, got %v", err)
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
