package extension

import "testing"

func TestDecideCopy(t *testing.T) {
	const b, d, n = "base", "disk", "next"
	for _, c := range []struct {
		name             string
		base, disk, next string
		force            bool
		want             copyDecision
	}{
		{"missing file is written", b, "", n, false, copyWrite},
		{"disk already the new version", b, n, n, false, copyAdopt},
		{"first install, different file, refused", "", d, n, false, copyRefuse},
		{"first install, different file, forced", "", d, n, true, copyWrite},
		{"unchanged since install", b, b, n, false, copyWrite},
		{"user changed, upstream didn't", b, d, b, false, copyKeep},
		{"both changed", b, d, n, false, copyPending},
		{"both changed, force doesn't apply", b, d, n, true, copyPending},
	} {
		if got := decideCopy(c.base, c.disk, c.next, c.force); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBlobIDMatchesGit(t *testing.T) {
	// `printf 'hello\n' | git hash-object --stdin`
	if got := BlobID([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("BlobID = %s", got)
	}
}
