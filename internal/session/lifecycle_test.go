package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

func TestHandOver(t *testing.T) {
	root := t.TempDir()
	ws := t.TempDir()
	writeCtx(t, root, "pauser", `{"area":"a","project":"p","dirty":true}`)
	writeCtx(t, root, "me", `{"area":"a","project":"p","dirty":false}`)
	writeCtx(t, root, "bystander", `{"area":"a","project":"q","dirty":true}`)

	// Not paused: nothing is retired.
	got, err := HandOver(root, ws, "me", "p")
	if err != nil || got != nil {
		t.Fatalf("HandOver on a fresh project = (%v, %v), want (nil, nil)", got, err)
	}

	p := ResumeFile(ws, "p")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("cursor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = HandOver(root, ws, "me", "p")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"pauser"}) {
		t.Errorf("retired = %v, want [pauser]", got)
	}
	for id, want := range map[string]bool{"pauser": false, "me": true, "bystander": true} {
		_, err := os.Stat(ContextFile(root, id))
		if exists := err == nil; exists != want {
			t.Errorf("badge %s exists = %v, want %v", id, exists, want)
		}
	}
}
