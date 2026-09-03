package meta

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "item.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Fields
	}{
		{
			name: "meta block only",
			body: "# Item\n\n## Meta\n\n- **Tags:** `tool`, `framework`\n- **Color:** cyan\n\n## Overview\n",
			want: Fields{Tags: []string{"tool", "framework"}, Color: "cyan"},
		},
		{
			name: "meta block with bare comma list",
			body: "# Item\n\n## Meta\n\n- **Tags:** practice, compliance\n",
			want: Fields{Tags: []string{"practice", "compliance"}},
		},
		{
			name: "frontmatter only",
			body: "---\ntags: [tool, framework]\ncolor: cyan\n---\n\n# Item\n",
			want: Fields{Tags: []string{"tool", "framework"}, Color: "cyan"},
		},
		{
			name: "frontmatter wins over meta block",
			body: "---\ntags: [fresh]\ncolor: green\n---\n\n# Item\n\n## Meta\n\n- **Tags:** `stale`\n- **Color:** red\n",
			want: Fields{Tags: []string{"fresh"}, Color: "green"},
		},
		{
			// The generated projection covers tags but not color, so a
			// whole-file fallback would silently drop the badge colour.
			name: "per-field fallback when frontmatter omits a field",
			body: "---\ntags: [tool]\n---\n\n# Item\n\n## Meta\n\n- **Tags:** `ignored`\n- **Color:** magenta\n",
			want: Fields{Tags: []string{"tool"}, Color: "magenta"},
		},
		{
			name: "frontmatter scalar tags split into a list",
			body: "---\ntags: communication, recommendations\n---\n\n# Item\n",
			want: Fields{Tags: []string{"communication", "recommendations"}},
		},
		{
			name: "malformed frontmatter falls back instead of failing",
			body: "---\ndescription: When a hook gates it: this is not valid YAML\n---\n\n## Meta\n\n- **Color:** blue\n",
			want: Fields{Color: "blue"},
		},
		{
			name: "unfilled template placeholders read as empty",
			body: "# Item\n\n## Meta\n\n- **Color:** {color}\n",
			want: Fields{},
		},
		{
			// An explicit empty list is a declaration that the item has no
			// tags, so a stale Meta block must not resurrect them.
			name: "explicit empty tags list wins over the Meta block",
			body: "---\ntags: []\n---\n\n# Item\n\n## Meta\n\n- **Tags:** `stale`\n",
			want: Fields{},
		},
		{
			name: "explicit empty color wins over the Meta block",
			body: "---\ncolor: \"\"\n---\n\n# Item\n\n## Meta\n\n- **Color:** red\n",
			want: Fields{},
		},
		{
			// A `{placeholder}` is an unfilled template value rather than a
			// decision, so unlike an explicit empty it still falls back.
			name: "frontmatter placeholder still falls back to the Meta block",
			body: "---\ncolor: \"{color}\"\n---\n\n# Item\n\n## Meta\n\n- **Color:** red\n",
			want: Fields{Color: "red"},
		},
		{
			name: "no metadata at all",
			body: "# Item\n\nJust prose.\n",
			want: Fields{},
		},
		{
			name: "horizontal rule mid-document is not frontmatter",
			body: "# Item\n\n---\n\n## Meta\n\n- **Color:** yellow\n",
			want: Fields{Color: "yellow"},
		},
		{
			name: "Color without a leading bullet",
			body: "# Item\n\n**Color:** purple\n",
			want: Fields{Color: "purple"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Read(write(t, tt.body))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if !reflect.DeepEqual(got.Tags, tt.want.Tags) {
				t.Errorf("Tags = %#v, want %#v", got.Tags, tt.want.Tags)
			}
			if got.Color != tt.want.Color {
				t.Errorf("Color = %q, want %q", got.Color, tt.want.Color)
			}
		})
	}
}

// A body bullet using the same shape must not be mistaken for metadata once it
// is far enough from the top of the file.
func TestReadIgnoresDeepBodyBullets(t *testing.T) {
	body := "# Item\n\n## Meta\n\n- **Tags:** `real`\n\n## Details\n"
	for i := 0; i < scanLineLimit; i++ {
		body += "\nfiller line\n"
	}
	body += "\n- **Color:** notmetadata\n"

	got, err := Read(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tags, []string{"real"}) {
		t.Errorf("Tags = %#v, want [real]", got.Tags)
	}
	if got.Color != "" {
		t.Errorf("Color = %q, want empty -- a bullet past the scan limit is prose", got.Color)
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "absent.md")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

// Real area files carry a `**Last reviewed:**` value of accumulated review
// prose on a single line, past 10 KB. bufio's default 64 KB ceiling would
// tolerate that, but the scanner must not choke as the history grows, and a
// long line before the wanted field must not stop the scan.
func TestReadHandlesVeryLongMetaLines(t *testing.T) {
	long := strings.Repeat("review history prose. ", 20000) // ~440 KB
	body := "# Area\n\n## Meta\n\n- **Last reviewed:** 2026-09-02 (" + long + ")\n- **Color:** cyan\n"

	got, err := Read(write(t, body))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Color != "cyan" {
		t.Errorf("Color = %q, want cyan -- a long preceding line must not stop the scan", got.Color)
	}
}
