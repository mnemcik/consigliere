package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

const sampleCLAUDE = `# CLAUDE.md

<!-- user:section:start=purpose -->
## Purpose
User-owned content the framework must never touch.
<!-- user:section:end=purpose -->

<!-- cg:section:start=memory-policy -->
## Memory Policy
Do not use auto-memory.
<!-- cg:section:end=memory-policy -->

<!-- cg:section:start=session-start -->
## Session Start
Identify project and area.
<!-- cg:section:end=session-start -->
`

func TestParseSectionsExtractsFrameworkSectionsOnly(t *testing.T) {
	got := ParseSections(sampleCLAUDE)

	if len(got) != 2 {
		t.Fatalf("expected 2 cg:section blocks, got %d: %v", len(got), got)
	}
	if _, ok := got["purpose"]; ok {
		t.Error("user:section 'purpose' must not be parsed as a framework section")
	}
	if want := "## Memory Policy\nDo not use auto-memory."; got["memory-policy"] != want {
		t.Errorf("memory-policy inner = %q, want %q", got["memory-policy"], want)
	}
	if want := "## Session Start\nIdentify project and area."; got["session-start"] != want {
		t.Errorf("session-start inner = %q, want %q", got["session-start"], want)
	}
}

func TestParseSectionsSkipsUnmatchedStart(t *testing.T) {
	content := "<!-- cg:section:start=orphan -->\nno end marker here\n"
	if got := ParseSections(content); len(got) != 0 {
		t.Errorf("expected no sections for unmatched start, got %v", got)
	}
}

func TestHashContentIsDeterministicAndDistinct(t *testing.T) {
	first := HashContent("abc")
	second := HashContent("abc")
	if first != second {
		t.Error("hash of identical content differs")
	}
	if first == HashContent("abd") {
		t.Error("hash of different content collides")
	}
}

func TestNotesFromFSHashesFilesAndSkipsDotfiles(t *testing.T) {
	fsys := fstest.MapFS{
		"guide.md":         {Data: []byte("guide body")},
		"sub/deep.md":      {Data: []byte("deep body")},
		".gitkeep":         {Data: []byte("placeholder")},
		"sub/.editorcache": {Data: []byte("junk")},
	}

	got, err := NotesFromFS(fsys, "notes")
	if err != nil {
		t.Fatalf("NotesFromFS: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 notes (dotfiles skipped), got %d: %v", len(got), got)
	}
	if _, ok := got["notes/.gitkeep"]; ok {
		t.Error("dotfile .gitkeep must not be registered as a managed note")
	}
	if want := HashContent("guide body"); got["notes/guide.md"].Hash != want {
		t.Errorf("notes/guide.md hash = %q, want %q", got["notes/guide.md"].Hash, want)
	}
	if want := HashContent("deep body"); got["notes/sub/deep.md"].Hash != want {
		t.Errorf("notes/sub/deep.md hash = %q, want %q", got["notes/sub/deep.md"].Hash, want)
	}
}

func TestNotesFromFSEmptyTreeYieldsEmptyMap(t *testing.T) {
	got, err := NotesFromFS(fstest.MapFS{".gitkeep": {Data: []byte("x")}}, "notes")
	if err != nil {
		t.Fatalf("NotesFromFS: %v", err)
	}
	if got == nil {
		t.Fatal("expected a non-nil map")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for a notes tree with only dotfiles, got %v", got)
	}
}

func TestFromCLAUDEBuildsManifest(t *testing.T) {
	m := FromCLAUDE(sampleCLAUDE, "1.2.3")

	if m.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	if m.FrameworkVersion != "1.2.3" {
		t.Errorf("frameworkVersion = %q, want %q", m.FrameworkVersion, "1.2.3")
	}
	if len(m.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(m.Sections))
	}
	if m.Notes == nil {
		t.Error("Notes should be initialized (non-nil), even when empty")
	}
	want := HashContent("## Memory Policy\nDo not use auto-memory.")
	if m.Sections["memory-policy"].Hash != want {
		t.Errorf("memory-policy hash = %q, want %q", m.Sections["memory-policy"].Hash, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := FromCLAUDE(sampleCLAUDE, "2.0.0")

	if err := orig.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, File)); err != nil {
		t.Fatalf("manifest file not written: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected manifest, got nil")
	}
	if loaded.FrameworkVersion != orig.FrameworkVersion {
		t.Errorf("frameworkVersion round-trip mismatch: %q vs %q", loaded.FrameworkVersion, orig.FrameworkVersion)
	}
	if len(loaded.Sections) != len(orig.Sections) {
		t.Errorf("section count round-trip mismatch: %d vs %d", len(loaded.Sections), len(orig.Sections))
	}
	if loaded.Sections["session-start"].Hash != orig.Sections["session-start"].Hash {
		t.Error("session-start hash did not round-trip")
	}
}

func TestLoadReturnsNilWhenAbsent(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest for empty dir, got %+v", m)
	}
}
