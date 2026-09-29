package session

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRelease(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","dirty":true}`)
	if err := Release(root, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ContextFile(root, "s1")); !os.IsNotExist(err) {
		t.Error("Release must delete the badge file")
	}
	// Releasing again (no file) succeeds.
	if err := Release(root, "s1"); err != nil {
		t.Errorf("second Release: %v", err)
	}
	if err := Release(root, "../x"); err == nil {
		t.Error("Release must reject an unsafe session ID")
	}
}

// touch sets a file's modification time.
func touch(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

// writeResume writes a project's resume.md with the given modification time.
func writeResume(t *testing.T, ws, project string, mt time.Time) {
	t.Helper()
	p := ResumeFile(ws, project)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	touch(t, p, mt)
}

func TestHandOver(t *testing.T) {
	root := t.TempDir()
	ws := t.TempDir()
	writeCtx(t, root, "pauser", `{"area":"a","project":"p","dirty":true,"paused":true}`)
	writeCtx(t, root, "resumer", `{"area":"a","project":"p","dirty":false}`)
	writeCtx(t, root, "me", `{"area":"a","project":"p","dirty":false}`)
	writeCtx(t, root, "bystander", `{"area":"a","project":"q","dirty":true,"paused":true}`)

	// Not paused: nothing is retired, not even a marked badge.
	got, err := HandOver(root, ws, "me", "p")
	if err != nil || got != nil {
		t.Fatalf("HandOver on a fresh project = (%v, %v), want (nil, nil)", got, err)
	}

	// Paused: only the badge carrying the pause marker is retired; a session
	// that already resumed the project keeps its claim.
	writeResume(t, ws, "p", time.Now())
	got, err = HandOver(root, ws, "me", "p")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"pauser"}) {
		t.Errorf("retired = %v, want [pauser]", got)
	}
	for id, want := range map[string]bool{"pauser": false, "resumer": true, "me": true, "bystander": true} {
		_, err := os.Stat(ContextFile(root, id))
		if exists := err == nil; exists != want {
			t.Errorf("badge %s exists = %v, want %v", id, exists, want)
		}
	}
}

func TestIsPausedSurfacesErrors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	ws := t.TempDir()
	dir := filepath.Join(ws, "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // a directory needs its execute bit back so TempDir cleanup can remove it
	if _, err := IsPaused(ws, "p"); err == nil {
		t.Error("IsPaused must return a permission error, not report the project as not paused")
	}
	if paused, err := IsPaused(ws, "missing"); err != nil || paused {
		t.Errorf("missing resume.md = (%v, %v), want not paused and no error", paused, err)
	}
}

func TestPauseMarkerIdentifiesThePausingSession(t *testing.T) {
	root := t.TempDir()
	ws := t.TempDir()
	now := time.Now()
	earlier := now.Add(-2 * time.Hour)

	// A and B both work on p; both badges predate the pause. B pauses.
	for _, id := range []string{"A", "B"} {
		if err := WriteContext(root, id, "a", "p"); err != nil {
			t.Fatal(err)
		}
		touch(t, ContextFile(root, id), earlier)
	}
	if marked, err := MarkPaused(root, "B", now); err != nil || !marked {
		t.Fatalf("MarkPaused(B) = (%v, %v)", marked, err)
	}
	touch(t, ContextFile(root, "B"), earlier) // both badges predate resume.md
	writeResume(t, ws, "p", now.Add(-time.Hour))

	opts := ActiveOptions{WorkspaceRoot: ws, Now: now, ActiveWindow: 4 * time.Hour, DirtyWindow: 48 * time.Hour}
	got, err := ActiveProjects(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, s := range got {
		states[s.SessionID] = s.State
	}
	if states["A"] != StateLive || states["B"] != StatePaused {
		t.Errorf("states = %v, want A live and B paused", states)
	}

	// C resumes p: only B's claim is handed over.
	if err := WriteContext(root, "C", "a", "p"); err != nil {
		t.Fatal(err)
	}
	retired, err := HandOver(root, ws, "C", "p")
	if err != nil || !reflect.DeepEqual(retired, []string{"B"}) {
		t.Errorf("HandOver retired %v (%v), want [B]", retired, err)
	}

	// A ends cleanly: its claim is released, not kept as a pause.
	if released, err := EndSession(root, ws, "A"); err != nil || !released {
		t.Errorf("EndSession(A) = (%v, %v), want released", released, err)
	}
}

func TestSetContextClearsPauseMarker(t *testing.T) {
	root := t.TempDir()
	if err := WriteContext(root, "s1", "a", "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkPaused(root, "s1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if c, _ := ReadContext(root, "s1"); c == nil || !c.Paused {
		t.Fatalf("MarkPaused did not set the marker: %+v", c)
	}
	// The same session resumes its own pause (session IDs survive a resume).
	if err := WriteContext(root, "s1", "a", "p"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ContextFile(root, "s1"))
	if c, _ := ReadContext(root, "s1"); c.Paused || strings.Contains(string(data), "pausedAt") {
		t.Errorf("set-context must clear the pause marker:\n%s", data)
	}
	// No badge: nothing to mark, not an error.
	if marked, err := MarkPaused(root, "ghost", time.Now()); err != nil || marked {
		t.Errorf("MarkPaused(ghost) = (%v, %v), want (false, nil)", marked, err)
	}
}
