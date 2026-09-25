package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/share"
	"github.com/mnemcik/consigliere/internal/workspace"
)

func TestIndexedProject(t *testing.T) {
	index := filepath.Join(t.TempDir(), "TODO.md")
	body := "| # | Project | Status | Areas | Folder |\n" +
		"|---|---|---|---|---|\n" +
		"| 1 | Pilot | In Progress | `a`, `b` | [pilot](pilot/README.md) |\n"
	if err := os.WriteFile(index, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := indexedProject(index, "pilot")
	if err != nil {
		t.Fatalf("indexedProject: %v", err)
	}
	if p.Slug != "pilot" || p.Status != "In Progress" || strings.Join(p.Areas, ",") != "a,b" {
		t.Errorf("got %+v", p)
	}

	if _, err := indexedProject(index, "missing"); err == nil || !strings.Contains(err.Error(), "no row") {
		t.Errorf("want no-row error, got %v", err)
	}
}

func TestParseAcks(t *testing.T) {
	acks, err := parseAcks([]string{"local-path:6c54c54b471a3e54", "placeholder:abc"})
	if err != nil {
		t.Fatalf("parseAcks: %v", err)
	}
	if len(acks) != 2 || acks[0].Rule != "local-path" || acks[0].Hash != "6c54c54b471a3e54" {
		t.Errorf("got %+v", acks)
	}
	for _, bad := range []string{"nohash", ":abc", "rule:"} {
		if _, err := parseAcks([]string{bad}); err == nil {
			t.Errorf("parseAcks(%q): expected an error", bad)
		}
	}
}

// shareWorkspace builds a committed workspace with two projects, alpha and
// beta, where alpha links to beta, and a .cg.json share block sharing both
// with audience "team" through a local bare repo. It returns the workspace
// root and the bare repo path.
func shareWorkspace(t *testing.T) (root, bare string) {
	t.Helper()
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("git integration not exercised here")
	}
	ctx := context.Background()
	root = t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	bare = filepath.Join(t.TempDir(), "share.git")
	mustGit(t, ctx, "", "init", "--bare", "--initial-branch=main", bare)
	mustGit(t, ctx, root, "init", "--initial-branch=main")
	mustGit(t, ctx, root, "config", "user.email", "test@example.com")
	mustGit(t, ctx, root, "config", "user.name", "Git Name")
	mustGit(t, ctx, root, "config", "commit.gpgsign", "false")

	cfg := &workspace.Config{
		Type: workspace.TypeConsigliere, Version: "1.0.0",
		Share: &workspace.ShareConfig{
			Owner:    "Ada Owner",
			Denylist: []string{"acme"},
			Audiences: map[string]workspace.ShareAudience{
				"team": {Repo: bare, Projects: map[string]workspace.ShareProject{
					"alpha": {Include: []string{"log.md"}},
					"beta":  {},
				}},
			},
		},
	}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "projects/TODO.md", "| # | Project | Status | Areas | Folder |\n|---|---|---|---|---|\n"+
		"| 1 | Alpha | In Progress | `a` | [alpha](alpha/README.md) |\n| 2 | Beta | Defining | `a` | [beta](beta/README.md) |\n")
	writeFile(t, root, "projects/alpha/README.md", "# Alpha\n\nSee [beta](../beta/README.md) and [notes](../../notes/x.md).\n")
	writeFile(t, root, "projects/alpha/log.md", "# Log\n")
	writeFile(t, root, "projects/beta/README.md", "# Beta\n")
	mustGit(t, ctx, root, "add", ".")
	mustGit(t, ctx, root, "commit", "-m", "seed")
	return root, bare
}

