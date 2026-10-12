package workspace

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		return ra == b
	}
	return ra == rb
}

func TestSaveIsAtomicKeepsModeAndFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "real.json")
	if err := os.WriteFile(cfgFile, []byte(`{"type":"consigliere","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ConfigFile)
	if err := os.Symlink("real.json", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reader, err := os.Open(cfgFile) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	cfg := &Config{Type: "consigliere", Version: "2.0.0"}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	if seen, _ := io.ReadAll(reader); !strings.Contains(string(seen), `"1.0.0"`) {
		t.Errorf("a reader opened before Save saw %q, want the complete previous content", seen)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".cg.json is no longer a symlink (err %v)", err)
	}
	got, err := os.ReadFile(cfgFile) //nolint:gosec // test temp dir
	if err != nil || !strings.Contains(string(got), `"2.0.0"`) {
		t.Errorf("link target = %q (err %v), want the new config", got, err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(cfgFile); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %o, want the existing 600", info.Mode().Perm())
		}
	}
}

func TestSaveRefusesReadOnlyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)
	if err := os.WriteFile(path, []byte(`{"type":"consigliere"}`), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) }) //nolint:gosec // test temp dir
	cfg := &Config{Type: "consigliere", Version: "2.0.0"}
	if err := cfg.Save(dir); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("err = %v, want a permission error", err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"type":"consigliere"}` { //nolint:gosec // test temp dir
		t.Errorf("content = %q, want it untouched", got)
	}
}

func TestSaveReportsSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, ConfigFile)
	if err := os.Symlink(ConfigFile, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg := &Config{Type: "consigliere"}
	if err := cfg.Save(dir); err == nil {
		t.Error("Save replaced a looping symlink instead of returning its error")
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".cg.json is no longer the symlink (err %v)", err)
	}
}
