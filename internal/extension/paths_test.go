package extension

import (
	"path/filepath"
	"testing"
)

func TestCleanSubdir(t *testing.T) {
	ok := map[string]string{
		"":          "",
		".":         "",
		"./":        "",
		"1pw":       "1pw",
		"  vault  ": "vault",
		"a/b":       filepath.Join("a", "b"),
		"./1pw":     "1pw",
		"a/../b":    "b",
	}
	for in, want := range ok {
		got, err := CleanSubdir(in)
		if err != nil {
			t.Errorf("CleanSubdir(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CleanSubdir(%q) = %q, want %q", in, got, want)
		}
	}

	for _, bad := range []string{"..", "../escape", "a/../../escape", "/abs/path"} {
		if _, err := CleanSubdir(bad); err == nil {
			t.Errorf("CleanSubdir(%q) should have errored", bad)
		}
	}
}

func TestExtensionsDirHonorsXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	want := filepath.Join(base, "consigliere", "extensions")
	if got := ExtensionsDir(); got != want {
		t.Errorf("ExtensionsDir() = %q, want %q", got, want)
	}
	if got := CloneDir("foo"); got != filepath.Join(want, "foo") {
		t.Errorf("CloneDir = %q", got)
	}
	if got := StagingDir(); got != filepath.Join(want, ".staging") {
		t.Errorf("StagingDir = %q", got)
	}
}
