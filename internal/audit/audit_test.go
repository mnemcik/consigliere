package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTags(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "areas", "a.md"), "# A\n\n- **Tags:** Tool, Framework\n")
	writeFile(t, filepath.Join(root, "areas", "b.md"), "# B\n\n- **Tags:** framework, practice\n")
	writeFile(t, filepath.Join(root, "areas", "INDEX.md"), "- **Tags:** ignored\n")
	writeFile(t, filepath.Join(root, "areas", "c.md"), "# C\n\n- **Tags:** {tags}\n") // placeholder → skipped

	counts, err := Tags(root)
	if err != nil {
		t.Fatal(err)
	}
	// framework appears in a + b → count 2 and sorts first.
	if len(counts) == 0 || counts[0].Tag != "framework" || len(counts[0].Areas) != 2 {
		t.Fatalf("expected framework with 2 areas first, got %+v", counts)
	}
	for _, c := range counts {
		if c.Tag == "ignored" {
			t.Error("INDEX.md should be excluded")
		}
	}
}

func TestColorsCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "projects", "p1", "README.md"), "# P1\n\n- **Color:** bright-yellow\n")
	writeFile(t, filepath.Join(root, "projects", "p2", "README.md"), "# P2\n\n- **Color:** bright-yellow\n") // dup
	writeFile(t, filepath.Join(root, "areas", "a.md"), "# A\n\n- **Color:** cyan\n")
	writeFile(t, filepath.Join(root, "areas", "nocolor.md"), "# N\n\n(no color)\n")
	writeFile(t, filepath.Join(root, "areas", "INDEX.md"), "- **Color:** ignored\n")

	rep, err := ColorsCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Assigned) != 3 {
		t.Errorf("expected 3 assigned, got %d (%+v)", len(rep.Assigned), rep.Assigned)
	}
	if len(rep.Missing) != 1 {
		t.Errorf("expected 1 missing, got %v", rep.Missing)
	}
	if len(rep.Duplicates) != 1 || rep.Duplicates[0].Color != "bright-yellow" || len(rep.Duplicates[0].Files) != 2 {
		t.Errorf("expected one bright-yellow duplicate group of 2, got %+v", rep.Duplicates)
	}
}
