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
