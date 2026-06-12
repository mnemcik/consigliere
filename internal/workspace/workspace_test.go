package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a .cg.json with the given content into dir.
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", ConfigFile, err)
	}
}

func TestDetectValidWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"type": "consigliere", "version": "1.0.0", "indexes": {"projects": "projects/TODO.md"}}`)

	cfg, err := Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if cfg.Type != TypeConsigliere {
		t.Errorf("expected type %q, got %q", TypeConsigliere, cfg.Type)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", cfg.Version)
	}
}

func TestDetectNoConfig(t *testing.T) {
	dir := t.TempDir()

	cfg, err := Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config for empty dir, got %+v", cfg)
	}
}

func TestDetectWrongType(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `{"type": "other-tool", "version": "1.0.0"}`)

	cfg, err := Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil for wrong type, got %+v", cfg)
	}
}

func TestDetectInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "not json")

	_, err := Detect(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWorktreeSettingsDefaults(t *testing.T) {
	// nil config and config without a worktree block both yield all-defaults.
	for name, cfg := range map[string]*Config{
		"nil":       nil,
		"no-block":  {Type: TypeConsigliere},
		"empty-blk": {Type: TypeConsigliere, Worktree: &WorktreeConfig{}},
	} {
		t.Run(name, func(t *testing.T) {
			w := cfg.WorktreeSettings()
			if w.BranchPrefix != DefaultBranchPrefix {
				t.Errorf("BranchPrefix = %q, want %q", w.BranchPrefix, DefaultBranchPrefix)
			}
			if w.LandingBranch != DefaultLandingBranch {
				t.Errorf("LandingBranch = %q, want %q", w.LandingBranch, DefaultLandingBranch)
			}
			if w.LandingStrategy != DefaultLandingStrategy {
				t.Errorf("LandingStrategy = %q, want %q", w.LandingStrategy, DefaultLandingStrategy)
			}
		})
	}
}

func TestWorktreeSettingsOverrides(t *testing.T) {
	cfg := &Config{
		Type: TypeConsigliere,
		Worktree: &WorktreeConfig{
			Root:            "/custom/prefix",
			BranchPrefix:    "wt/",
			LandingBranch:   "trunk",
			LandingStrategy: StrategyPR,
		},
	}
	w := cfg.WorktreeSettings()
	if w.Root != "/custom/prefix" || w.BranchPrefix != "wt/" ||
		w.LandingBranch != "trunk" || w.LandingStrategy != StrategyPR {
		t.Errorf("overrides not preserved: %+v", w)
	}
}

func TestSessionSettingsDefaults(t *testing.T) {
	s := (*Config)(nil).SessionSettings()
	if s.ActiveWindowMin != DefaultActiveWindowMin ||
		s.DirtyWindowMin != DefaultDirtyWindowMin ||
		s.PruneDays != DefaultPruneDays ||
		s.BadgeFormat != DefaultBadgeFormat {
		t.Errorf("session defaults not applied: %+v", s)
	}
}

func TestPushPolicySettingsDefaults(t *testing.T) {
	if got := (*Config)(nil).PushPolicySettings().Source; got != DefaultPushPolicySource {
		t.Errorf("push policy source = %q, want %q", got, DefaultPushPolicySource)
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"type":"consigliere","version":"1.1.0"}`)
	sub := filepath.Join(root, "projects", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, cfg, err := FindRoot(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// t.TempDir may sit behind a symlink (e.g. /var -> /private/var on macOS);
	// compare resolved paths.
	if !sameDir(t, got, root) {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
	if cfg == nil || cfg.Version != "1.1.0" {
		t.Errorf("expected config v1.1.0, got %+v", cfg)
	}
}

func TestFindRootNotFound(t *testing.T) {
	dir := t.TempDir() // bare temp dir, no markers
	got, _, err := FindRoot(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty root, got %q", got)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return a == b
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return a == b
	}
	return ra == rb
}
