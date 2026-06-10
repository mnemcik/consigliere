package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/manifest"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
}

func TestInitCreatesWorkspace(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)

	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify .cg.json exists and is valid
	data, err := os.ReadFile(filepath.Join(dir, ".cg.json"))
	if err != nil {
		t.Fatalf("cannot read .cg.json: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON in .cg.json: %v", err)
	}

	if cfg["type"] != "consigliere" {
		t.Errorf("expected type 'consigliere', got %v", cfg["type"])
	}

	// Verify directories exist
	expectedDirs := []string{"projects", "areas", "ideas", "notes", "insights", "templates", "templates/project"}
	for _, d := range expectedDirs {
		info, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Errorf("directory %s not created: %v", d, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}

	// Verify key files exist
	expectedFiles := []string{
		"CLAUDE.md",
		"PROFILE.md",
		"projects/TODO.md",
		"areas/INDEX.md",
		"ideas/BACKLOG.md",
		"notes/INDEX.md",
		"notes/claude-md-hygiene.md",
		"notes/project-structure.md",
		"insights/DRAFTS.md",
		"templates/idea.md",
		"templates/note.md",
		"templates/project/README.md",
		".claude/commands/match-project.md",
		".claude/commands/cg-init.md",
		".claude/commands/cg-sync.md",
	}
	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("file %s not created: %v", f, err)
		}
	}
}

func TestInitSkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)

	// Create a custom PROFILE.md before init
	customContent := "# My Custom Profile\n"
	if err := os.WriteFile(filepath.Join(dir, "PROFILE.md"), []byte(customContent), 0o644); err != nil {
		t.Fatalf("cannot write PROFILE.md: %v", err)
	}

	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify custom PROFILE.md was preserved
	data, err := os.ReadFile(filepath.Join(dir, "PROFILE.md"))
	if err != nil {
		t.Fatalf("cannot read PROFILE.md: %v", err)
	}
	if string(data) != customContent {
		t.Errorf("PROFILE.md was overwritten, got: %s", string(data))
	}
}

func TestInitGuardExistingWorkspace(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)

	// First init
	if err := runInit(nil, nil); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	// Second init without --force should not error (just print message)
	forceInit = false
	if err := runInit(nil, nil); err != nil {
		t.Fatalf("second init should not fail: %v", err)
	}
}

func TestInitSeedsManifest(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)

	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	mf, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if mf == nil {
		t.Fatal("expected manifest at .cg/manifest.json, got none")
	}
	if mf.SchemaVersion != manifest.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", mf.SchemaVersion, manifest.SchemaVersion)
	}
	if mf.FrameworkVersion != Version {
		t.Errorf("frameworkVersion = %q, want %q (build Version)", mf.FrameworkVersion, Version)
	}
	if len(mf.Sections) == 0 {
		t.Fatal("expected the seeded manifest to record CLAUDE.md sections, got none")
	}
	// A known framework section should be tracked, and its recorded hash must
	// match the hash of that section as parsed from the on-disk CLAUDE.md —
	// i.e. on a fresh init the manifest agrees with the workspace (no drift).
	claudeMD, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("cannot read CLAUDE.md: %v", err)
	}
	parsed := manifest.ParseSections(string(claudeMD))
	if _, ok := mf.Sections["session-start"]; !ok {
		t.Error("expected 'session-start' section to be tracked in the manifest")
	}
	for id, art := range mf.Sections {
		want := manifest.HashContent(parsed[id])
		if art.Hash != want {
			t.Errorf("section %q: manifest hash %q != on-disk hash %q (fresh init should not show drift)", id, art.Hash, want)
		}
	}

	// PR1 of load-on-demand ships the first framework note (claude-md-hygiene).
	// It must be copied into the workspace AND registered in the manifest, with
	// the recorded hash matching the on-disk file (no drift on a fresh init).
	// The .gitkeep alongside it must never be copied as a managed note.
	if mf.Notes == nil {
		t.Error("expected a non-nil notes map in the seeded manifest")
	}
	// Assert the hygiene note specifically rather than an exact total count:
	// later load-on-demand PRs ship more framework notes, and a count check
	// would force a churn edit here on each one. Presence + a matching hash is
	// the invariant that matters.
	const hygieneNote = "notes/claude-md-hygiene.md"
	hygieneArt, ok := mf.Notes[hygieneNote]
	if !ok {
		t.Fatalf("expected %q in the manifest notes, got %v", hygieneNote, mf.Notes)
	}
	hygieneOnDisk, rerr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(hygieneNote)))
	if rerr != nil {
		t.Fatalf("expected %s on disk: %v", hygieneNote, rerr)
	}
	if want := manifest.HashContent(string(hygieneOnDisk)); hygieneArt.Hash != want {
		t.Errorf("note %q: manifest hash %q != on-disk hash %q (fresh init should not show drift)", hygieneNote, hygieneArt.Hash, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", ".gitkeep")); err == nil {
		t.Error("notes/.gitkeep must not be copied into the workspace")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "INDEX.md")); err != nil {
		t.Errorf("expected notes/INDEX.md to exist: %v", err)
	}
}
