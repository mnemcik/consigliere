package migrate

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mnemcik/consigliere/internal/manifest"
)

// fm parses the frontmatter of a converted document.
func fm(t *testing.T, doc string) map[string]any {
	t.Helper()
	block, _ := manifest.SplitFrontmatter(doc)
	if block == "" {
		t.Fatalf("no frontmatter:\n%s", doc)
	}
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	var m map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:len(lines)-1], "\n")), &m); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, doc)
	}
	return m
}

func body(doc string) string {
	_, b := manifest.SplitFrontmatter(doc)
	return b
}

func convert(t *testing.T, in string) (string, Result) {
	t.Helper()
	out, res, err := Convert(in)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return out, res
}

func day(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func TestConvertProject(t *testing.T) {
	in := "# Invoice Export\n\n## Meta\n\n" +
		"- **Status:** In-Progress (blocked on the API; see #12)\n" +
		"- **Priority:** high\n" +
		"- **Areas:** [`billing`](../../areas/billing.md), [`Payments`](../../areas/payments.md)\n" +
		"- **Started:** 2026-03-01\n" +
		"- **Origin:** N/A\n" +
		"- **Output type:** tool\n\n" +
		"<!-- Statuses: defining → in-progress → done | on-hold -->\n\n" +
		"## Problem\n\nText.\n"
	out, res := convert(t, in)
	if !res.Changed {
		t.Fatal("not changed")
	}
	want := map[string]any{
		"title":       "Invoice Export",
		"status":      "in-progress",
		"status_note": "blocked on the API; see #12",
		"priority":    "high",
		"areas":       []any{"billing", "payments"},
		"created":     day("2026-03-01"),
		"output_type": "tool",
	}
	if got := fm(t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("frontmatter\n got %#v\nwant %#v", got, want)
	}
	if b := body(out); b != "\n# Invoice Export\n\n## Problem\n\nText.\n" {
		t.Errorf("body = %q", b)
	}
}

func TestConvertNoMetaIsUnchanged(t *testing.T) {
	in := "---\ntitle: x\n---\n\n# X\n\n## Problem\n"
	out, res := convert(t, in)
	if res.Changed || out != in {
		t.Errorf("changed a file with no ## Meta:\n%s", out)
	}
}

func TestConvertIsIdempotent(t *testing.T) {
	in := "# T\n\n## Meta\n\n- **Status:** raw\n- **Tags:** `a`, `b`\n\n## What\n"
	once, _ := convert(t, in)
	twice, res := convert(t, once)
	if res.Changed || twice != once {
		t.Errorf("second run changed the file:\n%s\n---\n%s", once, twice)
	}
}

// DEC-005: frontmatter wins per field; the differing Meta value is reported.
func TestConvertFrontmatterWins(t *testing.T) {
	in := "---\ntitle: \"Kept\"\nstatus: done\ncustom: keep-me\n---\n\n# T\n\n## Meta\n\n- **Status:** in-progress\n- **Priority:** low\n"
	out, res := convert(t, in)
	got := fm(t, out)
	if got["status"] != "done" || got["title"] != "Kept" || got["custom"] != "keep-me" || got["priority"] != "low" {
		t.Errorf("frontmatter = %#v", got)
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0], "status") {
		t.Errorf("conflicts = %q", res.Conflicts)
	}
	// Existing keys keep their order; new ones slot in by keyOrder.
	if i, j := strings.Index(out, "custom:"), strings.Index(out, "priority:"); i < 0 || j < 0 || j < strings.Index(out, "status:") {
		t.Errorf("key order:\n%s", out)
	}
}

// DEC-014: the earliest creation date wins; the rest of a date value is kept.
func TestConvertCreatedEarliestAndNote(t *testing.T) {
	in := "# T\n\n## Meta\n\n- **Created:** 2026-06-17\n- **Started:** 2026-06-12 (kick-off call)\n"
	out, res := convert(t, in)
	got := fm(t, out)
	if got["created"] != day("2026-06-12") {
		t.Errorf("created = %v", got["created"])
	}
	if got["created_note"] != "2026-06-12 (kick-off call)" {
		t.Errorf("created_note = %#v", got["created_note"])
	}
	if len(res.Notes) != 1 {
		t.Errorf("notes = %q, want the date collapse reported", res.Notes)
	}
}

func TestConvertAreaReviewHistory(t *testing.T) {
	in := "# Billing\n\n## Meta\n\n- **Slug:** `billing`\n- **Tags:** service, Payments\n" +
		"- **Last reviewed:** 2026-09-03 (checked the contacts)\n" +
		"- **Previously reviewed:** 2026-08-01 (initial pass)\n\n" +
		"## Overview\n\nX.\n\n## Related Areas\n\nY.\n"
	out, _ := convert(t, in)
	got := fm(t, out)
	if got["last_reviewed"] != day("2026-09-03") || got["slug"] != "billing" {
		t.Errorf("frontmatter = %#v", got)
	}
	if !reflect.DeepEqual(got["tags"], []any{"service", "payments"}) {
		t.Errorf("tags = %#v", got["tags"])
	}
	want := "## Review History\n\n- 2026-09-03 (checked the contacts)\n- 2026-08-01 (initial pass)\n\n## Related Areas"
	if !strings.Contains(out, want) {
		t.Errorf("review history not before Related Areas:\n%s", out)
	}
	if _, ok := got["previously_reviewed"]; ok {
		t.Error("previously reviewed leaked into frontmatter")
	}
}

