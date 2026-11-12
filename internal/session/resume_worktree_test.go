package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// pausedWorktreeRepo builds a git repo whose main checkout holds the workspace
// at sub (or at the top level when sub is "") and a linked worktree, then
// writes projects/p/resume.md in the linked worktree only, as a /wrap pause
// does. It returns the workspace root in the main checkout and in the linked
// worktree. Skips when git is unavailable.
func pausedWorktreeRepo(t *testing.T, sub string) (mainWS, linkedWS string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available on PATH")
	}
	ctx := context.Background()
	base := t.TempDir()
	if r, err := filepath.EvalSymlinks(base); err == nil {
		base = r
	}
	main := filepath.Join(base, "ws")
	linked := filepath.Join(base, "ws--p")
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := gitx.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("", "init", "--quiet", "--initial-branch=main", main)
	run(main, "config", "user.email", "test@example.com")
	run(main, "config", "user.name", "Test")
	run(main, "config", "commit.gpgsign", "false")
	run(main, "commit", "--quiet", "--allow-empty", "-m", "init")
	run(main, "worktree", "add", "--quiet", "-b", "session/p", linked)

	mainWS, linkedWS = filepath.Join(main, sub), filepath.Join(linked, sub)
	if err := os.MkdirAll(mainWS, 0o755); err != nil {
		t.Fatal(err)
	}
	writeResume(t, linkedWS, "p", time.Now().Add(-time.Hour))
	return mainWS, linkedWS
}

func TestResumeFoundInAnotherWorktree(t *testing.T) {
	for name, sub := range map[string]string{"top level": "", "nested workspace": "vault"} {
		t.Run(name, func(t *testing.T) {
			mainWS, linkedWS := pausedWorktreeRepo(t, sub)
			want := []string{ResumeFile(linkedWS, "p")}

			got, err := ResumeFiles(mainWS, "p")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("ResumeFiles from main = (%v, %v), want %v", got, err, want)
			}
			got, err = ResumeFiles(linkedWS, "p")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("ResumeFiles from the paused worktree = (%v, %v), want %v", got, err, want)
			}
			if paused, err := IsPaused(mainWS, "p"); err != nil || !paused {
				t.Errorf("IsPaused from main = (%v, %v), want paused", paused, err)
			}
			if paused, err := IsPaused(mainWS, "q"); err != nil || paused {
				t.Errorf("IsPaused(q) = (%v, %v), want not paused", paused, err)
			}
		})
	}
}

// A fresh session started in the main checkout must see the pause, take the
// pausing session's claim over, and list the project once.
func TestPauseSeenFromMainCheckout(t *testing.T) {
	mainWS, linkedWS := pausedWorktreeRepo(t, "")
	root := t.TempDir()
	now := time.Now()
	writeCtx(t, root, "pauser", `{"area":"a","project":"p","dirty":false,"paused":true}`)
	touch(t, ContextFile(root, "pauser"), now.Add(-30*time.Hour)) // past the active window

	opts := ActiveOptions{WorkspaceRoot: mainWS, Now: now, ActiveWindow: 4 * time.Hour, DirtyWindow: 48 * time.Hour}
	got, err := ActiveProjects(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "pauser" || got[0].State != StatePaused {
		t.Errorf("ActiveProjects from main = %+v, want only pauser, paused", got)
	}

	// A second resume.md for p in the main checkout: still listed once.
	writeResume(t, mainWS, "p", now)
	if err := Release(root, "pauser"); err != nil {
		t.Fatal(err)
	}
	got, err = ActiveProjects(root, opts)
	if err != nil || len(got) != 1 || got[0].State != StatePaused || got[0].SessionID != "" {
		t.Errorf("badge-less listing = (%+v, %v), want p once, paused", got, err)
	}
	if err := os.Remove(ResumeFile(mainWS, "p")); err != nil {
		t.Fatal(err)
	}

	writeCtx(t, root, "pauser", `{"area":"a","project":"p","dirty":false,"paused":true}`)
	writeCtx(t, root, "me", `{"area":"a","project":"p","dirty":false}`)
	retired, err := HandOver(root, mainWS, "me", "p")
	if err != nil || !reflect.DeepEqual(retired, []string{"pauser"}) {
		t.Errorf("HandOver from main = (%v, %v), want [pauser]", retired, err)
	}

	// The pausing session ending from its own worktree keeps a marked claim.
	writeCtx(t, root, "pauser", `{"area":"a","project":"p","dirty":false,"paused":true}`)
	if released, err := EndSession(root, linkedWS, "pauser"); err != nil || released {
		t.Errorf("EndSession(pauser) = (%v, %v), want kept", released, err)
	}
}

func TestResumeDirsOutsideGit(t *testing.T) {
	ws := t.TempDir()
	if got := ResumeDirs(ws); !reflect.DeepEqual(got, []string{ws}) {
		t.Errorf("ResumeDirs outside git = %v, want [%s]", got, ws)
	}
	if got := ResumeDirs(""); got != nil {
		t.Errorf("ResumeDirs(\"\") = %v, want nil", got)
	}
}
