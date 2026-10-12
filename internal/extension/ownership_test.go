package extension

import "testing"

func TestDecideCopy(t *testing.T) {
	const b, d, n = "base", "disk", "next"
	known := fileBase{id: b, tracked: true}
	legacy := fileBase{id: b, tracked: true, uncertain: true}
	unknown := fileBase{tracked: true}
	for _, c := range []struct {
		name  string
		base  fileBase
		disk  string
		next  string
		force bool
		want  copyDecision
	}{
		{"first install, no file", fileBase{}, "", n, false, copyWrite},
		{"first install, identical file adopted", fileBase{}, n, n, false, copyAdopt},
		{"first install, different file refused", fileBase{}, d, n, false, copyRefuse},
		{"first install, different file forced", fileBase{}, d, n, true, copyWrite},
		{"disk already the new version", known, n, n, false, copyAdopt},
		{"unchanged since install", known, b, n, false, copyWrite},
		{"user changed, upstream didn't", known, d, b, false, copyKeep},
		{"both changed", known, d, n, false, copyPending},
		{"both changed, force doesn't apply", known, d, n, true, copyPending},
		{"user deleted, upstream didn't change", known, "", b, false, copyKeep},
		{"user deleted, upstream changed", known, "", n, false, copyPending},
		{"legacy base, unchanged", legacy, b, n, false, copyWrite},
		{"legacy base, differs, upstream same as base", legacy, d, b, false, copyPending},
		{"tracked but base unknown", unknown, d, n, false, copyPending},
		{"tracked but base unknown, force doesn't refuse", unknown, d, n, true, copyPending},
	} {
		if got := decideCopy(c.base, c.disk, c.next, c.force); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestContentIDIgnoresCRLF(t *testing.T) {
	if ContentID([]byte("a\r\nb\r\n")) != ContentID([]byte("a\nb\n")) {
		t.Error("CRLF and LF versions of a file must have one content id")
	}
	if ContentID([]byte("a\nb\n")) != BlobID([]byte("a\nb\n")) {
		t.Error("an LF file's content id must be its git blob id")
	}
}

func TestBlobIDMatchesGit(t *testing.T) {
	// `printf 'hello\n' | git hash-object --stdin`
	if got := BlobID([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("BlobID = %s", got)
	}
}
