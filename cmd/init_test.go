package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/manifest"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
}

func TestInitInstallsClaudeIntegration(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)
	forceInit = false

	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Hook wrappers + statusline exist and are executable.
	execFiles := []string{
		".claude/hooks/session-start-gate.sh",
		".claude/hooks/mark-session-dirty.sh",
		".claude/hooks/pull-latest-main.sh",
		".claude/hooks/external-repo-push-policy.sh",
		".claude/statusline.sh",
	}
	for _, f := range execFiles {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
			continue
		}
		if fi.Mode()&0o100 == 0 {
			t.Errorf("%s should be executable, mode=%v", f, fi.Mode())
		}
	}

	// settings.json + gate template exist.
	for _, f := range []string{".claude/settings.json", ".claude/cg/session-gate.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}

	// .cg.json wires the gate template.
	data, _ := os.ReadFile(filepath.Join(dir, ".cg.json"))
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	sess, _ := cfg["session"].(map[string]any)
	if sess == nil || sess["gateTemplate"] != gateTemplateRel {
		t.Errorf("expected session.gateTemplate wired in .cg.json, got %v", cfg["session"])
	}
}

func TestInitForcePreservesUserOwnedClaudeFiles(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)
	forceInit = false
	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// User edits settings.json + gate template, and a hook wrapper.
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	gatePath := filepath.Join(dir, ".claude", "cg", "session-gate.md")
	wrapperPath := filepath.Join(dir, ".claude", "hooks", "session-start-gate.sh")
	if err := os.WriteFile(settingsPath, []byte(`{"custom":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte("MY custom gate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapperPath, []byte("# clobber me\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	forceInit = true
	defer func() { forceInit = false }()
	if err := runInit(nil, nil); err != nil {
		t.Fatalf("re-init failed: %v", err)
	}

	// User-owned files preserved.
	if b, _ := os.ReadFile(settingsPath); string(b) != `{"custom":true}` {
		t.Errorf("settings.json should be preserved on --force, got %q", b)
	}
	if b, _ := os.ReadFile(gatePath); string(b) != "MY custom gate" {
		t.Errorf("gate template should be preserved on --force, got %q", b)
	}
	// Framework-owned wrapper rewritten.
	if b, _ := os.ReadFile(wrapperPath); string(b) == "# clobber me\n" {
		t.Error("hook wrapper should be rewritten on --force")
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

	// init seeds the public registry under the built-in "cg" alias.
	regs, ok := cfg["registries"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected a registries map in .cg.json, got %v", cfg["registries"])
	}
	if regs["cg"] != extension.DefaultRegistryURL {
		t.Errorf("expected registries.cg == %q, got %v", extension.DefaultRegistryURL, regs["cg"])
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
		"notes/information-propagation.md",
		"notes/idea-to-project-workflow.md",
		"notes/session-end-capture.md",
		"notes/after-pr-checks.md",
		"notes/area-rules.md",
		"insights/DRAFTS.md",
		"templates/idea.md",
		"templates/note.md",
		"templates/project/README.md",
		".claude/commands/match-project.md",
		".claude/commands/cg-init.md",
		".claude/commands/cg-sync.md",
		".claude/skills/wrap/SKILL.md",
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