func mustGit(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(ctx, dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runShare executes `cg share <args>` in dir, resetting flags left over from
// earlier runs of the shared command tree.
func runShare(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	for _, c := range []*cobra.Command{shareExportCmd, shareStatusCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"share"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

// publishManifest pushes a manifest recording the given exports to the bare
// share repo, standing in for `cg share publish` until it exists.
func publishManifest(t *testing.T, bare string, results map[string]*share.Result) {
	t.Helper()
	ctx := context.Background()
	m := share.Manifest{Version: share.ManifestVersion, Owner: "Ada Owner", Audience: "team", Projects: map[string]share.PublishedProject{}}
	for slug, res := range results {
		m.Projects[slug] = share.PublishedEntry(res)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "pub")
	mustGit(t, ctx, "", "clone", "--quiet", bare, work)
	mustGit(t, ctx, work, "config", "user.email", "test@example.com")
	mustGit(t, ctx, work, "config", "user.name", "Git Name")
	mustGit(t, ctx, work, "config", "commit.gpgsign", "false")
	writeFile(t, work, share.ManifestFile, string(data))
	mustGit(t, ctx, work, "add", ".")
	mustGit(t, ctx, work, "commit", "-m", "publish")
	mustGit(t, ctx, work, "push", "--quiet", "origin", "HEAD:main")
}

func exportFor(t *testing.T, root, slug string) *share.Result {
	t.Helper()
	cfg, err := workspace.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	env := &shareEnv{root: root, indexPath: indexProjectsPath, share: cfg.Share}
	opts, err := env.exportOptions(context.Background(), slug, "team", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := share.Export(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestShareExportWithAudience(t *testing.T) {
	root, _ := shareWorkspace(t)
	res := exportFor(t, root, "alpha")

	readme := res.Files["alpha/README.md"]
	for _, want := range []string{"See [beta](../beta/README.md)", "notes *(private)*", "Owned by Ada Owner"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README missing %q:\n%s", want, readme)
		}
	}
	if _, ok := res.Files["alpha/log.md"]; !ok {
		t.Errorf("audience include of log.md not applied: %v", res.Files)
	}

	// Without the audience the sibling link is private and log.md stays out.
	out, err := runShare(t, root, "export", "alpha", "--check")
	if err != nil {
		t.Fatalf("export --check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 file(s)") {
		t.Errorf("without --audience only README (the default set present) should export, not log.md:\n%s", out)
	}

	if _, err := runShare(t, root, "export", "alpha", "--check", "--audience", "nobody"); err == nil {
		t.Error("an unknown audience must be an error")
	}
}

func TestShareExportAppliesConfigDenylist(t *testing.T) {
	root, _ := shareWorkspace(t)
	writeFile(t, root, "projects/beta/README.md", "# Beta\n\nKickoff with ACME.\n")
	mustGit(t, context.Background(), root, "commit", "-qam", "acme")
	out, err := runShare(t, root, "export", "beta", "--check")
	if err == nil || !strings.Contains(out, "denylist") {
		t.Fatalf("want a denylist finding from the config, got err=%v\n%s", err, out)
	}
}

func TestShareStatus(t *testing.T) {
	root, bare := shareWorkspace(t)

	out, err := runShare(t, root, "status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if strings.Count(out, "not published") != 2 {
		t.Errorf("fresh share repo: want both projects not published:\n%s", out)
	}

	publishManifest(t, bare, map[string]*share.Result{
		"alpha": exportFor(t, root, "alpha"),
		"beta":  exportFor(t, root, "beta"),
		"gone":  {Files: map[string]string{"gone/README.md": "x"}, Stamp: share.Stamp{SHA: "old", Date: "2026-01-01"}},
	})
	out, _ = runShare(t, root, "status", "team")
	for _, want := range []string{"alpha", "up to date", "gone", "removed"} {
		if !strings.Contains(out, want) {
			t.Errorf("after publish: missing %q:\n%s", want, out)
		}
	}

	// A status change in the index alone changes the rendered Meta, so the
	// content hash (not the project commit) must report it stale.
	writeFile(t, root, "projects/TODO.md", "| # | Project | Status | Areas | Folder |\n|---|---|---|---|---|\n"+
		"| 1 | Alpha | Done | `a` | [alpha](alpha/README.md) |\n| 2 | Beta | Defining | `a` | [beta](beta/README.md) |\n")
	mustGit(t, context.Background(), root, "commit", "-qam", "alpha done")
	out, _ = runShare(t, root, "status")
	if !strings.Contains(out, "stale") {
		t.Errorf("index-only status change must read as stale:\n%s", out)
	}

	writeFile(t, root, "projects/beta/README.md", "# Beta\n\nop://Employee/item/field\n")
	mustGit(t, context.Background(), root, "commit", "-qam", "vault ref")
	out, _ = runShare(t, root, "status")
	if !strings.Contains(out, "blocked") {
		t.Errorf("a project with open findings must read as blocked:\n%s", out)
	}
}

func TestShareStatusWithoutConfig(t *testing.T) {
	root := t.TempDir()
	if err := (&workspace.Config{Type: workspace.TypeConsigliere, Version: "1.0.0"}).Save(root); err != nil {
		t.Fatal(err)
	}
	out, err := runShare(t, root, "status")
	if err != nil || !strings.Contains(out, "nothing is shared") {
		t.Errorf("want a nothing-shared message, got err=%v\n%s", err, out)
	}
}

func TestShareExportSiblingsDegradeToPrivate(t *testing.T) {
	root, bare := shareWorkspace(t)
	cfg, err := workspace.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Share.Audiences["team"]
	a.Projects["ghost"] = workspace.ShareProject{}                            // in config, not in the index
	a.Projects["beta"] = workspace.ShareProject{Include: []string{"nope.md"}} // cannot be selected
	cfg.Share.Audiences["team"] = a
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "projects/ghost/README.md", "# Ghost\n")
	writeFile(t, root, "projects/alpha/README.md", "# Alpha\n\nSee [beta](../beta/README.md) and [ghost](../ghost/README.md).\n")
	mustGit(t, context.Background(), root, "add", ".")
	mustGit(t, context.Background(), root, "commit", "-qm", "siblings")

	res := exportFor(t, root, "alpha")
	readme := res.Files["alpha/README.md"]
	for _, want := range []string{"beta *(private)*", "ghost *(private)*"} {
		if !strings.Contains(readme, want) {
			t.Errorf("a sibling that cannot be exported must stay private (%q):\n%s", want, readme)
		}
	}
	out, _ := runShare(t, root, "status")
	if !strings.Contains(out, "alpha") || strings.Contains(firstStatusLine(out, "alpha"), "error") {
		t.Errorf("one broken sibling must not put alpha into error:\n%s", out)
	}
	_ = bare
}

func firstStatusLine(out, slug string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), slug+" ") {
			return l
		}
	}
	return ""
}

func TestShareOwnerPrecedence(t *testing.T) {
	root, _ := shareWorkspace(t)
	cfg, _ := workspace.Detect(root)
	env := &shareEnv{root: root, indexPath: indexProjectsPath, share: cfg.Share}
	for flag, want := range map[string]string{"": "Ada Owner", "   ": "Ada Owner", "Flag Name": "Flag Name"} {
		opts, err := env.exportOptions(context.Background(), "alpha", "team", nil, nil, flag)
		if err != nil {
			t.Fatal(err)
		}
		if opts.Owner != want {
			t.Errorf("--owner %q: owner = %q, want %q", flag, opts.Owner, want)
		}
	}
}

func TestShareStatusIgnoresOtherAudiencesManifest(t *testing.T) {
	root, bare := shareWorkspace(t)
	m := share.Manifest{Version: share.ManifestVersion, Audience: "someone-else", Projects: map[string]share.PublishedProject{"theirs": {}}}
	data, _ := json.Marshal(m)
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "pub")
	mustGit(t, ctx, "", "clone", "--quiet", bare, work)
	mustGit(t, ctx, work, "config", "user.email", "t@example.com")
	mustGit(t, ctx, work, "config", "user.name", "T")
	mustGit(t, ctx, work, "config", "commit.gpgsign", "false")
	writeFile(t, work, share.ManifestFile, string(data))
	mustGit(t, ctx, work, "add", ".")
	mustGit(t, ctx, work, "commit", "-qm", "x")
	mustGit(t, ctx, work, "push", "--quiet", "origin", "HEAD:main")

	out, _ := runShare(t, root, "status")
	if !strings.Contains(out, `holds audience "someone-else"`) || strings.Contains(out, "removed") {
		t.Errorf("another audience's manifest must be refused, not diffed:\n%s", out)
	}
}

func TestShareRejectsWorkspaceOwnRepo(t *testing.T) {
	root, bare := shareWorkspace(t)
	mustGit(t, context.Background(), root, "remote", "add", "origin", bare+"/")
	out, err := runShare(t, root, "status")
	if err == nil || !strings.Contains(err.Error()+out, "own repo") {
		t.Errorf("an audience on the workspace's own origin must be refused, got err=%v\n%s", err, out)
	}
}

func TestShareSiblingThatWouldNotPublishStaysPrivate(t *testing.T) {
	root, _ := shareWorkspace(t)
	// beta is indexed and its files can be selected, but a denylist hit
	// blocks its own export, so it will never be published.
	writeFile(t, root, "projects/beta/README.md", "# Beta\n\nKickoff with ACME.\n")
	mustGit(t, context.Background(), root, "commit", "-qam", "acme")
	readme := exportFor(t, root, "alpha").Files["alpha/README.md"]
	if !strings.Contains(readme, "beta *(private)*") || strings.Contains(readme, "../beta/README.md") {
		t.Errorf("a blocked sibling must stay private:\n%s", readme)
	}
}
