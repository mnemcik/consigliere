package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const sampleCLAUDE = `# CLAUDE.md

<!-- user:section:start=purpose -->
## Purpose
User-owned content the framework must never touch.
<!-- user:section:end=purpose -->

<!-- cg:section:start=memory-policy -->
## Memory Policy
Do not use auto-memory.
<!-- cg:section:end=memory-policy -->

<!-- cg:section:start=session-start -->
## Session Start
Identify project and area.
<!-- cg:section:end=session-start -->
`

func TestParseSectionsExtractsFrameworkSectionsOnly(t *testing.T) {
	got := ParseSections(sampleCLAUDE)

	if len(got) != 2 {
		t.Fatalf("expected 2 cg:section blocks, got %d: %v", len(got), got)
	}
	if _, ok := got["purpose"]; ok {
		t.Error("user:section 'purpose' must not be parsed as a framework section")
	}
	if want := "## Memory Policy\nDo not use auto-memory."; got["memory-policy"] != want {
		t.Errorf("memory-policy inner = %q, want %q", got["memory-policy"], want)
	}
	if want := "## Session Start\nIdentify project and area."; got["session-start"] != want {
		t.Errorf("session-start inner = %q, want %q", got["session-start"], want)
	}
}

func TestParseSectionsSkipsUnmatchedStart(t *testing.T) {
	content := "<!-- cg:section:start=orphan -->\nno end marker here\n"
	if got := ParseSections(content); len(got) != 0 {
		t.Errorf("expected no sections for unmatched start, got %v", got)
	}
}

func TestReplaceSectionRewritesInnerAndIsParseable(t *testing.T) {
	content := "# CLAUDE.md\n" +
		"<!-- cg:section:start=a -->\nold a\n<!-- cg:section:end=a -->\n" +
		"<!-- cg:section:start=b -->\nold b\n<!-- cg:section:end=b -->\n"

	got, ok := ReplaceSection(content, "a", "new a body")
	if !ok {
		t.Fatal("expected section a to be found")
	}
	parsed := ParseSections(got)
	if parsed["a"] != "new a body" {
		t.Errorf("section a inner = %q, want %q", parsed["a"], "new a body")
	}
	if parsed["b"] != "old b" {
		t.Errorf("section b must be untouched, got %q", parsed["b"])
	}
	// Sentinels preserved exactly once each.
	if strings.Count(got, "cg:section:start=a") != 1 || strings.Count(got, "cg:section:end=a") != 1 {
		t.Errorf("section a sentinels not preserved exactly once: %q", got)
	}
}

func TestReplaceSectionReturnsFalseForMissingOrUnterminated(t *testing.T) {
	content := "<!-- cg:section:start=a -->\nbody\n<!-- cg:section:end=a -->\n"
	if _, ok := ReplaceSection(content, "missing", "x"); ok {
		t.Error("expected ok=false for a section that does not exist")
	}
	unterminated := "<!-- cg:section:start=a -->\nbody but no end marker\n"
	if got, ok := ReplaceSection(unterminated, "a", "x"); ok || got != unterminated {
		t.Error("expected ok=false and unchanged content for an unterminated section")
	}
}

func TestAppendSectionAddsParseableBlock(t *testing.T) {
	content := "# CLAUDE.md\n<!-- cg:section:start=a -->\nbody a\n<!-- cg:section:end=a -->\n"
	got := AppendSection(content, "fresh", "fresh body")
	parsed := ParseSections(got)
	if parsed["fresh"] != "fresh body" {
		t.Errorf("appended section not parseable: got %q", parsed["fresh"])
	}
	if parsed["a"] != "body a" {
		t.Errorf("existing section disturbed: got %q", parsed["a"])
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("appended content should end with a newline")
	}
}

func TestAppendSectionAddsTrailingNewlineWhenMissing(t *testing.T) {
	got := AppendSection("no trailing newline", "x", "y")
	if ParseSections(got)["x"] != "y" {
		t.Errorf("appended section not parseable when source lacked a trailing newline: %q", got)
	}
}

func TestHashContentIsDeterministicAndDistinct(t *testing.T) {
	first := HashContent("abc")
	second := HashContent("abc")
	if first != second {
		t.Error("hash of identical content differs")
	}
	if first == HashContent("abd") {
		t.Error("hash of different content collides")
	}
}

