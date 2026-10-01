package templates

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// TestEmbedCoversTree fails when a file under templates/ is missing from FS:
// a new top-level entry not added to the //go:embed list in embed.go, or a
// dotfile inside a directory whose pattern lost its all: prefix. It also
// fails on the reverse, so the embedded set and the tree on disk match exactly.
func TestEmbedCoversTree(t *testing.T) {
	onDisk := map[string]bool{}
	err := fs.WalkDir(os.DirFS("."), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".go") {
			return err
		}
		onDisk[p] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates/: %v", err)
	}

	embedded := map[string]bool{}
	err = fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embedded[p] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded FS: %v", err)
	}

	for p := range onDisk {
		if !embedded[p] {
			t.Errorf("templates/%s is not embedded; add its top-level entry to the //go:embed list in embed.go", p)
		}
	}
	for p := range embedded {
		if !onDisk[p] {
			t.Errorf("embedded file %s has no counterpart under templates/", p)
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("found no files under templates/; the test is not looking at the tree")
	}
}
