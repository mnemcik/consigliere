package extension

import (
	"path/filepath"
	"testing"
)

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
