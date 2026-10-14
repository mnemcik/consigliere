package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const indexBase = "# Notes Index\n\n## Workflow\n\n- [Hand row](hand.md) — mine\n"

// indexWorkspace returns a workspace holding notes/INDEX.md with content.
func indexWorkspace(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "notes", "INDEX.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readIndex(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "notes", "INDEX.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func notesManifest(notes ...CopyContribution) *Manifest {
	return &Manifest{Manifest: 1, Name: "demo", Version: "1.0.0", Description: "d",
		Contributes: Contributions{Notes: notes}}
}

// writeSrc overwrites a source file in the clone.
func writeSrc(t *testing.T, clone, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(clone, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexRowsApplyAndReverse(t *testing.T) {
	root := indexWorkspace(t, indexBase)
	m := notesManifest(
		CopyContribution{Src: "notes/a.md", Dest: "notes/demo-a.md", Index: "when to read A"},
		CopyContribution{Src: "notes/b.md", Dest: "notes/demo-b.md"},
	)
	m.Contributes.Templates = []CopyContribution{{Src: "tpl/t.md", Dest: "templates/demo.md"}}
	clone := makeClone(t, m)
	writeSrc(t, clone, "notes/a.md", "---\ntitle: \"Guide [A]\"\n---\n\n# Ignored\n")
	writeSrc(t, clone, "notes/b.md", "# Guide B\n\nbody\n")

	l, err := ApplyWith(root, clone, m, ApplyOptions{NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}
	want := indexBase + "\n<!-- ext:demo:index:start -->\n## From the demo extension\n\n" +
		"- [Guide \\[A\\]](demo-a.md) — when to read A\n" +
		"- [Guide B](demo-b.md)\n" +
		"<!-- ext:demo:index:end -->\n"
	if got := readIndex(t, root); got != want {
		t.Errorf("index after apply:\n%s\nwant:\n%s", got, want)
	}
	if len(l.IndexRows) != 1 || l.IndexRows[0] != (LedgerIndexRow{File: "notes/INDEX.md", Marker: "ext:demo:index"}) {
		t.Errorf("ledger index rows = %+v", l.IndexRows)
	}

	// Re-applying leaves one block.
	if _, err := ApplyWith(root, clone, m, ApplyOptions{Old: l, NotesIndex: DefaultNotesIndex}); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, root); got != want {
		t.Errorf("index after re-apply:\n%s", got)
	}

	if _, err := Reverse(root, l); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, root); got != indexBase {
		t.Errorf("index after reverse:\n%q\nwant:\n%q", got, indexBase)
	}
}

func TestIndexRowsNeedAnIndex(t *testing.T) {
	m := notesManifest(CopyContribution{Src: "notes/a.md", Dest: "notes/demo-a.md"})
	clone := makeClone(t, m)

	root := t.TempDir()
	l, err := ApplyWith(root, clone, m, ApplyOptions{NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.IndexRows) != 0 {
		t.Errorf("rows recorded with no index: %+v", l.IndexRows)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "INDEX.md")); !os.IsNotExist(err) {
		t.Errorf("index created: %v", err)
	}

	// No configured index writes nothing either.
	root = indexWorkspace(t, indexBase)
	if _, err := ApplyWith(root, clone, m, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, root); got != indexBase {
		t.Errorf("index changed with no NotesIndex: %q", got)
	}
}

func TestIndexRowsSkipANoteAlreadyLinked(t *testing.T) {
	m := notesManifest(
		CopyContribution{Src: "notes/a.md", Dest: "notes/demo-a.md"},
		CopyContribution{Src: "notes/b.md", Dest: "notes/demo-b.md"},
	)
	clone := makeClone(t, m)
	root := indexWorkspace(t, indexBase)
	l, err := ApplyWith(root, clone, m, ApplyOptions{NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}

	// The user lists one note by hand, in either link form: it loses its row.
	hand := indexBase + "- [A, by hand](./demo-a.md#top \"t\")\n"
	withBlock := strings.Replace(readIndex(t, root), indexBase, hand, 1)
	if err := os.WriteFile(filepath.Join(root, "notes", "INDEX.md"), []byte(withBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err = ApplyWith(root, clone, m, ApplyOptions{Old: l, NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}
	got := readIndex(t, root)
	if strings.Contains(got, "(demo-a.md)") || !strings.Contains(got, "- [demo-b](demo-b.md)") {
		t.Errorf("index after hand row:\n%s", got)
	}

	// Both listed by hand: the block goes, and nothing is recorded.
	hand += "- [B, by hand](<demo-b.md>)\n"
	if err := os.WriteFile(filepath.Join(root, "notes", "INDEX.md"), []byte(strings.Replace(got, indexBase+"- [A, by hand](./demo-a.md#top \"t\")\n", hand, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	next, err := ApplyWith(root, clone, m, ApplyOptions{Old: l, NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, root); got != hand {
		t.Errorf("index with both by hand:\n%q\nwant:\n%q", got, hand)
	}
	if len(next.IndexRows) != 0 {
		t.Errorf("rows recorded: %+v", next.IndexRows)
	}
	if o := OrphanLedger(l, next); o == nil || len(o.IndexRows) != 1 {
		t.Errorf("orphan of the dropped block = %+v", o)
	}
}

func TestIndexRowsKeepCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(indexBase, "\n", "\r\n")
	root := indexWorkspace(t, crlf)
	m := notesManifest(CopyContribution{Src: "notes/a.md", Dest: "notes/demo-a.md"})
	clone := makeClone(t, m)
	l, err := ApplyWith(root, clone, m, ApplyOptions{NotesIndex: DefaultNotesIndex})
	if err != nil {
		t.Fatal(err)
	}
	got := readIndex(t, root)
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") || !strings.Contains(got, "ext:demo:index:end") {
		t.Errorf("index not all CRLF:\n%q", got)
	}
	if _, err := Reverse(root, l); err != nil {
		t.Fatal(err)
	}
	if got := readIndex(t, root); got != crlf {
		t.Errorf("index after reverse:\n%q", got)
	}
}

func TestIndexRowsRollBackOnError(t *testing.T) {
	root := indexWorkspace(t, indexBase)
	m := notesManifest(CopyContribution{Src: "notes/a.md", Dest: "notes/demo-a.md"})
	m.Contributes.Hooks = []HookContribution{{Event: "SessionStart", Wrapper: "hooks/gate.sh", Command: "cg-demo gate"}}
	clone := makeClone(t, m)
	if err := os.Remove(filepath.Join(clone, "hooks", "gate.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyWith(root, clone, m, ApplyOptions{NotesIndex: DefaultNotesIndex}); err == nil {
		t.Fatal("expected the missing hook to fail the apply")
	}
	if got := readIndex(t, root); got != indexBase {
		t.Errorf("index not restored: %q", got)
	}
}

func TestOrphanLedgerIndexRows(t *testing.T) {
	row := LedgerIndexRow{File: "notes/INDEX.md", Marker: "ext:demo:index"}
	old := &Ledger{Name: "demo", IndexRows: []LedgerIndexRow{row}}
	if o := OrphanLedger(old, &Ledger{Name: "demo", IndexRows: []LedgerIndexRow{row}}); o != nil {
		t.Errorf("kept block reported orphaned: %+v", o)
	}
	moved := LedgerIndexRow{File: "docs/INDEX.md", Marker: row.Marker}
	o := OrphanLedger(old, &Ledger{Name: "demo", IndexRows: []LedgerIndexRow{moved}})
	if o == nil || len(o.IndexRows) != 1 || o.IndexRows[0] != row {
		t.Errorf("block in the old index not orphaned: %+v", o)
	}
}

func TestNoteTitle(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"---\ntitle: \"Front\"\ntags: [a]\n---\n# Heading\n", "Front"},
		{"---\r\ntitle: CRLF\r\n---\r\n", "CRLF"},
		{"---\ntags: [a]\n---\n\n# From  heading \n", "From heading"},
		{"no heading\n## Second level\n", "guide"},
		{"---\ntitle: [broken\n---\n# Fallback\n", "Fallback"},
	} {
		if got := noteTitle([]byte(c.data), "notes/guide.md"); got != c.want {
			t.Errorf("noteTitle(%q) = %q, want %q", c.data, got, c.want)
		}
	}
}

func TestIndexLink(t *testing.T) {
	for _, c := range []struct{ index, dest, want string }{
		{"notes/INDEX.md", "notes/a.md", "a.md"},
		{"notes/INDEX.md", "notes/sub/a.md", "sub/a.md"},
		{"notes/INDEX.md", "docs/a.md", "../docs/a.md"},
		{"INDEX.md", "notes/a.md", "notes/a.md"},
		{"kb/notes/INDEX.md", "kb/a.md", "../a.md"},
	} {
		if got := indexLink(c.index, c.dest); got != c.want {
			t.Errorf("indexLink(%q, %q) = %q, want %q", c.index, c.dest, got, c.want)
		}
	}
	if got := indexRow("my note.md", "T", ""); got != "- [T](<my note.md>)" {
		t.Errorf("row with a space = %q", got)
	}
}
