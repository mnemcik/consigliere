package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// TestInstallListsNotesInTheNotesIndex: install adds the extension's block to
// the notes index .cg.json names, and remove takes it out again.
func TestInstallListsNotesInTheNotesIndex(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := t.TempDir()
	cfg := workspace.Config{Type: workspace.TypeConsigliere, Version: "1.3.0",
		Indexes: map[string]string{dirNotes: "kb/NOTES.md"}}
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	const base = "# Notes\n"
	index := filepath.Join(ws, "kb", "NOTES.md")
	if err := os.MkdirAll(filepath.Dir(index), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, []byte(base), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	// A default-named index the config doesn't point at stays untouched.
	if err := os.MkdirAll(filepath.Join(ws, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(ws, "notes", "INDEX.md")
	if err := os.WriteFile(other, []byte(base), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	cfgHome := t.TempDir()
	repo := makeExtRepo(t, testManifest)
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(index) //nolint:gosec // test temp dir
	if !strings.Contains(string(got), "<!-- ext:demo:index:start -->\n## From the demo extension\n\n- [demo](../notes/demo.md)\n") {
		t.Errorf("index after install:\n%s", got)
	}
	if o, _ := os.ReadFile(other); string(o) != base { //nolint:gosec // test temp dir
		t.Errorf("unconfigured index changed:\n%s", o)
	}
	if rows := ledgerFor(t, ws).IndexRows; len(rows) != 1 || rows[0].File != "kb/NOTES.md" {
		t.Errorf("ledger index rows = %+v", rows)
	}

	if out, err := runExtCfg(t, cfgHome, ws, "remove", "demo"); err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(index); string(got) != base { //nolint:gosec // test temp dir
		t.Errorf("index after remove:\n%q", got)
	}
}

// An update whose old index holds a broken block stops before writing
// anything, rather than failing to reverse it after the new files are down.
func TestReinstallRefusesABrokenOldIndexBlock(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	old := filepath.Join(ws, "notes", "INDEX.md")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("# Notes\n"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	cfgHome := t.TempDir()
	repo := makeExtRepo(t, testManifest)
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	// The user breaks the block's end marker, and points .cg.json at a new index.
	data, _ := os.ReadFile(old) //nolint:gosec // test temp dir
	broken := strings.Replace(string(data), "<!-- ext:demo:index:end -->\n", "", 1)
	if err := os.WriteFile(old, []byte(broken), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	cfg, err := workspace.Detect(ws)
	if err != nil || cfg == nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Indexes = map[string]string{dirNotes: "NOTES.md"}
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(ws, "NOTES.md")
	if err := os.WriteFile(fresh, []byte("# New\n"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	if err == nil || !strings.Contains(out+err.Error(), "markers are broken") {
		t.Fatalf("reinstall: err=%v\n%s", err, out)
	}
	if got, _ := os.ReadFile(fresh); string(got) != "# New\n" { //nolint:gosec // test temp dir
		t.Errorf("new index written: %q", got)
	}
	if got, _ := os.ReadFile(old); string(got) != broken { //nolint:gosec // test temp dir
		t.Errorf("old index changed: %q", got)
	}
}
