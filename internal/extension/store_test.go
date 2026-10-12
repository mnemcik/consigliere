package extension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

func TestRepoKeyIsSafeAndDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, repo := range []string{
		"https://github.com/mnemcik/cg-extensions",
		"https://github.com/mnemcik/cg-extensions.git",
		"git@github.com:acme/cg-extensions.git",
		"ssh://git@github-work/mnemcik-visma/cg-extensions.git",
		"/tmp/../../etc",
		"C:\\Users\\x\\ext",
		"..",
		"",
	} {
		k := repoKey(repo)
		if strings.ContainsAny(k, `/\:.`) || k == "" {
			t.Errorf("repoKey(%q) = %q: not a safe single path segment", repo, k)
		}
		if prev, ok := seen[k]; ok {
			t.Errorf("repoKey(%q) = repoKey(%q) = %q", repo, prev, k)
		}
		seen[k] = repo
	}
	if got := repoKey("https://github.com/mnemcik/cg-extensions"); !strings.HasPrefix(got, "cg-extensions-") {
		t.Errorf("readable part missing: %q", got)
	}
}

func TestValidCommit(t *testing.T) {
	for c, want := range map[string]bool{
		strings.Repeat("a", 40):         true,
		strings.Repeat("0", 64):         true,
		strings.Repeat("a", 39):         false,
		strings.Repeat("A", 40):         false,
		strings.Repeat("a", 41):         false,
		"HEAD":                          false,
		"../" + strings.Repeat("a", 37): false,
	} {
		if got := ValidCommit(c); got != want {
			t.Errorf("ValidCommit(%q) = %v, want %v", c, got, want)
		}
	}
}

// A tree is exported once per commit, keeps executable bits, and isn't
// touched again: a second EnsureTree for the same commit returns it as is.
func TestEnsureTreeExportsOnceWithModes(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx := context.Background()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "test"},
	} {
		if _, err := gitx.Run(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bin", "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".cg-index"), []byte("a tracked file with an awkward name"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "x"}} {
		if _, err := gitx.Run(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}

	mirror, err := EnsureMirror(ctx, repo, true)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := ResolveCommit(ctx, mirror, "")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := EnsureTree(ctx, repo, mirror, commit)
	if err != nil {
		t.Fatal(err)
	}
	if tree != TreeDir(repo, commit) {
		t.Errorf("tree at %s, want %s", tree, TreeDir(repo, commit))
	}
	if b, err := os.ReadFile(filepath.Join(tree, ".cg-index")); err != nil || string(b) != "a tracked file with an awkward name" { //nolint:gosec // test temp dir
		t.Errorf("tracked .cg-index = %q (err %v)", b, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(tree, "bin", "tool"))
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Errorf("bin/tool lost its executable bit (err %v)", err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(tree))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Errorf("staging left behind: %s", e.Name())
		}
	}

	// A marker proves the second call leaves the existing tree alone.
	marker := filepath.Join(tree, "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	if again, err := EnsureTree(ctx, repo, mirror, commit); err != nil || again != tree {
		t.Fatalf("second EnsureTree = %s, %v", again, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("second EnsureTree rewrote the existing tree")
	}

	if _, err := ResolveCommit(ctx, mirror, "no-such-ref"); err == nil {
		t.Error("resolving a missing ref should fail")
	}
	if _, err := EnsureTree(ctx, repo, mirror, "../escape"); err == nil {
		t.Error("an invalid commit must be refused")
	}
}
