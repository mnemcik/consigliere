package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// runExt executes `cg extension <args...>` with cwd set to workdir and a fresh
// isolated config home each call. Use runExtCfg when multiple calls must share
// one config home (so the machine-shared clone persists across them).
func runExt(t *testing.T, workdir string, args ...string) (string, error) {
	t.Helper()
	return runExtCfg(t, t.TempDir(), workdir, args...)
}

// runExtCfg runs the command with an explicit config home. Package-level flag
// vars are reset so state never leaks between calls.
func runExtCfg(t *testing.T, configHome, workdir string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Chdir(workdir)
	extListJSON = false
	extInstallRef = ""
	extRemovePurge = false

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"extension"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

// cloneDirFor returns the install path of name under a given config home,
// mirroring internal/extension.CloneDir.
func cloneDirFor(configHome, name string) string {
	return filepath.Join(configHome, "consigliere", "extensions", name)
}

// updateExtRepo rewrites the fixture's manifest, commits, and optionally tags it,
// simulating an upstream release.
func updateExtRepo(t *testing.T, repo, manifest, tag string) {
	t.Helper()
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(repo, "cg-extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, repo, "commit", "--quiet", "-am", "bump"); err != nil {
		t.Fatal(err)
	}
	if tag != "" {
		if _, err := gitx.Run(ctx, repo, "tag", tag); err != nil {
			t.Fatal(err)
		}
	}
}

// newWorkspace makes a minimal Consigliere workspace (just .cg.json) in a temp dir.
func newWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := workspace.Config{Type: workspace.TypeConsigliere, Version: "1.3.0"}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// makeExtRepo creates a git repo at a temp path containing manifest plus any
// extra files, commits it, and returns the path (usable as a clone source).
func makeExtRepo(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "test"},
	} {
		if _, err := gitx.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cg-extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, dir, "commit", "--quiet", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

const testManifest = `{
  "manifest": 1,
  "name": "demo",
  "version": "0.2.0",
  "description": "a demo extension",
  "contributes": {
    "claude-md-sections": [{ "id": "demo-rules", "path": "fragments/demo.md" }],
    "notes": [{ "src": "notes/demo.md", "dest": "notes/demo.md" }]
  }
}`

func TestExtListEmpty(t *testing.T) {
	out, err := runExt(t, newWorkspace(t), "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "no extensions installed") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestExtInstallDirectFromLocalRepo(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)

	out, err := runExt(t, ws, "install", repo)
	if err != nil {
		t.Fatalf("install: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Installed demo v0.2.0") {
		t.Errorf("summary missing: %q", out)
	}
	if !strings.Contains(out, "1 CLAUDE.md section(s)") || !strings.Contains(out, "1 note(s)") {
		t.Errorf("declared contributions not summarised: %q", out)
	}

	// .cg.json records the extension.
	cfg, err := workspace.Detect(ws)
	if err != nil || cfg == nil {
		t.Fatalf("Detect after install: cfg=%v err=%v", cfg, err)
	}
	if len(cfg.Extensions) != 1 {
		t.Fatalf("expected 1 extension recorded, got %d", len(cfg.Extensions))
	}
	e := cfg.Extensions[0]
	if e.Name != "demo" || e.Version != "0.2.0" || e.Source != workspace.ExtSourceDirect || e.Repo != repo {
		t.Errorf("recorded ref wrong: %+v", e)
	}
	if e.Installed == "" {
		t.Error("installed timestamp not set")
	}

	// list now shows it (table + json).
	listOut, _ := runExt(t, ws, "list")
	if !strings.Contains(listOut, "demo") || !strings.Contains(listOut, "0.2.0") {
		t.Errorf("list missing the extension: %q", listOut)
	}
	jsonOut, _ := runExt(t, ws, "list", "--json")
	var refs []workspace.ExtensionRef
	if err := json.Unmarshal([]byte(jsonOut), &refs); err != nil {
		t.Fatalf("list --json not valid JSON: %v\n%s", err, jsonOut)
	}
	if len(refs) != 1 || refs[0].Name != "demo" {
		t.Errorf("json list wrong: %+v", refs)
	}
}

func TestExtInstallReinstallReplaces(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	if _, err := runExt(t, ws, "install", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := runExt(t, ws, "install", repo); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	cfg, _ := workspace.Detect(ws)
	if len(cfg.Extensions) != 1 {
		t.Errorf("reinstall should not duplicate the entry, got %d", len(cfg.Extensions))
	}
}

func TestExtInstallFromRegistry(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	// Registry index points the name "demo" at the local fixture repo.
	index := `{"version":1,"extensions":[{"name":"demo","description":"d","repo":"` +
		repo + `","latestVersion":"0.2.0","manifestUrl":"x"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(index))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", srv.URL)

	out, err := runExt(t, ws, "install", "demo")
	if err != nil {
		t.Fatalf("install by name: %v\n%s", err, out)
	}
	if !strings.Contains(out, "source:   registry") {
		t.Errorf("summary should show registry source: %q", out)
	}
	cfg, _ := workspace.Detect(ws)
	if len(cfg.Extensions) != 1 || cfg.Extensions[0].Source != workspace.ExtSourceRegistry {
		t.Errorf("expected one registry-sourced extension, got %+v", cfg.Extensions)
	}
	if cfg.Extensions[0].Repo != repo {
		t.Errorf("recorded repo should be the resolved URL %q, got %q", repo, cfg.Extensions[0].Repo)
	}
}

func TestExtInstallNameNotInRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":1,"extensions":[]}`))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", srv.URL)

	_, err := runExt(t, newWorkspace(t), "install", "absent")
	if err == nil {
		t.Fatal("expected an error for a name not in the registry")
	}
	if !strings.Contains(err.Error(), "not found in the registry") {
		t.Errorf("error should explain the name is unknown: %v", err)
	}
}

func TestExtRemove(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	clone := cloneDirFor(cfgHome, "demo")
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("clone should exist after install: %v", err)
	}

	// remove (no purge): entry gone, clone left in place.
	out, err := runExtCfg(t, cfgHome, ws, "remove", "demo")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, "Removed demo") || !strings.Contains(out, "left in place") {
		t.Errorf("unexpected remove output: %q", out)
	}
	cfg, _ := workspace.Detect(ws)
	if len(cfg.Extensions) != 0 {
		t.Errorf("entry should be gone, got %+v", cfg.Extensions)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Errorf("clone should remain without --purge: %v", err)
	}

	// removing an absent extension errors.
	if _, err := runExtCfg(t, cfgHome, ws, "remove", "demo"); err == nil {
		t.Error("removing an uninstalled extension should error")
	}

	// reinstall + remove --purge deletes the clone.
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "remove", "--purge", "demo"); err != nil {
		t.Fatalf("remove --purge: %v", err)
	}
	if _, err := os.Stat(clone); !os.IsNotExist(err) {
		t.Errorf("clone should be gone after --purge, stat err = %v", err)
	}
}

