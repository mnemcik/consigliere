package share

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentHash(t *testing.T) {
	a := map[string]string{"p/README.md": "r", "p/todo.md": "t"}
	b := map[string]string{"p/todo.md": "t", "p/README.md": "r"}
	if ContentHash(a) != ContentHash(b) {
		t.Error("hash must not depend on map order")
	}
	for name, other := range map[string]map[string]string{
		"content change": {"p/README.md": "r!", "p/todo.md": "t"},
		"file added":     {"p/README.md": "r", "p/todo.md": "t", "p/log.md": ""},
		"boundary shift": {"p/README.mdr": "", "p/todo.md": "t"},
	} {
		if ContentHash(other) == ContentHash(a) {
			t.Errorf("%s: hash unchanged", name)
		}
	}
}

func TestPublishedEntry(t *testing.T) {
	res := &Result{
		Files: map[string]string{"pilot/todo.md": "t", "pilot/README.md": "r"},
		Stamp: Stamp{SHA: "abc", Date: "2026-09-25"},
	}
	e := PublishedEntry(res)
	if e.SourceCommit != "abc" || e.SourceDate != "2026-09-25" || strings.Join(e.Files, ",") != "README.md,todo.md" || e.ContentHash != ContentHash(res.Files) {
		t.Errorf("got %+v", e)
	}
}

// shareRemote creates a bare repo standing in for a share repo, optionally
// with a commit on main carrying the given manifest.
func shareRemote(t *testing.T, manifest *Manifest) string {
	t.Helper()
	ctx, work := initWorkspace(t)
	bare := filepath.Join(t.TempDir(), "share.git")
	git(t, ctx, "", "init", "--bare", "--initial-branch=main", bare)
	if manifest == nil {
		return bare
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	write(t, work, ManifestFile, string(data))
	git(t, ctx, work, "add", ManifestFile)
	git(t, ctx, work, "commit", "-m", "publish")
	git(t, ctx, work, "push", bare, "HEAD:main")
	return bare
}

func TestReadPublished(t *testing.T) {
	ctx := context.Background()
	want := &Manifest{Version: 1, Owner: "Ada", Audience: "team", Projects: map[string]PublishedProject{
		"pilot": {SourceCommit: "abc", SourceDate: "2026-09-25", Files: []string{"README.md"}, ContentHash: "h"},
	}}
	got, err := ReadPublished(ctx, shareRemote(t, want), "main")
	if err != nil {
		t.Fatalf("ReadPublished: %v", err)
	}
	if got == nil || got.Projects["pilot"].ContentHash != "h" || got.Owner != "Ada" {
		t.Errorf("got %+v", got)
	}
}

func TestReadPublishedNothingYet(t *testing.T) {
	ctx := context.Background()
	if m, err := ReadPublished(ctx, shareRemote(t, nil), "main"); m != nil || err != nil {
		t.Errorf("empty repo: want (nil, nil), got (%v, %v)", m, err)
	}
	withManifest := shareRemote(t, &Manifest{Version: 1})
	if m, err := ReadPublished(ctx, withManifest, "other"); m != nil || err != nil {
		t.Errorf("missing branch: want (nil, nil), got (%v, %v)", m, err)
	}
}

func TestReadPublishedErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := ReadPublished(ctx, filepath.Join(t.TempDir(), "missing.git"), "main"); err == nil {
		t.Error("an unreachable repo must be an error, not 'nothing published'")
	}
	newer := shareRemote(t, &Manifest{Version: ManifestVersion + 1})
	if _, err := ReadPublished(ctx, newer, "main"); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("want a newer-version error, got %v", err)
	}
}
