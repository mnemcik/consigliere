package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// pauseIn writes projects/p/resume.md in dir and commits it with a WIP commit,
// as the pause-mode wrap does.
func pauseIn(t *testing.T, ctx context.Context, dir string) {
	t.Helper()
	pauseAt(t, ctx, dir, "")
}

// pauseAt is pauseIn for a workspace at the subdirectory sub of the worktree.
func pauseAt(t *testing.T, ctx context.Context, dir, sub string) {
	t.Helper()
	p := filepath.Join(dir, sub, "projects", "p", "resume.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, dir, "add", "-A")
	mustGit(t, ctx, dir, "commit", "--quiet", "-m", "WIP pause: half done")
}

func TestCreateReusesPausedWorktree(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseIn(t, ctx, path)

	log.Reset()
	got, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil || got != path {
		t.Fatalf("Create on a paused worktree = (%q, %v), want reuse of %q", got, err, path)
	}
	if out := log.String(); !strings.Contains(out, "resuming paused worktree") || strings.Contains(out, "cg worktree land") {
		t.Errorf("log = %q, want the paused reuse and no landing hint", out)
	}
}

func TestCreateAttachesPausedBranch(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseIn(t, ctx, path)
	mustGit(t, ctx, root, "worktree", "remove", path)

	log.Reset()
	got, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil || got != path {
		t.Fatalf("Create on a paused branch = (%q, %v), want attach at %q", got, err, path)
	}
	if out := log.String(); !strings.Contains(out, "paused branch") || strings.Contains(out, "branch -D") {
		t.Errorf("log = %q, want the paused attach and no branch-delete hint", out)
	}
}

// A resume.md that was landed on main by mistake must not switch the
// unlanded-commit guard off for every worktree and branch.
func TestCreateLandedResumeKeepsGuard(t *testing.T) {
	ctx, root := setupWorkspace(t)
	pauseIn(t, ctx, root)
	mustGit(t, ctx, root, "push", "--quiet", "origin", "main")

	var log bytes.Buffer
	path, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, path, "commit", "--allow-empty", "-m", "wip work")

	_, err = Create(ctx, "p", defaultOpts(root), &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("worktree: expected ExitDirty, got %v", err)
	}

	mustGit(t, ctx, root, "worktree", "remove", path)
	_, err = Create(ctx, "p", defaultOpts(root), &log)
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("orphan branch: expected ExitDirty, got %v", err)
	}
}

// A workspace nested below the git top level: the paused worktree is still
// recognised, both with its directory and as a bare branch.
func TestCreateReusesPausedNestedWorkspace(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	var log bytes.Buffer
	path, err := Create(ctx, "p", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseAt(t, ctx, path, "vault")
	if _, err := Create(ctx, "p", localOpts(root), &log); err != nil {
		t.Fatalf("Create on a paused nested worktree: %v\n%s", err, log.String())
	}
	mustGit(t, ctx, root, "worktree", "remove", path)
	if _, err := Create(ctx, "p", localOpts(root), &log); err != nil {
		t.Fatalf("Create on a paused nested branch: %v\n%s", err, log.String())
	}
}

// Under the local strategy the landing ref is the local branch: a resume.md
// landed on it by mistake keeps the guard too.
func TestCreateLandedResumeKeepsGuardLocal(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	pauseIn(t, ctx, root)
	var log bytes.Buffer
	path, err := Create(ctx, "p", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, path, "commit", "--allow-empty", "-m", "wip work")
	_, err = Create(ctx, "p", localOpts(root), &log)
	wantExit(t, err, cgerr.ExitDirty, &log)
}

// Landing must refuse while the pause cursor is still on HEAD, including when
// it was deleted in the working tree but the deletion is not committed, and
// succeed once the deletion is committed.
func TestLandRefusesPauseCursor(t *testing.T) {
	for name, sub := range map[string]string{"top level": "", "nested workspace": "vault"} {
		t.Run(name, func(t *testing.T) {
			ctx, root := setupLocalWorkspace(t)
			var log bytes.Buffer
			wt, err := Create(ctx, "p", localOpts(root), &log)
			if err != nil {
				t.Fatal(err)
			}
			pauseAt(t, ctx, wt, sub)
			cursor := filepath.Join(wt, sub, "projects", "p", "resume.md")

			_, err = Land(ctx, localLandOpts(wt), &log)
			wantExit(t, err, cgerr.ExitDirty, &log)
			if !strings.Contains(err.Error(), "git rm") {
				t.Errorf("refusal should say how to fix it: %v", err)
			}

			if err := os.Remove(cursor); err != nil {
				t.Fatal(err)
			}
			_, err = Land(ctx, localLandOpts(wt), &log)
			wantExit(t, err, cgerr.ExitDirty, &log) // deletion not committed yet

			mustGit(t, ctx, wt, "add", "-A")
			mustGit(t, ctx, wt, "commit", "--quiet", "-m", "resume: drop cursor")
			if _, err := Land(ctx, localLandOpts(wt), &log); err != nil {
				t.Fatalf("Land after committing the deletion: %v\n%s", err, log.String())
			}
		})
	}
}

func TestLandRefusesPauseCursorRemote(t *testing.T) {
	ctx, root := setupWorkspace(t)
	var log bytes.Buffer
	wt, err := Create(ctx, "p", defaultOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	pauseIn(t, ctx, wt)
	_, err = Land(ctx, &LandOptions{WorktreeDir: wt, BranchPrefix: "session/", LandingBranch: "main",
		Strategy: workspace.StrategyDirectToMain}, &log)
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitDirty {
		t.Fatalf("expected ExitDirty, got %v", err)
	}
}

// A cursor landed by mistake, then paused over again: the new content is a
// real pause, so land still refuses it and create still reuses the worktree.
func TestRepausedOverLandedCursor(t *testing.T) {
	ctx, root := setupLocalWorkspace(t)
	pauseIn(t, ctx, root) // the stale cursor on main
	var log bytes.Buffer
	wt, err := Create(ctx, "p", localOpts(root), &log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "projects", "p", "resume.md"), []byte("new cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, wt, "commit", "--quiet", "-am", "WIP pause: again")
	if _, err := Create(ctx, "p", localOpts(root), &log); err != nil {
		t.Fatalf("Create on a re-paused worktree: %v\n%s", err, log.String())
	}
	_, err = Land(ctx, localLandOpts(wt), &log)
	wantExit(t, err, cgerr.ExitDirty, &log)
}
