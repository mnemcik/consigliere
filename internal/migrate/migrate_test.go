package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const metaItem = "# T\n\n## Meta\n\n- **Status:** raw\n\n## What\n"

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// shippedFS stands in for templates.FS with frontmatter-form templates.
var shippedFS = fstest.MapFS{
	"idea.md": {Data: []byte("---\ntitle: \"{Idea Title}\"\n---\n\n# {Idea Title}\n")},
	"note.md": {Data: []byte("---\ntitle: \"{Note Title}\"\n---\n\n# {Note Title}\n")},
}

func TestBuildAndApply(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ideas/one.md", metaItem)
	write(t, root, "projects/p/README.md", metaItem)
	write(t, root, "projects/p/log.md", "---\ntitle: Log\n---\n\n# Log\n")
	write(t, root, "projects/TODO.md", "# Projects\n\n## Meta\n\n- **Status:** not an item\n")
	write(t, root, "notes/framework.md", metaItem)
	write(t, root, "notes/from-extension.md", metaItem)
	write(t, root, "notes/mine.md", metaItem)
	write(t, root, ".cg/manifest.json", `{"schemaVersion":1,"notes":{"notes/framework.md":{"hash":"x"}}}`)
	write(t, root, ".cg/ext/demo.json", `{"name":"demo","notes":["notes/from-extension.md"]}`)

	plan, err := Build(root, shippedFS)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(plan.Changes))
	for _, c := range plan.Changes {
		paths = append(paths, c.Path)
	}
	if got := strings.Join(paths, " "); got != "ideas/one.md notes/mine.md projects/p/README.md" {
		t.Errorf("changes = %s", got)
	}
	if got := strings.Join(plan.Skipped, " "); got != "notes/framework.md notes/from-extension.md" {
		t.Errorf("skipped = %s", got)
	}

	if read(t, root, "ideas/one.md") != metaItem {
		t.Fatal("Build wrote to disk")
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "ideas/one.md"); !strings.HasPrefix(got, "---\ntitle: T\nstatus: raw\n---\n") {
		t.Errorf("ideas/one.md =\n%s", got)
	}
	for _, untouched := range []string{"notes/framework.md", "notes/from-extension.md"} {
		if read(t, root, untouched) != metaItem {
			t.Errorf("%s was rewritten", untouched)
		}
	}
	if !strings.Contains(read(t, root, "projects/TODO.md"), "## Meta") {
		t.Error("an index file was rewritten")
	}

	again, err := Build(root, shippedFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != 0 {
		t.Errorf("second run would change %d files", len(again.Changes))
	}
}

// TestShippedTemplateHashes pins shippedTemplates to the files cg actually
// shipped, kept under testdata/shipped/ (one per version, named
// <template>-<commit>.md). Those hashes are the contract with every workspace
// cg ever initialised: a match is replaced, anything else is left alone.
func TestShippedTemplateHashes(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "shipped", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(shippedTemplates) {
		t.Fatalf("%d testdata versions for %d hashes", len(files), len(shippedTemplates))
	}
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // test fixture
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if !shippedTemplates[hex.EncodeToString(sum[:])] {
			t.Errorf("%s is not in shippedTemplates", f)
		}
	}
}

func TestBuildTemplates(t *testing.T) {
	root := t.TempDir()
	write(t, root, "templates/idea.md", fixture(t, "idea-11356ee.md"))
	write(t, root, "templates/note.md", fixture(t, "note-d766d8a.md")+"\n- my edit\n")

	plan, err := Build(root, shippedFS)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, tc := range plan.Templates {
		actions[tc.Path] = tc.Action
	}
	if actions["templates/idea.md"] != TemplateReplace || actions["templates/note.md"] != TemplateCustomised {
		t.Errorf("actions = %v", actions)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, root, "templates/idea.md"), "---\n") {
		t.Error("untouched template not replaced")
	}
	if !strings.Contains(read(t, root, "templates/note.md"), "- my edit") {
		t.Error("customised template overwritten")
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "shipped", name)) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildReportsUnconvertibleFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ideas/bad.md", "---\ntitle: [unclosed\n---\n\n# T\n\n## Meta\n\n- **Status:** raw\n")
	plan, err := Build(root, shippedFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Errors) != 1 || len(plan.Changes) != 0 {
		t.Errorf("errors = %q, changes = %d", plan.Errors, len(plan.Changes))
	}
}
