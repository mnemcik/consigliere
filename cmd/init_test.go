package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesWorkspace(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	err := runInit(nil, nil)
	if err != nil {
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
		"insights/DRAFTS.md",
		"templates/idea.md",
		"templates/note.md",
		"templates/project/README.md",
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
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// Create a custom PROFILE.md before init
	customContent := "# My Custom Profile\n"
	os.WriteFile(filepath.Join(dir, "PROFILE.md"), []byte(customContent), 0644)

	err := runInit(nil, nil)
	if err != nil {
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
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// First init
	runInit(nil, nil)

	// Second init without --force should not error (just print message)
	forceInit = false
	err := runInit(nil, nil)
	if err != nil {
		t.Fatalf("second init should not fail: %v", err)
	}
}
