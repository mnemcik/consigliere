package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
)

// Removing the extension that contributed the last script for a hook removes
// the dispatcher and puts back the hook that was there before cg.
func TestExtRemoveRestoresForeignGitHook(t *testing.T) {
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("needs git and a POSIX shell")
	}
	cfgHome := t.TempDir()
	ws := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere","version":"1.3.0"}`)
	repo := makeExtRepo(t, `{"manifest":1,"name":"demo","version":"1.0.0","description":"d","contributes":{"git-hooks":[{"hook":"post-commit","script":"hooks/x.sh"}]}}`)

	hook := filepath.Join(ws, ".git", "hooks", "post-commit")
	foreign := "#!/bin/sh\necho mine\n"
	if err := os.WriteFile(hook, []byte(foreign), 0o755); err != nil { //nolint:gosec // test hook
		t.Fatal(err)
	}

	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if got := readFile(t, hook); !strings.Contains(got, "cg-git-hook-dispatcher") {
		t.Fatalf("dispatcher not installed:\n%s", got)
	}
	if got := readFile(t, hook+".pre-cg"); got != foreign {
		t.Fatalf("foreign hook not moved aside: %q", got)
	}

	if out, err := runExtCfg(t, cfgHome, ws, "remove", "demo"); err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if got := readFile(t, hook); got != foreign {
		t.Errorf("foreign hook not restored: %q", got)
	}
	if _, err := os.Lstat(hook + ".pre-cg"); !os.IsNotExist(err) {
		t.Errorf("post-commit.pre-cg left behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".cg", "git-hooks", "post-commit.d", "demo-x.sh")); !os.IsNotExist(err) {
		t.Errorf("contributed script left behind: %v", err)
	}
}

// From a linked worktree, an unapproved script in the main worktree is
// reported with the reason: what this worktree installed applies once landed.
func TestEnsureGitHooksExplainsLinkedWorktree(t *testing.T) {
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("needs git and a POSIX shell")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx := context.Background()
	repo := newGitRepo(t, filepath.Join(t.TempDir(), "ws"), ".", `{"type":"consigliere"}`)
	writeFile(t, repo, ".cg/git-hooks/post-commit.d/demo-x.sh", "#!/bin/sh\nexit 0\n")
	wt := addWorktree(t, repo)

	main := strings.Join(ensureGitHooks(ctx, repo), "\n")
	if !strings.Contains(main, "demo-x.sh") || strings.Contains(main, "linked worktree") {
		t.Errorf("main worktree message = %q", main)
	}
	linked := strings.Join(ensureGitHooks(ctx, wt), "\n")
	if !strings.Contains(linked, "demo-x.sh") || !strings.Contains(linked, "linked worktree") {
		t.Errorf("linked worktree message = %q", linked)
	}
}

// Session start reconciles the git hooks after the pull, so a script that
// arrives with the pull is approved in the same session start, not the next.
func TestSessionPullLatestApprovesPulledScript(t *testing.T) {
	if !gitx.Available() || runtime.GOOS == "windows" {
		t.Skip("needs git and a POSIX shell")
	}
	clearSessionEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx := context.Background()
	script := "#!/bin/sh\nexit 0\n"
	clone := extension.CloneDir("demo")
	writeFile(t, clone, "cg-extension.json", `{"manifest":1,"name":"demo","version":"1.0.0","description":"d","contributes":{"git-hooks":[{"hook":"post-commit","script":"x.sh"}]}}`)
	writeFile(t, clone, "x.sh", script)

	origin := newGitRepo(t, filepath.Join(t.TempDir(), "origin"), ".", `{"type":"consigliere","extensions":[{"name":"demo","version":"1.0.0","source":"direct","repo":"r"}]}`)
	mustGit(t, ctx, origin, "branch", "-M", "main")
	ws := filepath.Join(filepath.Dir(origin), "ws")
	mustGit(t, ctx, "", "clone", "--quiet", origin, ws)
	writeFile(t, origin, ".cg/git-hooks/post-commit.d/demo-x.sh", script)
	mustGit(t, ctx, origin, "add", "-A")
	mustGit(t, ctx, origin, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "add script")

	out, err := runSession(t, ws, "{}", "pull-latest")
	if err != nil {
		t.Fatalf("pull-latest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Pulled") {
		t.Fatalf("pull-latest did not pull: %q", out)
	}
	if got := readFile(t, filepath.Join(ws, ".git", "hooks", "post-commit")); !strings.Contains(got, "cg-git-hook-dispatcher") {
		t.Errorf("dispatcher not installed for the pulled script:\n%s", got)
	}
}