func TestNotesFromFSHashesFilesAndSkipsDotfiles(t *testing.T) {
	fsys := fstest.MapFS{
		"guide.md":          {Data: []byte("guide body")},
		"sub/deep.md":       {Data: []byte("deep body")},
		".gitkeep":          {Data: []byte("placeholder")},
		"sub/.editorcache":  {Data: []byte("junk")},
		".hidden/buried.md": {Data: []byte("must not be registered")},
		"sub/.cache/x.md":   {Data: []byte("also hidden")},
	}

	got, err := NotesFromFS(fsys, "notes")
	if err != nil {
		t.Fatalf("NotesFromFS: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 notes (dotfiles + hidden dirs skipped), got %d: %v", len(got), got)
	}
	if _, ok := got["notes/.gitkeep"]; ok {
		t.Error("dotfile .gitkeep must not be registered as a managed note")
	}
	// Files nested under a hidden directory must be pruned, not registered.
	if _, ok := got["notes/.hidden/buried.md"]; ok {
		t.Error("file under a dot-directory must not be registered")
	}
	if _, ok := got["notes/sub/.cache/x.md"]; ok {
		t.Error("file under a nested dot-directory must not be registered")
	}
	if want := HashContent("guide body"); got["notes/guide.md"].Hash != want {
		t.Errorf("notes/guide.md hash = %q, want %q", got["notes/guide.md"].Hash, want)
	}
	if want := HashContent("deep body"); got["notes/sub/deep.md"].Hash != want {
		t.Errorf("notes/sub/deep.md hash = %q, want %q", got["notes/sub/deep.md"].Hash, want)
	}
}

func TestNotesFromFSEmptyTreeYieldsEmptyMap(t *testing.T) {
	got, err := NotesFromFS(fstest.MapFS{".gitkeep": {Data: []byte("x")}}, "notes")
	if err != nil {
		t.Fatalf("NotesFromFS: %v", err)
	}
	if got == nil {
		t.Fatal("expected a non-nil map")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for a notes tree with only dotfiles, got %v", got)
	}
}

func TestFromCLAUDEBuildsManifest(t *testing.T) {
	m := FromCLAUDE(sampleCLAUDE, "1.2.3")

	if m.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", m.SchemaVersion, SchemaVersion)
	}
	if m.FrameworkVersion != "1.2.3" {
		t.Errorf("frameworkVersion = %q, want %q", m.FrameworkVersion, "1.2.3")
	}
	if len(m.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(m.Sections))
	}
	if m.Notes == nil {
		t.Error("Notes should be initialized (non-nil), even when empty")
	}
	want := HashContent("## Memory Policy\nDo not use auto-memory.")
	if m.Sections["memory-policy"].Hash != want {
		t.Errorf("memory-policy hash = %q, want %q", m.Sections["memory-policy"].Hash, want)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := FromCLAUDE(sampleCLAUDE, "2.0.0")

	if err := orig.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, File)); err != nil {
		t.Fatalf("manifest file not written: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected manifest, got nil")
	}
	if loaded.FrameworkVersion != orig.FrameworkVersion {
		t.Errorf("frameworkVersion round-trip mismatch: %q vs %q", loaded.FrameworkVersion, orig.FrameworkVersion)
	}
	if len(loaded.Sections) != len(orig.Sections) {
		t.Errorf("section count round-trip mismatch: %d vs %d", len(loaded.Sections), len(orig.Sections))
	}
	if loaded.Sections["session-start"].Hash != orig.Sections["session-start"].Hash {
		t.Error("session-start hash did not round-trip")
	}
}

func TestLoadReturnsNilWhenAbsent(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest for empty dir, got %+v", m)
	}
}

