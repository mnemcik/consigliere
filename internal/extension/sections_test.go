package extension

import (
	"strings"
	"testing"
)

func TestUpsertSectionInsertAndReplace(t *testing.T) {
	base := "# CLAUDE.md\n\nuser content\n"
	out := UpsertSection(base, "demo", "rules", "first body")
	if !strings.Contains(out, "<!-- ext:demo:section:start=rules -->") ||
		!strings.Contains(out, "<!-- ext:demo:section:end=rules -->") ||
		!strings.Contains(out, "first body") {
		t.Fatalf("insert did not produce the block:\n%s", out)
	}
	if !strings.HasPrefix(out, base) {
		t.Errorf("existing content should be preserved as a prefix:\n%s", out)
	}

	// Replace keeps a single block with the new body.
	out2 := UpsertSection(out, "demo", "rules", "second body")
	if strings.Contains(out2, "first body") {
		t.Errorf("replace should drop the old body:\n%s", out2)
	}
	if !strings.Contains(out2, "second body") {
		t.Errorf("replace should contain the new body:\n%s", out2)
	}
	if strings.Count(out2, "ext:demo:section:start=rules") != 1 {
		t.Errorf("replace must not duplicate the block:\n%s", out2)
	}
}

func TestRemoveSectionRoundtrip(t *testing.T) {
	base := "# CLAUDE.md\n\nuser content\n"
	withBlock := UpsertSection(base, "demo", "rules", "body")
	out, ok := RemoveSection(withBlock, "demo", "rules")
	if !ok {
		t.Fatal("RemoveSection should report found")
	}
	if strings.Contains(out, "ext:demo:section") {
		t.Errorf("block markers should be gone:\n%q", out)
	}
	if out != base {
		t.Errorf("remove should restore the original content.\n got: %q\nwant: %q", out, base)
	}

	if _, ok := RemoveSection(base, "demo", "absent"); ok {
		t.Error("removing an absent block should report not-found")
	}
}

func TestUpsertSectionNamespacing(t *testing.T) {
	out := UpsertSection("", "a", "x", "A")
	out = UpsertSection(out, "b", "x", "B")
	// Same id "x" under different extension names must coexist.
	if strings.Count(out, "section:start=x") != 2 {
		t.Errorf("expected two distinct blocks for the same id across names:\n%s", out)
	}
	out, _ = RemoveSection(out, "a", "x")
	if strings.Contains(out, "ext:a:section") || !strings.Contains(out, "ext:b:section:start=x") {
		t.Errorf("removing a's block must not touch b's:\n%s", out)
	}
}

func TestGitignoreBlockRoundtripAndNamespace(t *testing.T) {
	base := ".DS_Store\n"
	out := UpsertGitignore(base, "demo", "cache", "/cache/")
	if !strings.Contains(out, "# ext:demo:gitignore:start=cache\n/cache/\n# ext:demo:gitignore:end=cache\n") {
		t.Errorf("gitignore block not written with # markers:\n%q", out)
	}
	// A CLAUDE.md section with the same name and id is a different block.
	if _, ok := RemoveSection(out, "demo", "cache"); ok {
		t.Error("RemoveSection must not match a gitignore block")
	}
	back, ok := RemoveGitignore(out, "demo", "cache")
	if !ok || back != base {
		t.Errorf("RemoveGitignore = %q, %v; want %q, true", back, ok, base)
	}
}

func TestGitignoreIDPrefixDoesNotMatchLongerID(t *testing.T) {
	out := UpsertGitignore("", "x", "cache-v2", "/v2/")
	out = UpsertGitignore(out, "x", "cache", "/v1/")
	if strings.Count(out, "gitignore:start=") != 2 || !strings.Contains(out, "start=cache-v2\n/v2/\n") ||
		!strings.Contains(out, "start=cache\n/v1/\n") {
		t.Fatalf("installing id cache after cache-v2 must add its own block:\n%s", out)
	}
	out, ok := RemoveGitignore(out, "x", "cache")
	if !ok || out != "\n# ext:x:gitignore:start=cache-v2\n/v2/\n# ext:x:gitignore:end=cache-v2\n" {
		t.Errorf("removing cache must leave cache-v2 intact: ok=%v\n%q", ok, out)
	}
	if _, ok := RemoveGitignore(out, "x", "cache"); ok {
		t.Error("RemoveGitignore must not match the longer id cache-v2")
	}
}

func TestRemoveBlockKeepsUserBlankLines(t *testing.T) {
	for _, base := range []string{"a\n\n", "a\n\n\n", "a\n", ""} {
		out, _ := RemoveGitignore(UpsertGitignore(base, "x", "c", "/c/"), "x", "c")
		if out != base {
			t.Errorf("round trip of %q gave %q", base, out)
		}
		out, _ = RemoveSection(UpsertSection(base, "x", "c", "body"), "x", "c")
		if out != base {
			t.Errorf("section round trip of %q gave %q", base, out)
		}
	}
}

func TestIndexLineEdgeCases(t *testing.T) {
	const m = "# ext:x:gitignore:start=c"
	cases := []struct {
		content string
		want    int
	}{
		{m + "\nrest\n", 0},     // at file start
		{"a\n" + m, 2},          // at EOF with no newline
		{"a\n" + m + "\r\n", 2}, // followed by CRLF
		{"a " + m + "\n", -1},   // inside a line
		{m + "-v2\n", -1},       // prefix of a longer id
		{"# ext:x\n" + m + "-v2\n" + m + "\n", 8 + len(m) + 4}, // near-matches before the real one
	}
	for _, c := range cases {
		if got := indexLine(c.content, m); got != c.want {
			t.Errorf("indexLine(%q) = %d, want %d", c.content, got, c.want)
		}
	}
}

func TestRemoveBlockAbsorbsACRLFAfterTheEnd(t *testing.T) {
	got, ok := removeBlock("a\n\nSTART\r\nrow\r\nEND\r\nb\r\n", "START", "END", indexLine)
	if !ok || got != "a\nb\r\n" {
		t.Errorf("removeBlock = %q, %v", got, ok)
	}
}
