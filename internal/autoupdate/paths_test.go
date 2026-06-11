package autoupdate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDirHonorsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := StateDir()
	want := filepath.Join(dir, "consigliere")
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDirFallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := StateDir()
	want := filepath.Join(home, ".config", "consigliere")
	if got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestPathHelpersLiveUnderStateDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	base := StateDir()

	cases := map[string]string{
		"LastCheckPath":      LastCheckPath(),
		"LockPath":           LockPath(),
		"UpdatedMarkerPath":  UpdatedMarkerPath(),
		"MajorAvailablePath": MajorAvailablePath(),
		"MajorIgnoredPath":   MajorIgnoredPath(),
		"ErrorLogPath":       ErrorLogPath(),
		"InstalledStatePath": InstalledStatePath(),
	}
	for name, p := range cases {
		if !strings.HasPrefix(p, base+string(filepath.Separator)) {
			t.Errorf("%s() = %q, not under StateDir %q", name, p, base)
		}
	}
}
