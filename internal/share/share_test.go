package share

import (
	"reflect"
	"strings"
	"testing"
)

// pilotReadme is modelled on a real project: derived frontmatter carrying
// priority, a Meta block with a local repository path and a workspace Origin
// link, the [You] placeholder, and every link shape the export must handle.
const pilotReadme = `---
title: "Pilot"
status: "in-progress"
priority: "high"
---

# Pilot

## Meta

- **Status:** in-progress
- **Priority:** high
- **Areas:** [` + "`product-config`" + `](../../areas/product-config.md)
- **Started:** 2025-11-09
- **Origin:** [ideas/pilot.md](../../ideas/pilot.md)
- **Output type:** documentation
- **Repository:** ` + "`~/source/pilot-repo`" + `

<!-- Statuses: defining → in-progress → done | on-hold -->

## Problem

See the [area](../../areas/product-config.md), the [note](../../notes/naming.md#rules),
the [sibling](../sibling/) and [its decisions](../sibling/decisions.md#dec-005).
Our own [decisions](decisions.md#dec-001), the [analysis](analysis.md),
the [spec](https://example.com/spec), [top](#problem) and ![diagram](diagram.png).
Surfaced by [` + "`sibling-ui`" + ` DEC-005](../sibling/decisions.md) in review.
Literal: ` + "`[kept](../../notes/x.md)`" + ` stays code.

` + "```" + `
[fenced](../../notes/y.md)
` + "```" + `

<!-- share:exclude:start -->
Private aside naming a colleague.
<!-- share:exclude:end -->

## Stakeholders

| Role | Who |
|------|-----|
| Owner | [You] |
`

var pilotStamp = Stamp{Owner: "Ada Owner", SHA: "0123456789abcdef0123", Date: "2026-09-02"}

func pilotInput() *Input {
	return &Input{
		Project: Project{Slug: "pilot", Status: "In Progress", Areas: []string{"product-config"}},
		Files: map[string]string{
			"README.md":    pilotReadme,
			"decisions.md": "# Decisions\n\n### DEC-001 — First\n",
		},
		Stamp: pilotStamp,
	}
}

func renderREADME(t *testing.T, in *Input) string {
	t.Helper()
	out, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	body, ok := out["pilot/README.md"]
	if !ok {
		t.Fatalf("no pilot/README.md in output; got %v", keys(out))
	}
	return body
}

func keys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestRenderPilot(t *testing.T) {
	got := renderREADME(t, pilotInput())

	for _, want := range []string{
		// stamp
		"> **Authority:** mirror — Ada Owner's workspace @ `0123456789ab` · **Verified:** 2026-09-02",
		// Meta rebuilt from the index
		"- **Status:** In Progress",
		"- **Areas:** `product-config`",
		"- **Started:** 2025-11-09",
		"- **Output type:** documentation",
		// private links de-linked, text kept
		"See the area *(private)*, the note *(private)*,",
		"the sibling *(private)* and its decisions *(private)*.",
		"Surfaced by `sibling-ui` DEC-005 *(private)* in review.",
		// exported file rewritten; non-exported own file is private
		"Our own [decisions](decisions.md#dec-001), the analysis *(private)*,",
		// external and fragment links kept; image de-linked to its alt text
		"the [spec](https://example.com/spec), [top](#problem) and diagram *(private)*.",
		// code is literal
		"Literal: `[kept](../../notes/x.md)` stays code.",
		"[fenced](../../notes/y.md)",
		// owner placeholder
		"| Owner | Ada Owner |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s", want, got)
		}
	}

	for _, banned := range []string{
		"priority", "Priority", "~/source", "Repository", "ideas/pilot.md",
		"Private aside", "share:exclude", "[You]", "../../areas", "Statuses:",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("output leaks %q\n---\n%s", banned, got)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(strings.SplitN(got, "\n\n", 2)[1]), "---") {
		t.Errorf("frontmatter survived:\n%s", got)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, err := Render(pilotInput())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(pilotInput())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("two renders of the same input differ")
	}
}

func TestRenderRewritesLinksToSharedProjects(t *testing.T) {
	in := pilotInput()
	in.Shared = map[string]string{
		"projects/sibling/README.md":    "sibling/README.md",
		"projects/sibling/decisions.md": "sibling/decisions.md",
	}
	got := renderREADME(t, in)
	for _, want := range []string{
		"the [sibling](../sibling/README.md) and [its decisions](../sibling/decisions.md#dec-005).",
		"Surfaced by [`sibling-ui` DEC-005](../sibling/decisions.md) in review.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s", want, got)
		}
	}
}