func TestExtUpdateUntagged(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest) // v0.2.0

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Upstream releases v0.4.0 on the default branch (no tag).
	updateExtRepo(t, repo, strings.Replace(testManifest, "0.2.0", "0.4.0", 1), "")

	out, err := runExtCfg(t, cfgHome, ws, "update", "demo")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if !strings.Contains(out, "demo: v0.2.0 → v0.4.0") {
		t.Errorf("update output should show the bump: %q", out)
	}
	cfg, _ := workspace.Detect(ws)
	if cfg.Extensions[0].Version != "0.4.0" {
		t.Errorf(".cg.json version should be 0.4.0, got %q", cfg.Extensions[0].Version)
	}
}

func TestExtUpdateTagged(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest) // v0.2.0, untagged

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Upstream tags v0.5.0.
	updateExtRepo(t, repo, strings.Replace(testManifest, "0.2.0", "0.5.0", 1), "v0.5.0")

	out, err := runExtCfg(t, cfgHome, ws, "update") // all
	if err != nil {
		t.Fatalf("update all: %v\n%s", err, out)
	}
	cfg, _ := workspace.Detect(ws)
	if cfg.Extensions[0].Version != "0.5.0" {
		t.Errorf("expected version 0.5.0 after tagged update, got %q", cfg.Extensions[0].Version)
	}
}

func TestExtInstallInvalidManifestFails(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	bad := makeExtRepo(t, `{"manifest":1,"name":"Bad Name","version":"1.0.0","description":"x","contributes":{}}`)
	_, err := runExt(t, ws, "install", bad)
	if err == nil {
		t.Fatal("expected validation failure for a bad manifest")
	}
	// Nothing should have been recorded.
	cfg, _ := workspace.Detect(ws)
	if cfg != nil && len(cfg.Extensions) != 0 {
		t.Errorf("a failed install must not record an extension: %+v", cfg.Extensions)
	}
}

func TestExtInstallOutsideWorkspaceFails(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := makeExtRepo(t, testManifest)
	// A plain temp dir with no .cg.json and no structural markers.
	_, err := runExt(t, t.TempDir(), "install", repo)
	if err == nil {
		t.Fatal("expected failure when not inside a workspace")
	}
}
