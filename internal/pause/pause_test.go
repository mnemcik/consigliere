package pause

import "testing"

func TestIsCursorPath(t *testing.T) {
	for name, want := range map[string]bool{
		"projects/p/resume.md":        true,
		"vault/projects/p/resume.md":  true,
		"a/b/projects/p/resume.md":    true,
		"projects/p/resume.md\r":      true,
		"projects/resume.md":          false,
		"projects//resume.md":         false,
		"projects/p/notes/resume.md":  false,
		"other/p/resume.md":           false,
		"projects/p/Resume.md":        false,
		"projects/p/resume.md.bak":    false,
		"templates/project/resume.md": false,
	} {
		if got := IsCursorPath(name); got != want {
			t.Errorf("IsCursorPath(%q) = %v, want %v", name, got, want)
		}
	}
}
