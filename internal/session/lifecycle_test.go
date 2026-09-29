package session

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	pausedAt := time.Now().Add(-time.Hour)
	badge := func(id, content string, mt time.Time) {
		writeCtx(t, root, id, content)
		touch(t, ContextFile(root, id), mt)
	}
	badge("pauser", `{"area":"a","project":"p","dirty":true}`, pausedAt.Add(-time.Minute))
	badge("resumer", `{"area":"a","project":"p","dirty":false}`, pausedAt.Add(time.Minute))
	badge("me", `{"area":"a","project":"p","dirty":false}`, time.Now())
	badge("bystander", `{"area":"a","project":"q","dirty":true}`, pausedAt.Add(-time.Minute))

	// Not paused: nothing is retired.
	got, err := HandOver(root, ws, "me", "p")
	if err != nil || got != nil {
		t.Fatalf("HandOver on a fresh project = (%v, %v), want (nil, nil)", got, err)
	}

	// Paused: only the badge written no later than resume.md is retired; the
	// session that already resumed it after the pause keeps its claim.
	writeResume(t, ws, "p", pausedAt)
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

func TestPausedSinceSurfacesErrors(t *testing.T) {
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
	if _, _, err := PausedSince(ws, "p"); err == nil {
		t.Error("PausedSince must return a permission error, not report the project as not paused")
	}
	if _, paused, err := PausedSince(ws, "missing"); err != nil || paused {
		t.Errorf("missing resume.md = (%v, %v), want not paused and no error", paused, err)
	}
}