func TestConvertBareDateLastReviewedAddsNoHistory(t *testing.T) {
	out, _ := convert(t, "# A\n\n## Meta\n\n- **Last reviewed:** 2026-09-03\n\n## Overview\n")
	if strings.Contains(out, "Review History") {
		t.Errorf("a bare date needs no history entry:\n%s", out)
	}
}

// Nothing in the Meta section is lost: prose and unknown comments stay in the
// body, shipped vocabulary comments go, continuation lines join.
func TestConvertKeepsNonFieldContent(t *testing.T) {
	in := "# N\n\n## Meta\n\n- **Category:** tool-gotchas\n- **Source session:** a long\n  wrapped value\n\n" +
		"<!-- Categories: tool-gotchas | workflow | architecture | process | research | reference | troubleshooting -->\n" +
		"<!-- workspace-own note -->\n\nA summary paragraph that sat under the bullets.\n\n## Details\n"
	out, _ := convert(t, in)
	got := fm(t, out)
	if got["source_session"] != "a long wrapped value" {
		t.Errorf("source_session = %#v", got["source_session"])
	}
	b := body(out)
	for _, want := range []string{"<!-- workspace-own note -->", "A summary paragraph that sat under the bullets."} {
		if !strings.Contains(b, want) {
			t.Errorf("body lost %q:\n%s", want, b)
		}
	}
	if strings.Contains(b, "Categories:") || strings.Contains(b, "## Meta") {
		t.Errorf("body kept Meta scaffolding:\n%s", b)
	}
}

func TestConvertRepeatedLabelsAreKept(t *testing.T) {
	in := "# P\n\n## Meta\n\n- **Follow-up PR:** #1\n- **Follow-up PR:** #2\n- **Status:** done\n- **Status:** stale\n"
	got := fm(t, mustOut(t, in))
	if !reflect.DeepEqual(got["follow_up_pr"], []any{"#1", "#2"}) {
		t.Errorf("follow_up_pr = %#v", got["follow_up_pr"])
	}
	if got["status"] != "done" || !reflect.DeepEqual(got["status_extra"], []any{"stale"}) {
		t.Errorf("status = %#v, status_extra = %#v", got["status"], got["status_extra"])
	}
}

func TestConvertValueShapes(t *testing.T) {
	in := "# P\n\n## Meta\n\n" +
		"- **Status:** **done (2026-06-13)** — released in v1.3.0\n" +
		"- **Authority:** mirror — https://example.com/pricing\n" +
		"- **Verified:** 2026-09-01\n" +
		"- **Areas:** `vips-core` (primary), `devops`\n" +
		"- **Repository:** `~/source/x` <!-- local clone -->\n" +
		"- **Origin:** N/A (spun off from another project)\n" +
		"- **Color:** {color}\n" +
		"- **Version:** 1.10\n"
	got := fm(t, mustOut(t, in))
	checks := map[string]any{
		"status":         "done",
		"status_note":    "(2026-06-13) — released in v1.3.0",
		"authority":      "mirror",
		"authority_note": "https://example.com/pricing",
		"verified":       day("2026-09-01"),
		"areas":          []any{"vips-core", "devops"},
		"areas_note":     "`vips-core` (primary), `devops`",
		"repository":     "`~/source/x`",
		"origin":         "N/A (spun off from another project)",
		"version":        "1.10", // a string, not the float 1.1
	}
	for k, want := range checks {
		if !reflect.DeepEqual(got[k], want) {
			t.Errorf("%s = %#v, want %#v", k, got[k], want)
		}
	}
	if _, ok := got["color"]; ok {
		t.Error("an unfilled {placeholder} was migrated")
	}
	if !strings.Contains(mustOut(t, in), "repository: '`~/source/x`' # local clone") {
		t.Errorf("inline comment not kept as a YAML comment:\n%s", mustOut(t, in))
	}
}

func TestConvertCRLF(t *testing.T) {
	out := mustOut(t, "# T\r\n\r\n## Meta\r\n\r\n- **Status:** raw\r\n\r\n## What\r\n")
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Errorf("mixed line endings:\n%q", out)
	}
	if fm(t, strings.ReplaceAll(out, "\r\n", "\n"))["status"] != "raw" {
		t.Errorf("status lost:\n%q", out)
	}
}

func TestConvertRejectsBrokenFrontmatter(t *testing.T) {
	if _, _, err := Convert("---\ntitle: [unclosed\n---\n\n# T\n\n## Meta\n\n- **Status:** raw\n"); err == nil {
		t.Error("want an error for invalid existing frontmatter, not a rewrite")
	}
}

func mustOut(t *testing.T, in string) string {
	t.Helper()
	out, _ := convert(t, in)
	return out
}
