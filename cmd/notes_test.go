package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/mnemcik/consigliere/internal/manifest"
)

// copyFrameworkNotes and manifest.NotesFromFS must agree exactly: every file the
// copier writes to disk must be registered in the manifest, and vice versa.
// Dotfiles are excluded from both; pre-existing files are preserved, not
// clobbered. This guards the invariant that every manifest note record has a
// backing file (and no orphan files exist).
func TestCopyFrameworkNotesMatchesManifestRegistration(t *testing.T) {
	srcFS := fstest.MapFS{
		"hygiene.md":      {Data: []byte("hygiene body")},
		"sub/deep.md":     {Data: []byte("deep body")},
		".gitkeep":        {Data: []byte("placeholder")},
		"sub/.tmpjunk":    {Data: []byte("junk")},
		".git/config":     {Data: []byte("must be pruned")},
		"sub/.cache/c.md": {Data: []byte("nested hidden")},
	}
	dir := t.TempDir()

	created, skipped, err := copyFrameworkNotes(srcFS, dir, "notes")
	if err != nil {
		t.Fatalf("copyFrameworkNotes: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("expected nothing skipped on a fresh copy, got %v", skipped)
	}

	// Real notes copied to notes/<rel> with original content; dotfiles excluded.
	for rel, want := range map[string]string{
		"notes/hygiene.md":  "hygiene body",
		"notes/sub/deep.md": "deep body",
	} {
		got, rerr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Errorf("expected %s on disk: %v", rel, rerr)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", ".gitkeep")); err == nil {
		t.Error("notes/.gitkeep must not be copied")
	}
	// Hidden directories must be pruned, not descended into.
	for _, pruned := range []string{"notes/.git/config", "notes/sub/.cache/c.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(pruned))); err == nil {
			t.Errorf("%s under a dot-directory must not be copied", pruned)
		}
	}

	// The created set must equal the manifest's registered note keys.
	notes, err := manifest.NotesFromFS(srcFS, "notes")
	if err != nil {
		t.Fatalf("NotesFromFS: %v", err)
	}
	if len(created) != len(notes) {
		t.Errorf("copied %d files but manifest registered %d: created=%v notes=%v", len(created), len(notes), created, notes)
	}
	for _, rel := range created {
		if _, ok := notes[rel]; !ok {
			t.Errorf("copied %q is not registered in the manifest", rel)
		}
	}
}

func TestCopyFrameworkNotesPreservesExistingFiles(t *testing.T) {
	srcFS := fstest.MapFS{"hygiene.md": {Data: []byte("framework body")}}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "notes", "hygiene.md")
	if err := os.WriteFile(existing, []byte("user edits"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, skipped, err := copyFrameworkNotes(srcFS, dir, "notes")
	if err != nil {
		t.Fatalf("copyFrameworkNotes: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("expected nothing created when the note already exists, got %v", created)
	}
	if len(skipped) != 1 || skipped[0] != "notes/hygiene.md" {
		t.Errorf("expected notes/hygiene.md skipped, got %v", skipped)
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "user edits" {
		t.Errorf("existing note was clobbered: content = %q, want %q", got, "user edits")
	}
}