func TestSplitFrontmatterAndHashBody(t *testing.T) {
	const body = "# Note\n\n## Meta\n\n- **Tags:** `a`, `b`\n\n## Summary\n\ntext\n"

	bare := body
	withFM := "---\ntitle: \"Note\"\ntags: [a, b]\n---\n\n" + body

	// The whole point: a workspace adding frontmatter to a framework note must
	// not register as drift, so both forms must hash identically.
	if HashBody(bare) != HashBody(withFM) {
		t.Error("HashBody differs with and without frontmatter; a locally-added property would read as drift")
	}
	// ...while a real body edit still changes the hash.
	if HashBody(bare) == HashBody(bare+"\nedited\n") {
		t.Error("HashBody ignored a body change")
	}
	// And it must not collapse to hashing nothing.
	if HashBody(bare) == HashContent("") {
		t.Error("HashBody hashed an empty body")
	}

	// Backward compatibility: manifests written before body-hashing existed
	// hold HashContent of the whole file. For a note with no frontmatter the
	// two must agree exactly, or upgrading would mark every note as drifted.
	if HashBody(bare) != HashContent(bare) {
		t.Error("HashBody != HashContent for a note with no frontmatter; legacy manifests would all read as drift")
	}

	// SplitFrontmatter is deliberately lossless -- the body keeps the blank
	// line that followed the block -- so losslessness, not equality with the
	// bare body, is the invariant to assert here.
	fm, rest := SplitFrontmatter(withFM)
	if fm == "" {
		t.Error("SplitFrontmatter found no frontmatter")
	}
	if fm+rest != withFM {
		t.Error("SplitFrontmatter is not lossless")
	}
	if strings.TrimSpace(rest) != strings.TrimSpace(body) {
		t.Errorf("body content changed: got %q", rest)
	}

	// A horizontal rule mid-document must not be mistaken for frontmatter.
	hr := "# Note\n\n---\n\nafter the rule\n"
	if fm, rest := SplitFrontmatter(hr); fm != "" || rest != hr {
		t.Errorf("mid-document horizontal rule treated as frontmatter: fm=%q", fm)
	}
	// An unterminated block is not frontmatter either.
	if fm, _ := SplitFrontmatter("---\ntitle: x\nno terminator\n"); fm != "" {
		t.Error("unterminated frontmatter should not split")
	}
}

// Regression: the closing delimiter may sit at EOF with no trailing newline.
// An earlier index-arithmetic implementation panicked here
// ("slice bounds out of range [:17] with length 16"), which would have crashed
// `cg sync` on such a note.
func TestSplitFrontmatterEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantFM  string
		wantHas bool
	}{
		{"closing delimiter at EOF", "---\ntitle: x\n---", "---\ntitle: x\n---", true},
		{"closing delimiter at EOF with newline", "---\ntitle: x\n---\n", "---\ntitle: x\n---\n", true},
		{"CRLF throughout", "---\r\ntitle: x\r\n---\r\n\r\n# N\r\n", "---\r\ntitle: x\r\n---\r\n", true},
		{"CRLF closing at EOF", "---\r\ntitle: x\r\n---", "---\r\ntitle: x\r\n---", true},
		{"unterminated block", "---\ntitle: x\nno end\n", "", false},
		{"opening delimiter alone", "---", "", false},
		{"opening delimiter then EOF newline", "---\n", "", false},
		{"horizontal rule mid-document", "# N\n\n---\n\nafter\n", "", false},
		{"no frontmatter", "# N\n\nbody\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body := SplitFrontmatter(tt.in)
			if (fm != "") != tt.wantHas {
				t.Fatalf("frontmatter detected = %v, want %v (fm=%q)", fm != "", tt.wantHas, fm)
			}
			if fm != tt.wantFM {
				t.Errorf("fm = %q, want %q", fm, tt.wantFM)
			}
			if fm+body != tt.in {
				t.Errorf("not lossless: %q + %q != %q", fm, body, tt.in)
			}
		})
	}
}

// A CRLF note must hash the same with and without frontmatter, just as an LF
// one does -- otherwise a Windows checkout reads every note as edited.
func TestHashBodyCRLF(t *testing.T) {
	body := "# Note\r\n\r\n## Meta\r\n\r\n- **Tags:** `a`\r\n"
	withFM := "---\r\ntitle: \"Note\"\r\n---\r\n\r\n" + body
	if HashBody(body) != HashBody(withFM) {
		t.Error("CRLF note hashes differently with and without frontmatter")
	}
	if HashBody(body) != HashContent(body) {
		t.Error("HashBody != HashContent for a CRLF note with no frontmatter")
	}
}
