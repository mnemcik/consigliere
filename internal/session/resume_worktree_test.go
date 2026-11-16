package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
			want := ResumeFile(linkedWS, "p")

			for from, ws := range map[string]string{"main": mainWS, "paused worktree": linkedWS} {
				got, err := FindResume(ws, "p")
				if err != nil || len(got) != 1 || got[0].File != want {
					t.Fatalf("FindResume from %s = (%+v, %v), want %s", from, got, err, want)
				}
				if own := canonical(ws) == canonical(linkedWS); got[0].Checkout.Own != own {
					t.Errorf("FindResume from %s: Own = %v, want %v", from, got[0].Checkout.Own, own)
				}
				if got[0].Checkout.Branch != "session/p" {
					t.Errorf("FindResume from %s: Branch = %q, want session/p", from, got[0].Checkout.Branch)
				}
			}
			if paused, err := IsPaused(mainWS, "p"); err != nil || !paused {
				t.Errorf("IsPaused from main = (%v, %v), want paused", paused, err)
			}
			if paused, err := IsPaused(mainWS, "q"); err != nil || paused {
				t.Errorf("IsPaused(q) = (%v, %v), want not paused", paused, err)
			}
			got := PausedProjects(mainWS)
			if len(got) != 1 || got[0].Project != "p" || got[0].File != want {
				t.Errorf("PausedProjects from main = %+v, want p at %s", got, want)
			}
		})
	}
}

// An unreadable directory in another worktree must not break the lookup; the
// same error in the caller's own checkout is still reported.
func TestResumeSkipsUnreadableOtherWorktree(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	mainWS, linkedWS := pausedWorktreeRepo(t, "")
	dir := filepath.Join(linkedWS, "projects", "p")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore so TempDir cleanup can remove it
	if paused, err := IsPaused(mainWS, "p"); err != nil || paused {
		t.Errorf("IsPaused from main = (%v, %v), want not paused and no error", paused, err)
	}
	if _, err := IsPaused(linkedWS, "p"); err == nil {
		t.Error("IsPaused from its own checkout must report the permission error")
	}
}

func TestGateListsPausedProjects(t *testing.T) {
	mainWS, linkedWS := pausedWorktreeRepo(t, "")
	text, emit := Gate(context.Background(), mainWS,
		GateInput{Prompt: "carry on", SessionID: "s", CWD: mainWS, WorkspaceRoot: mainWS}, "", 0)
	if !emit || !strings.Contains(text, "Paused projects") || !strings.Contains(text, "p: "+ResumeFile(linkedWS, "p")) {
		t.Errorf("gate text lacks the paused project:\n%s", text)
	}
	if !strings.HasSuffix(text, "</user-prompt-submit-hook>") {
		t.Errorf("notice must sit inside the hook block:\n%s", text)
	}
	text, _ = Gate(context.Background(), mainWS,
		GateInput{Prompt: "carry on", SessionID: "s", CWD: mainWS}, "", 0)
	if strings.Contains(text, "Paused projects") {
		t.Error("no WorkspaceRoot: the gate must not list paused projects")
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

func TestCheckoutsOutsideGit(t *testing.T) {
	ws := t.TempDir()
	if got := Checkouts(ws); !reflect.DeepEqual(got, []Checkout{{Dir: ws, Own: true}}) {
		t.Errorf("Checkouts outside git = %+v, want only %s", got, ws)
	}
	if got := Checkouts(""); got != nil {
		t.Errorf("Checkouts(\"\") = %v, want nil", got)
	}
}

// A dangling resume.md symlink in the caller's checkout must not hide a real
// cursor in another worktree.
func TestPausedProjectsSkipsDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	mainWS, linkedWS := pausedWorktreeRepo(t, "")
	dangling := ResumeFile(mainWS, "p")
	if err := os.MkdirAll(filepath.Dir(dangling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(mainWS, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	got := PausedProjects(mainWS)
	if len(got) != 1 || got[0].File != ResumeFile(linkedWS, "p") {
		t.Errorf("PausedProjects = %+v, want the worktree's cursor", got)
	}
}
