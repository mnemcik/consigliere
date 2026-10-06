package extension

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// A script is trusted under its workspace install path with the blob id of
// the clone's copy; an extension without a clone, or with a clone of another
// name, contributes nothing.
func TestTrustedGitHookScripts(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo := t.TempDir()
	if _, err := gitx.Run(ctx, "", "init", "--quiet", repo); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Manifest: 1, Name: "demo", Version: "1.0.0", Description: "d",
		Contributes: Contributions{GitHooks: []GitHookContribution{{Hook: "post-commit", Script: "hooks/b.sh"}}},
	}
	clone := filepath.Join(CloneDir("demo"), "sub")
	if err := os.MkdirAll(filepath.Join(clone, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "hooks", "b.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test script
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, ManifestFile), []byte(`{"manifest":1,"name":"demo","version":"1.0.0","description":"d","contributes":{"git-hooks":[{"hook":"post-commit","script":"hooks/b.sh"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := []workspace.ExtensionRef{{Name: "demo", Path: "sub"}, {Name: "missing"}}
	got := TrustedGitHookScripts(ctx, repo, refs)
	want, err := gitx.Run(ctx, repo, "hash-object", "--no-filters", "--", filepath.Join(clone, "hooks", "b.sh"))
	if err != nil {
		t.Fatal(err)
	}
	rel := gitHookDestRel("demo", m.Contributes.GitHooks[0])
	if len(got) != 1 || got[rel] != want {
		t.Errorf("trusted = %v, want {%s: %s}", got, rel, want)
	}
}
