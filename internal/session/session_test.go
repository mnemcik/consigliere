package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeCtx(t *testing.T, root, id, content string) {
	t.Helper()
	dir := ContextDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ContextFile(root, id), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadContext(t *testing.T) {
	root := t.TempDir()

	// Missing file → (nil, nil).
	c, err := ReadContext(root, "nope")
	if err != nil || c != nil {
		t.Fatalf("missing ctx: got (%+v, %v), want (nil, nil)", c, err)
	}

	writeCtx(t, root, "s1", `{"area":"consigliere","project":"cg-subcommands","dirty":true}`)
	c, err = ReadContext(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Area != "consigliere" || c.Project != "cg-subcommands" || !c.Dirty {
		t.Errorf("unexpected context: %+v", c)
	}
}

func TestIsContextPath(t *testing.T) {
	root := "/ws"
	ctxFile := ContextFile(root, "abc")
	cases := map[string]bool{
		ctxFile: true,
		filepath.Join(ContextDir(root), "x.json"): true,
		filepath.Join(root, "notes", "n.md"):      false,
		"":                                        false,
	}
	for p, want := range cases {
		if got := IsContextPath(root, p); got != want {
			t.Errorf("IsContextPath(%q) = %v, want %v", p, got, want)
		}
	}
	// Relative path resolved against root.
	if !IsContextPath(root, filepath.Join(ContextDirName, "rel.json")) {
		t.Error("expected relative context path to match")
	}
}

func TestMarkDirty(t *testing.T) {
	root := t.TempDir()

	// No file → no-op, no error, no file created.
	if err := MarkDirty(root, "ghost"); err != nil {
		t.Fatalf("MarkDirty on missing file: %v", err)
	}
	if _, err := os.Stat(ContextFile(root, "ghost")); !os.IsNotExist(err) {
		t.Error("MarkDirty must not create a file for an untracked session")
	}

	// Existing file → dirty flipped, other fields preserved.
	writeCtx(t, root, "s1", `{"area":"a","project":"p","note":"keep me"}`)
	if err := MarkDirty(root, "s1"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	data, _ := os.ReadFile(ContextFile(root, "s1"))
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["dirty"] != true {
		t.Errorf("dirty = %v, want true", m["dirty"])
	}
	if m["area"] != "a" || m["project"] != "p" || m["note"] != "keep me" {
		t.Errorf("unknown/known fields not preserved: %+v", m)
	}
}
