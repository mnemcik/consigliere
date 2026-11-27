//go:build windows

package extension

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// On a case-sensitive Windows directory (fsutil setCaseSensitiveInfo),
// Repo and repo are two directories: one is never inside the other.
func TestWithinDirCaseSensitiveWindows(t *testing.T) {
	base := t.TempDir()
	if out, err := exec.Command("fsutil.exe", "file", "setCaseSensitiveInfo", base, "enable").CombinedOutput(); err != nil { //nolint:gosec // fixed tool, test temp dir
		t.Skipf("can't make a case-sensitive directory here: %v: %s", err, out)
	}
	upper, lower := filepath.Join(base, "Repo"), filepath.Join(base, "repo")
	for _, d := range []string{upper, lower} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	ui, err1 := os.Stat(upper)
	li, err2 := os.Stat(lower)
	if err1 != nil || err2 != nil || os.SameFile(ui, li) {
		t.Skip("the directory isn't case-sensitive after all")
	}
	if withinDir(upper, filepath.Join(lower, "f")) {
		t.Error("repo counted as inside Repo on a case-sensitive directory")
	}
	if !withinDir(upper, filepath.Join(upper, "f")) {
		t.Error("Repo\\f isn't inside Repo")
	}
}
