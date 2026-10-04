package templates

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestEmbedCoversTree fails when a file under templates/ is missing from FS:
// a new top-level entry not added to the //go:embed list in embed.go, or a
// dotfile inside a directory whose pattern lost its all: prefix. It also
// fails on the reverse, so the embedded set and the tree on disk match exactly.
func TestEmbedCoversTree(t *testing.T) {
	onDisk := map[string]bool{}
	err := fs.WalkDir(os.DirFS("."), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".go") {
			return err
		}
		onDisk[p] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates/: %v", err)
	}

	embedded := map[string]bool{}
	err = fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embedded[p] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded FS: %v", err)
	}

	for p := range onDisk {
		if !embedded[p] {
			t.Errorf("templates/%s is not embedded; add its top-level entry to the //go:embed list in embed.go", p)
		}
	}
	for p := range embedded {
		if !onDisk[p] {
			t.Errorf("embedded file %s has no counterpart under templates/", p)
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("found no files under templates/; the test is not looking at the tree")
	}
}

// TestItemTemplatesHaveValidFrontmatter pins the item templates to the
// frontmatter form. An unquoted `{placeholder}` is a YAML flow mapping, so a
// template edit that drops the quotes would still look right but hand every
// new item a block that parses to the wrong type -- or not at all.
func TestItemTemplatesHaveValidFrontmatter(t *testing.T) {
	for _, p := range []string{"project/README.md", "idea.md", "area.md", "note.md", "insight.md"} {
		b, err := fs.ReadFile(FS, p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		doc := string(b)
		if !strings.HasPrefix(doc, "---\n") {
			t.Errorf("%s does not open with frontmatter", p)
			continue
		}
		end := strings.Index(doc[4:], "\n---\n")
		if end < 0 {
			t.Errorf("%s: unterminated frontmatter", p)
			continue
		}
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(doc[4:4+end]), &fm); err != nil {
			t.Errorf("%s: frontmatter is not valid YAML: %v", p, err)
			continue
		}
		for k, v := range fm {
			switch v.(type) {
			case string, []any:
			default:
				t.Errorf("%s: %s is a %T, want a string or a list (unquoted placeholder?)", p, k, v)
			}
		}
		if _, ok := fm["title"].(string); !ok {
			t.Errorf("%s: no title (the only required key, DEC-009)", p)
		}
		if strings.Contains(doc, "## Meta") {
			t.Errorf("%s still carries a ## Meta block", p)
		}
	}
}
