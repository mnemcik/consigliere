package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// consigliere#140: a command run from a session worktree acts on that
// worktree, not on the main checkout git reports as the common root.
func TestExtensionInstallFromLinkedWorktreeWritesThere(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere","version":"1.3.0"}`)
	wt := addWorktree(t, repo)
	ext := makeExtRepo(t, testManifest)

	if out, err := runExt(t, wt, "install", ext); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if cfg, err := workspace.Detect(wt); err != nil || cfg == nil || len(cfg.Extensions) != 1 {
		t.Errorf("worktree .cg.json does not record the extension: %+v, %v", cfg, err)
	}
	if cfg, err := workspace.Detect(repo); err != nil || cfg == nil || len(cfg.Extensions) != 0 {
		t.Errorf("main checkout .cg.json was written: %+v, %v", cfg, err)
	}
	if out, _ := gitx.StatusPorcelain(t.Context(), repo); out != "" {
		t.Errorf("main checkout has changes:\n%s", out)
	}
}

func runTagsIn(t *testing.T, dir string) string {
	t.Helper()
	t.Chdir(dir)
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"tags", "--no-auto-update"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("tags: %v\n%s", err, buf.String())
	}
	return buf.String()
}

func TestTagsReadsTheCurrentWorktree(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	wt := addWorktree(t, repo)
	writeFile(t, wt, "areas/only-here.md", "---\ntags: [session-only]\n---\n\n# Only Here\n")

	if out := runTagsIn(t, wt); !strings.Contains(out, "session-only") {
		t.Errorf("tags from the worktree did not read its areas:\n%s", out)
	}
	if out := runTagsIn(t, repo); strings.Contains(out, "session-only") {
		t.Errorf("tags from the main checkout read the worktree's areas:\n%s", out)
	}
}

// A workspace in a subdirectory of its repo is found by walking up from cwd,
// since the git toplevel carries no .cg.json.
func TestWorkspaceRootInRepoSubdirectory(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "repo"), "kb", `{"type":"consigliere"}`)
	ws := filepath.Join(repo, "kb")
	writeFile(t, ws, "areas/a.md", "---\ntags: [nested]\n---\n\n# A\n")
	if err := os.MkdirAll(filepath.Join(ws, "notes"), 0o750); err != nil {
		t.Fatal(err)
	}
	if out := runTagsIn(t, filepath.Join(ws, "notes")); !strings.Contains(out, "nested") {
		t.Errorf("workspace in a subdirectory not found:\n%s", out)
	}
}

// cmdIn is a bare command carrying the test's context, so a resolver can be
// called directly without depending on an earlier test having run rootCmd.
func cmdIn(t *testing.T) *cobra.Command {
	c := &cobra.Command{}
	c.SetContext(t.Context())
	return c
}

// A .cg.json that does not parse is reported as such, naming the file, not as
// "not inside a Consigliere workspace".
func TestWorkspaceRootReportsMalformedConfig(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere",`)
	t.Chdir(repo)
	_, err := workspaceRoot(cmdIn(t))
	if err == nil || strings.Contains(err.Error(), "not inside") || !strings.Contains(err.Error(), ".cg.json") {
		t.Errorf("want the parse error naming .cg.json, got %v", err)
	}
}

// cg share deliberately reads the main checkout, even from a session worktree:
// a publish stamps the commit it read, and a session commit may never land.
func TestShareRootIsTheMainCheckoutFromAWorktree(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	wt := addWorktree(t, repo)
	t.Chdir(wt)
	got, err := landedWorkspaceRoot(cmdIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Errorf("share root = %s, want the main checkout %s", got, repo)
	}
	if cur, _ := workspaceRoot(cmdIn(t)); cur != wt {
		t.Errorf("workspaceRoot = %s, want the worktree %s", cur, wt)
	}
}

// The same holds for a workspace in a subdirectory of its repo: the main
// checkout's copy of that subdirectory, not the worktree's.
func TestShareRootMapsASubdirectoryWorkspace(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "repo"), "kb", `{"type":"consigliere"}`)
	wt := addWorktree(t, repo)
	t.Chdir(filepath.Join(wt, "kb"))
	got, err := landedWorkspaceRoot(cmdIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repo, "kb"); got != want {
		t.Errorf("share root = %s, want %s", got, want)
	}
}

// cg share is wired to the main checkout: loadShareEnv, which every share
// subcommand goes through, resolves it from a worktree.
func TestLoadShareEnvUsesTheMainCheckout(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	wt := addWorktree(t, repo)
	t.Chdir(wt)
	env, err := loadShareEnv(cmdIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if env.root != repo {
		t.Errorf("share env root = %s, want the main checkout %s", env.root, repo)
	}
}