func TestRenderKeepsExternalOrigin(t *testing.T) {
	in := pilotInput()
	in.Files["README.md"] = "# P\n\n## Meta\n\n- **Origin:** VPAAS-1068\n- **Started:** 2026-01-01\n\n## Problem\n"
	got := renderREADME(t, in)
	if !strings.Contains(got, "- **Origin:** VPAAS-1068") {
		t.Errorf("external Origin dropped:\n%s", got)
	}
}

func TestRenderInsertsMetaWhenMissing(t *testing.T) {
	in := pilotInput()
	in.Files["README.md"] = "# P\n\nBody.\n"
	got := renderREADME(t, in)
	if !strings.Contains(got, "# P\n\n## Meta\n\n- **Status:** In Progress\n") {
		t.Errorf("Meta not inserted after the title:\n%s", got)
	}
}

func TestRenderErrors(t *testing.T) {
	cases := map[string]struct {
		file    string
		body    string
		wantErr string
	}{
		"unclosed marker": {"README.md", "# P\n" + ExcludeStart + "\nsecret\n", "never closed"},
		"stray end":       {"decisions.md", "x\n" + ExcludeEnd + "\n", "without a matching start"},
		"nested marker":   {"decisions.md", ExcludeStart + "\n" + ExcludeStart + "\n" + ExcludeEnd + "\n", "nested"},
		"escaping link":   {"decisions.md", "[up](../../../etc/passwd)\n", "escapes the workspace"},
		"resume included": {"resume.md", "cursor\n", "never exported"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := pilotInput()
			in.Files[tc.file] = tc.body
			_, err := Render(in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRenderRequiresStamp(t *testing.T) {
	in := pilotInput()
	in.Stamp.Owner = ""
	if _, err := Render(in); err == nil {
		t.Fatal("expected an error for a missing owner")
	}
}

func TestRelativePath(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"pilot", "pilot/decisions.md", "decisions.md"},
		{"pilot", "sibling/README.md", "../sibling/README.md"},
		{"pilot/sub", "pilot/decisions.md", "../decisions.md"},
	}
	for _, tc := range cases {
		if got := relativePath(tc.from, tc.to); got != tc.want {
			t.Errorf("relativePath(%q, %q) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestCodeSpans(t *testing.T) {
	line := "a `b` c ``d ` e`` f `unclosed"
	got := codeSpans(line)
	want := [][2]int{{2, 5}, {8, 17}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("codeSpans = %v, want %v", got, want)
	}
}

// Link shapes beyond the plain inline form: each must be rewritten, kept or
// de-linked, and never leave a private path behind.
func TestRenderLinkForms(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"angle-bracket private":   {"[a](<../../notes/x y.md>)", "a *(private)*"},
		"angle-bracket exported":  {"[a](<decisions.md>)", "[a](<decisions.md>)"},
		"parenthesized private":   {"[a](../../notes/x(1).md) z", "a *(private)* z"},
		"titled private":          {`[a](../../notes/x.md "t")`, "a *(private)*"},
		"linked image, both priv": {"[![alt](../../notes/i.png)](../../notes/x.md)", "alt *(private)* *(private)*"},
		"linked image, ext outer": {"[![alt](../../notes/i.png)](https://e.com)", "[alt *(private)*](https://e.com)"},
		"file scheme":             {"[a](file:///Users/me/x.md)", "a *(private)*"},
		"uppercase file scheme":   {"[a](FILE:///tmp/x)", "a *(private)*"},
		"windows drive path":      {`[a](C:\Users\me\x.md)`, "a *(private)*"},
		"ref def private":         {"[n]: ../../notes/x.md", ""},
		"ref def exported":        {"[n]: decisions.md#dec-001", "[n]: decisions.md#dec-001"},
		"ref def external":        {"[n]: https://e.com/x", "[n]: https://e.com/x"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := pilotInput()
			in.Files["decisions.md"] = tc.in + "\n"
			out, err := Render(in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			body := out["pilot/decisions.md"]
			line := strings.TrimSuffix(body[strings.Index(body, "\n\n")+2:], "\n")
			if line != tc.want {
				t.Errorf("got %q, want %q", line, tc.want)
			}
		})
	}
}

func TestRenderRejectsUnsupportedLinkForm(t *testing.T) {
	in := pilotInput()
	in.Files["decisions.md"] = "[[[deep]]](../../notes/x.md)\n"
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "unsupported link form") {
		t.Fatalf("want unsupported-link-form error, got %v", err)
	}
}
