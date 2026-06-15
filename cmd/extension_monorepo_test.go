package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Two extensions co-located in one repo, each in its own subdir.
const ext1pwManifest = `{
  "manifest": 1,
  "name": "1pw",
  "version": "0.1.0",
  "description": "1Password credential store",
  "contributes": {
    "claude-md-sections": [{ "id": "creds", "path": "fragments/creds.md" }],
    "notes": [{ "src": "notes/creds.md", "dest": "notes/creds.md" }]
  }
}`

const extVaultManifest = `{
  "manifest": 1,
  "name": "vault",
  "version": "0.3.0",
  "description": "vault notes",
  "contributes": {
    "notes": [{ "src": "notes/vault.md", "dest": "notes/vault.md" }]
  }
}`

// makeMonorepo creates a single git repo holding several extensions, each at the
// subdir key of subdirs with its own cg-extension.json and payload stubs. Returns
// the repo path (usable as a clone source).
func makeMonorepo(t *testing.T, subdirs map[string]string) string {
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
	for sub, manifest := range subdirs {
		base := filepath.Join(dir, filepath.FromSlash(sub))
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "cg-extension.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		writeReferencedStubs(t, base, manifest) // paths resolve relative to the subdir
	}
	if _, err := gitx.Run(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, dir, "commit", "--quiet", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func findRef(refs []workspace.ExtensionRef, name string) *workspace.ExtensionRef {
	for i := range refs {
		if refs[i].Name == name {
			return &refs[i]
		}
	}
	return nil
}

// TestExtInstallSubdirDirectIsolation installs two extensions from one monorepo
// via --path and verifies each lands its own contributions, records its Path, and
// gets its own name-keyed clone.
func TestExtInstallSubdirDirectIsolation(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("# CLAUDE.md\n\nuser\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := makeMonorepo(t, map[string]string{"1pw": ext1pwManifest, "vault": extVaultManifest})

	out, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "1pw")
	if err != nil {
		t.Fatalf("install 1pw: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installed 1pw v0.1.0") || !strings.Contains(out, "subdir:   1pw") {
		t.Errorf("1pw summary wrong: %q", out)
	}
	if out2, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "vault"); err != nil {
		t.Fatalf("install vault: %v\n%s", err, out2)
	}

	cfg, _ := workspace.Detect(ws)
	if len(cfg.Extensions) != 2 {
		t.Fatalf("expected 2 extensions recorded, got %+v", cfg.Extensions)
	}
	if r := findRef(cfg.Extensions, "1pw"); r == nil || r.Path != "1pw" || r.Repo != repo {
		t.Errorf("1pw ref wrong: %+v", r)
	}
	if r := findRef(cfg.Extensions, "vault"); r == nil || r.Path != "vault" {
		t.Errorf("vault ref wrong: %+v", r)
	}

	// Both extensions' contributions landed.
	mustExist(t, filepath.Join(ws, "notes", "creds.md"))
	mustExist(t, filepath.Join(ws, "notes", "vault.md"))
	claude, _ := os.ReadFile(filepath.Join(ws, "CLAUDE.md"))
	if !strings.Contains(string(claude), "ext:1pw:section:start=creds") {
		t.Errorf("1pw section missing from CLAUDE.md:\n%s", claude)
	}
	// Each extension has its own name-keyed clone.
	mustExist(t, cloneDirFor(cfgHome, "1pw"))
	mustExist(t, cloneDirFor(cfgHome, "vault"))
}

// TestExtInstallSubdirFromRegistry resolves a registry entry carrying a path.
func TestExtInstallSubdirFromRegistry(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ws := newWorkspace(t)
	repo := makeMonorepo(t, map[string]string{"1pw": ext1pwManifest, "vault": extVaultManifest})
	index := `{"version":1,"extensions":[{"name":"1pw","description":"d","repo":"` +
		repo + `","path":"1pw","latestVersion":"0.1.0","manifestUrl":"x"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(index))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", srv.URL)

	out, err := runExt(t, ws, "install", "1pw")
	if err != nil {
		t.Fatalf("install by name: %v\n%s", err, out)
	}
	cfg, _ := workspace.Detect(ws)
	r := findRef(cfg.Extensions, "1pw")
	if r == nil || r.Source != workspace.ExtSourceRegistry || r.Path != "1pw" {
		t.Errorf("registry subdir ref wrong: %+v", r)
	}
	mustExist(t, filepath.Join(ws, "notes", "creds.md"))
}

// TestExtInstallPathFlagRejectedForName guards --path against registry-name
// installs (the entry carries its own subdir).
func TestExtInstallPathFlagRejectedForName(t *testing.T) {
	_, err := runExt(t, newWorkspace(t), "install", "somename", "--path", "1pw")
	if err == nil {
		t.Fatal("expected --path with a bare name to error")
	}
	if !strings.Contains(err.Error(), "--path applies only to direct") {
		t.Errorf("error should explain the --path restriction: %v", err)
	}
}

// TestExtInstallSubdirTraversalRejected refuses a path that escapes the repo.
func TestExtInstallSubdirTraversalRejected(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := makeMonorepo(t, map[string]string{"1pw": ext1pwManifest})
	_, err := runExt(t, newWorkspace(t), "install", repo, "--path", "../escape")
	if err == nil {
		t.Fatal("expected a traversal path to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid subdir") {
		t.Errorf("error should flag the bad subdir: %v", err)
	}
}

// TestExtUpdateSubdirIgnoresRepoTag proves a subdir extension updates off the
// default branch + manifest version, NOT a whole-repo tag (which can't map to a
// single co-located extension's version).
func TestExtUpdateSubdirIgnoresRepoTag(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeMonorepo(t, map[string]string{"1pw": ext1pwManifest}) // v0.1.0

	if _, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "1pw"); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Tag the whole repo at the v0.1.0 commit — a root extension's update would
	// check this tag out. Then advance the default branch to v0.2.0.
	if _, err := gitx.Run(ctx, repo, "tag", "v9.9.9"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "1pw", "cg-extension.json"),
		[]byte(strings.Replace(ext1pwManifest, "0.1.0", "0.2.0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, repo, "commit", "--quiet", "-am", "bump 1pw to 0.2.0"); err != nil {
		t.Fatal(err)
	}

	out, err := runExtCfg(t, cfgHome, ws, "update", "1pw")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1pw: v0.1.0 → v0.2.0") {
		t.Errorf("subdir update should track the default branch (manifest 0.2.0), not tag v9.9.9: %q", out)
	}
	cfg, _ := workspace.Detect(ws)
	if r := findRef(cfg.Extensions, "1pw"); r == nil || r.Version != "0.2.0" {
		t.Errorf(".cg.json version should be 0.2.0, got %+v", r)
	}
}

// TestExtRemoveSubdirLeavesSibling removes one co-located extension and confirms
// the sibling's clone and contributions are untouched.
func TestExtRemoveSubdirLeavesSibling(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("# CLAUDE.md\n\nuser\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := makeMonorepo(t, map[string]string{"1pw": ext1pwManifest, "vault": extVaultManifest})
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "1pw"); err != nil {
		t.Fatalf("install 1pw: %v", err)
	}
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo, "--path", "vault"); err != nil {
		t.Fatalf("install vault: %v", err)
	}

	if _, err := runExtCfg(t, cfgHome, ws, "remove", "--purge", "1pw"); err != nil {
		t.Fatalf("remove 1pw: %v", err)
	}

	// 1pw gone; vault intact.
	if _, err := os.Stat(cloneDirFor(cfgHome, "1pw")); !os.IsNotExist(err) {
		t.Errorf("1pw clone should be purged, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes", "creds.md")); !os.IsNotExist(err) {
		t.Errorf("1pw note should be reversed, stat err=%v", err)
	}
	mustExist(t, cloneDirFor(cfgHome, "vault"))
	mustExist(t, filepath.Join(ws, "notes", "vault.md"))
	cfg, _ := workspace.Detect(ws)
	if len(cfg.Extensions) != 1 || cfg.Extensions[0].Name != "vault" {
		t.Errorf("only vault should remain: %+v", cfg.Extensions)
	}
}
