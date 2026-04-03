package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectValidWorkspace(t *testing.T) {
	dir := t.TempDir()
	content := `{"type": "consigliere", "version": "1.0.0", "indexes": {"projects": "projects/TODO.md"}}`
	os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644)

	cfg, err := Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if cfg.Type != "consigliere" {
		t.Errorf("expected type 'consigliere', got '%s'", cfg.Type)
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
	content := `{"type": "other-tool", "version": "1.0.0"}`
	os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644)

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
	os.WriteFile(filepath.Join(dir, ConfigFile), []byte("not json"), 0644)

	_, err := Detect(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
