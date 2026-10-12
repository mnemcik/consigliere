package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

const gitHookManifest = `{"manifest":1,"name":"demo","version":"1.0.0","description":"d","contributes":{"git-hooks":[{"hook":"post-commit","script":"hooks/x.sh"}]}}`

// commitFile overwrites rel in repo with body and commits it.
func commitFile(t *testing.T, repo, rel, body string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := gitx.Run(ctx, repo, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, repo, "commit", "--quiet", "-m", "change "+rel); err != nil {
		t.Fatal(err)
	}
}

func entryFor(t *testing.T, ws string) workspace.ExtensionRef {
	const name = "demo"
	t.Helper()
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Extensions {
		if cfg.Extensions[i].Name == name {
			return cfg.Extensions[i]
		}
	}
	t.Fatalf("extension %q not recorded in %s", name, ws)
	return workspace.ExtensionRef{}
}

// #162 (1): updating an extension in one workspace must not change what
// another workspace on the same machine reads, including the git-hook script
// its approval is checked against.
func TestExtUpdateInOneWorkspaceLeavesAnotherAlone(t *testing.T) {
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("needs git and a POSIX shell")
	}
	cfgHome := t.TempDir()
	wsA := newGitRepo(t, filepath.Join(t.TempDir(), "a"), ".", `{"type":"consigliere","version":"1.3.0"}`)
	wsB := newGitRepo(t, filepath.Join(t.TempDir(), "b"), ".", `{"type":"consigliere","version":"1.3.0"}`)
	repo := makeExtRepo(t, gitHookManifest)

	for _, ws := range []string{wsA, wsB} {
		if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
			t.Fatalf("install in %s: %v\n%s", ws, err, out)
		}
	}
	before := entryFor(t, wsA)
	trustedBefore := extension.TrustedGitHookScripts(context.Background(), wsA, []workspace.ExtensionRef{before})

	commitFile(t, repo, "hooks/x.sh", "#!/bin/sh\necho v2\n")
	if out, err := runExtCfg(t, cfgHome, wsB, "update", "demo"); err != nil {
		t.Fatalf("update in B: %v\n%s", err, out)
	}

	if b := entryFor(t, wsB); b.Commit == before.Commit {
		t.Fatalf("B should have moved to the new commit, still at %s", b.Commit)
	}
	after := entryFor(t, wsA)
	if after.Commit != before.Commit {
		t.Errorf("A's recorded commit changed from %s to %s", before.Commit, after.Commit)
	}
	if got := readFile(t, filepath.Join(extension.SourceRoot(&after), "hooks", "x.sh")); got != "#!/bin/sh\nexit 0\n" {
		t.Errorf("A's source changed under it: %q", got)
	}
	trustedAfter := extension.TrustedGitHookScripts(context.Background(), wsA, []workspace.ExtensionRef{after})
	key := ".cg/git-hooks/post-commit.d/demo-x.sh"
	if trustedBefore[key] == "" || trustedAfter[key] != trustedBefore[key] {
		t.Errorf("A's approved script id changed: before %q, after %q", trustedBefore[key], trustedAfter[key])
	}
}

// #162 (2): a re-install on a new machine applies the recorded commit, not
// whatever the default branch holds by then.
func TestInitReinstallUsesRecordedCommit(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("# CLAUDE.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := makeExtRepo(t, testManifest)
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	recorded := entryFor(t, ws).Commit

	commitFile(t, repo, "notes/demo.md", "upstream moved on")
	// A new machine: no store, and the applied note is gone.
	if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(ws, "notes", "demo.md")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Chdir(ws)
	forceInit = true
	t.Cleanup(func() { forceInit = false })
	rootCmd.SetArgs([]string{"init", "--force"})
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init --force: %v", err)
	}

	if got := entryFor(t, ws).Commit; got != recorded {
		t.Errorf("commit after re-install = %s, want the recorded %s", got, recorded)
	}
	if got := readFile(t, filepath.Join(ws, "notes", "demo.md")); got != "note notes/demo.md" {
		t.Errorf("re-installed note = %q, want the recorded commit's content", got)
	}
}

// #162 (3): extensions with the same name from different repos don't share a
// source directory, so installing one can't replace the other.
func TestSameNameFromDifferentReposDoesNotCollide(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws1, ws2 := newWorkspace(t), newWorkspace(t)
	// Same directory name, different repos: only the URL hash tells them apart.
	sameName := func() string {
		dst := filepath.Join(t.TempDir(), "cg-extensions")
		if err := os.Rename(makeExtRepo(t, testManifest), dst); err != nil {
			t.Fatal(err)
		}
		return dst
	}
	repo1, repo2 := sameName(), sameName()
	commitFile(t, repo2, "notes/demo.md", "the other demo")

	if _, err := runExtCfg(t, cfgHome, ws1, "install", repo1); err != nil {
		t.Fatalf("install repo1: %v", err)
	}
	if _, err := runExtCfg(t, cfgHome, ws2, "install", repo2); err != nil {
		t.Fatalf("install repo2: %v", err)
	}
	src1, src2 := sourceFor(t, ws1, "demo"), sourceFor(t, ws2, "demo")
	if src1 == src2 {
		t.Fatalf("both repos map to one source: %s", src1)
	}
	if got := readFile(t, filepath.Join(src1, "notes", "demo.md")); got != "note notes/demo.md" {
		t.Errorf("repo1's source was replaced: %q", got)
	}
}

// An entry recorded before the store (no commit) still reads the legacy clone,
// and its next update records a commit.
func TestLegacyEntryReadsCloneUntilUpdated(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	ws := newWorkspace(t)
	repo := makeExtRepo(t, testManifest)
	if _, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	cfg, _ := workspace.Detect(ws)
	cfg.Extensions[0].Commit = ""
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	legacy := entryFor(t, ws)
	if got, want := extension.SourceRoot(&legacy), cloneDirFor(cfgHome, "demo"); got != want {
		t.Errorf("legacy entry reads %s, want the legacy clone %s", got, want)
	}

	if _, err := runExtCfg(t, cfgHome, ws, "update", "demo"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if c := entryFor(t, ws).Commit; !extension.ValidCommit(c) {
		t.Errorf("update recorded no commit: %q", c)
	}
}

// A tampered commit in .cg.json never becomes part of a store path.
func TestTamperedCommitFallsBackToLegacyPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range []string{"../../../etc", "abc", "HEAD", "0123456789abcdef0123456789abcdef0123456/"} {
		ref := workspace.ExtensionRef{Name: "demo", Repo: "https://example.com/x.git", Commit: c}
		if got := extension.SourceRoot(&ref); got != extension.CloneDir("demo") {
			t.Errorf("commit %q: SourceRoot = %s, want the legacy path", c, got)
		}
	}
}
