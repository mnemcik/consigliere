package share

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// initWorkspace creates a git-backed workspace with one committed project and
// returns its root. It skips where git integration is not exercised.
func initWorkspace(t *testing.T) (ctx context.Context, root string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping git-integration test on windows")
	}
	ctx = context.Background()
	root = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	git(t, ctx, root, "init", "--initial-branch=main")
	git(t, ctx, root, "config", "user.email", "test@example.com")
	git(t, ctx, root, "config", "user.name", "Git Name")
	git(t, ctx, root, "config", "commit.gpgsign", "false")

	write(t, root, "projects/TODO.md", "| # | Project | Status | Areas | Folder |\n|---|---|---|---|---|\n| 1 | Pilot | In Progress | `a` | [pilot](pilot/README.md) |\n")
	write(t, root, "projects/pilot/README.md", "# Pilot\n\n## Meta\n\n- **Started:** 2026-01-01\n")
	write(t, root, "projects/pilot/decisions.md", "# Decisions\n")
	write(t, root, "projects/pilot/log.md", "# Log\n")
	write(t, root, "projects/pilot/resume.md", "cursor\n")
	git(t, ctx, root, "add", ".")
	git(t, ctx, root, "commit", "-m", "seed")
	return ctx, root
}

func git(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.Run(ctx, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pilotOptions(root string) *Options {
	return &Options{
		Root:      root,
		Project:   Project{Slug: "pilot", Status: "In Progress", Areas: []string{"a"}},
		IndexPath: "projects/TODO.md",
	}
}

func TestExportStampsSourceCommit(t *testing.T) {
	ctx, root := initWorkspace(t)
	head := git(t, ctx, root, "rev-parse", "HEAD")
	date := git(t, ctx, root, "log", "-1", "--format=%cs")

	res, err := Export(ctx, pilotOptions(root))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if res.Stamp.SHA != head || res.Stamp.Date != date || res.Stamp.Owner != "Git Name" {
		t.Errorf("stamp = %+v, want sha %s date %s owner from git", res.Stamp, head, date)
	}
	// defaults only: todo.md is absent, log.md and resume.md are not included
	if got := strings.Join(sortedKeys(res.Files), ","); got != "pilot/README.md,pilot/decisions.md" {
		t.Errorf("exported %s", got)
	}
}

func TestExportStampIgnoresUnrelatedCommits(t *testing.T) {
	ctx, root := initWorkspace(t)
	first, err := Export(ctx, pilotOptions(root))
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "notes/other.md", "unrelated\n")
	git(t, ctx, root, "add", ".")
	git(t, ctx, root, "commit", "-m", "unrelated")

	second, err := Export(ctx, pilotOptions(root))
	if err != nil {
		t.Fatal(err)
	}
	if first.Files["pilot/README.md"] != second.Files["pilot/README.md"] {
		t.Error("an unrelated commit changed the export; it must be byte-identical")
	}
}

func TestExportRefusesUncommittedChanges(t *testing.T) {
	for _, rel := range []string{"projects/pilot/decisions.md", "projects/TODO.md"} {
		t.Run(rel, func(t *testing.T) {
			ctx, root := initWorkspace(t)
			write(t, root, rel, "edited\n")
			_, err := Export(ctx, pilotOptions(root))
			if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
				t.Fatalf("want uncommitted-changes error, got %v", err)
			}
		})
	}
}

func TestExportInclude(t *testing.T) {
	ctx, root := initWorkspace(t)
	opts := pilotOptions(root)
	opts.Include = []string{"log.md"}
	res, err := Export(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Files["pilot/log.md"]; !ok {
		t.Errorf("included log.md not exported: %v", sortedKeys(res.Files))
	}

	for include, wantErr := range map[string]string{
		"resume.md":        "never exported",
		"missing.md":       "no such file",
		"../TODO.md":       "not a file inside the project folder",
		"/etc/passwd":      "not a file inside the project folder",
		"sub/../resume.md": "never exported",
	} {
		opts.Include = []string{include}
		if _, err := Export(ctx, opts); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("include %q: want error containing %q, got %v", include, wantErr, err)
		}
	}
}

func TestExportOwner(t *testing.T) {
	ctx, root := initWorkspace(t)
	opts := pilotOptions(root)
	opts.Owner = "Flag Name"
	res, err := Export(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stamp.Owner != "Flag Name" {
		t.Errorf("owner = %q, want the explicit override", res.Stamp.Owner)
	}

	git(t, ctx, root, "config", "--unset", "user.name")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	opts.Owner = ""
	if _, err := Export(ctx, opts); err == nil || !strings.Contains(err.Error(), "no owner name") {
		t.Errorf("want no-owner error, got %v", err)
	}
}

func TestExportRejectsBadSlug(t *testing.T) {
	ctx, root := initWorkspace(t)
	for _, slug := range []string{"", "..", "a/b", "missing"} {
		opts := pilotOptions(root)
		opts.Project.Slug = slug
		if _, err := Export(ctx, opts); err == nil {
			t.Errorf("slug %q: expected an error", slug)
		}
	}
}

func TestWriteTo(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	files := map[string]string{"pilot/README.md": "r\n", "pilot/decisions.md": "d\n"}
	if err := WriteTo(out, files); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "pilot", "README.md"))
	if err != nil || string(data) != "r\n" {
		t.Fatalf("README not written: %q %v", data, err)
	}
	if err := WriteTo(out, files); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("want not-empty error on a second write, got %v", err)
	}
}

func sortedKeys(m map[string]string) []string {
	ks := keys(m)
	sort.Strings(ks)
	return ks
}
