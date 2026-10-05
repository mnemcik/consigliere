package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetVersionTouchesOnlyTheTopLevelValue(t *testing.T) {
	dir := t.TempDir()
	// Keys this binary does not model, an extension's own "version", odd
	// spacing and a shell string that Config.Save would HTML-escape.
	in := `{
  "type": "consigliere",
  "extensions": [{"name": "x", "version": "0.1.0"}],
  "version" :  "1.20.0",
  "obsidian": {"futureKey": true},
  "statuslineUpstream": "~/a.sh && echo '<b>' > /dev/null"
}
`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetVersion(dir, "1.25.0"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in, `"1.20.0"`, `"1.25.0"`, 1)
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	cfg, err := Detect(dir)
	if err != nil || cfg == nil || cfg.Version != "1.25.0" {
		t.Errorf("Detect after SetVersion: %+v, %v", cfg, err)
	}
}

func TestSetVersionRefusesWhenItCannotPatchSafely(t *testing.T) {
	for name, in := range map[string]string{
		"missing":    `{"type": "consigliere"}`,
		"not string": `{"type": "consigliere", "version": 3}`,
		"not object": `["version"]`,
		"broken":     `{"type": "consigliere", "version": "1`,
		"null":       `{"type": "consigliere", "version": null}`,
		"duplicate":  `{"version": "1.0.0", "type": "consigliere", "version": "1.1.0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(in), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SetVersion(dir, "1.25.0"); err == nil {
				t.Error("want an error")
			}
			if got, _ := os.ReadFile(filepath.Join(dir, ConfigFile)); string(got) != in {
				t.Errorf("file changed despite the error: %s", got)
			}
		})
	}
}

// A symlinked .cg.json is refused, and its target is not written.
func TestSetVersionRefusesSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	in := `{"type": "consigliere", "version": "1.0.0"}`
	if err := os.WriteFile(target, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ConfigFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := SetVersion(dir, "1.25.0"); err == nil {
		t.Error("want a refusal for a symlinked .cg.json")
	}
	if got, _ := os.ReadFile(target); string(got) != in {
		t.Errorf("symlink target was written: %s", got)
	}
}

// The patched file keeps the original's permissions and leaves no temp file.
func TestSetVersionKeepsModeAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)
	if err := os.WriteFile(path, []byte(`{"version": "1.0.0"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil { //nolint:gosec // not 0600 on purpose: CreateTemp defaults to 0600, so only another mode proves it is preserved
		t.Fatal(err)
	}
	if err := SetVersion(dir, "1.25.0"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, %v; want 0640", info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}
