package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func runMigrateCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	migrateCmd.SilenceUsage = false
	migrateCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(append([]string{"migrate", "--no-auto-update"}, args...))
	err := rootCmd.Execute()
	return buf.String(), err
}

const migrateItem = "# Idea\n\n## Meta\n\n- **Status:** raw\n\n## What\n"

// cg migrate acts on the worktree it runs in: a workspace-wide rewrite
// started from a session worktree must not land in the main checkout.
func TestMigrateDryRunThenApplyInLinkedWorktree(t *testing.T) {
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	writeFile(t, repo, "ideas/one.md", migrateItem)
	ctx := context.Background()
	mustGit(t, ctx, repo, "add", "-A")
	mustGit(t, ctx, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "item")
	wt := addWorktree(t, repo)

	out, err := runMigrateCmd(t, wt)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Items to convert: 1") || !strings.Contains(out, "Dry run: nothing written") {
		t.Errorf("dry-run output:\n%s", out)
	}
	if got := readFile(t, filepath.Join(wt, "ideas", "one.md")); got != migrateItem {
		t.Fatalf("dry run wrote:\n%s", got)
	}

	if out, err := runMigrateCmd(t, wt, "--apply"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(wt, "ideas", "one.md")); !strings.HasPrefix(got, "---\ntitle: Idea\nstatus: raw\n---\n") {
		t.Errorf("worktree item not converted:\n%s", got)
	}
	if got := readFile(t, filepath.Join(repo, "ideas", "one.md")); got != migrateItem {
		t.Errorf("main checkout was rewritten:\n%s", got)
	}
}

func TestMigrateApplyRefusesDirtyTree(t *testing.T) {
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	writeFile(t, repo, "ideas/one.md", migrateItem)
	mustGit(t, context.Background(), repo, "add", "ideas/one.md") // staged, not committed

	out, err := runMigrateCmd(t, repo, "--apply")
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("want a dirty-tree refusal, got %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(repo, "ideas", "one.md")); got != migrateItem {
		t.Errorf("file rewritten despite the refusal:\n%s", got)
	}
}
