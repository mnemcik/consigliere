package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStatusFile writes content to dir/rel, creating parent directories.
func writeStatusFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const twoTableIndex = `# Projects & Todo List

## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
| 1 | Alpha | In Progress | ` + "`a`" + ` | [alpha](alpha/README.md) |
| 2 | Beta | Done | ` + "`a`" + ` | [beta](beta/) |
| 3 | Gamma | Defining | ` + "`a`" + ` | [gamma](gamma/) |

## Standing Commitments

Recurring duties with no finish line.

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
| — | Weekly Review | In Progress | ` + "`a`" + ` | [weekly-review](weekly-review/README.md) |
| — | Retired Duty | Done | ` + "`a`" + ` | [retired-duty](retired-duty/) |
`

func TestParseProjectIndexSkipsEveryTableHeader(t *testing.T) {
	dir := t.TempDir()
	writeStatusFile(t, dir, "TODO.md", twoTableIndex)

	projects, err := parseProjectIndex(filepath.Join(dir, "TODO.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(projects))
	for _, p := range projects {
		got = append(got, p.Name+"@"+p.Section)
	}
	want := []string{
		"Alpha@Active Projects", "Beta@Active Projects", "Gamma@Active Projects",
		"Weekly Review@Standing Commitments", "Retired Duty@Standing Commitments",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rows = %v, want %v (a second table's header or separator must not parse as a project)", got, want)
	}
}

func TestProjectsSummaryStandingTable(t *testing.T) {
	dir := t.TempDir()
	writeStatusFile(t, dir, "TODO.md", twoTableIndex)

	// Done standing rows are not counted as standing; the rest of the
	// standing table is, with or without a README.
	got := projectsSummary(filepath.Join(dir, "TODO.md"))
	want := "Projects: 5 total, 2 active, 1 standing"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectsSummaryReadmeHeading(t *testing.T) {
	// An index with no standing table, as in a workspace initialised before
	// the table existed: the README heading alone marks a standing project.
	dir := t.TempDir()
	writeStatusFile(t, dir, "TODO.md", `## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
| 1 | Alpha | In Progress | a | [alpha](alpha/) |
| 2 | Digest | In Progress | a | [digest](digest/README.md) |
| 3 | Monitor | On Hold | a | [monitor](monitor/) |
| 4 | Fenced | In Progress | a | [fenced](fenced/) |
| 5 | No Readme | In Progress | a | [no-readme](no-readme/) |
`)
	writeStatusFile(t, dir, "alpha/README.md", "# Alpha\n\n## Goals\n\n- ship\n")
	// CRLF line endings, as in a Windows checkout.
	writeStatusFile(t, dir, "digest/README.md", "# Digest\r\n\r\n## Standard to Maintain\r\n\r\n- weekly\r\n")
	// A standing project that is on hold is still standing, not active.
	writeStatusFile(t, dir, "monitor/README.md", "# Monitor\n\n## Standard to Maintain\n")
	// The heading quoted inside a code block is not the project's own.
	writeStatusFile(t, dir, "fenced/README.md", "# Fenced\n\n## Goals\n\n```md\n## Standard to Maintain\n```\n")

	got := projectsSummary(filepath.Join(dir, "TODO.md"))
	want := "Projects: 5 total, 3 active, 2 standing"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectsSummaryNoStanding(t *testing.T) {
	// Neither signal anywhere: output is unchanged from before standing
	// projects existed.
	dir := t.TempDir()
	writeStatusFile(t, dir, "TODO.md", `## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
| 1 | Alpha | In Progress | a | [alpha](alpha/) |
| 2 | Beta | done | a | [beta](beta/) |
`)
	writeStatusFile(t, dir, "alpha/README.md", "# Alpha\n\nno headings at all\n")

	if got, want := projectsSummary(filepath.Join(dir, "TODO.md")), "Projects: 2 total, 1 active"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectsSummaryEmptyOrMissing(t *testing.T) {
	dir := t.TempDir()
	if got := projectsSummary(filepath.Join(dir, "missing.md")); got != "Projects: 0" {
		t.Errorf("missing index: got %q", got)
	}
	writeStatusFile(t, dir, "TODO.md", indexProjectsTODO)
	if got := projectsSummary(filepath.Join(dir, "TODO.md")); got != "Projects: 0" {
		t.Errorf("fresh index: got %q", got)
	}
}

func TestHasStandingHeading(t *testing.T) {
	tests := []struct {
		name, body string
		want       bool
	}{
		{"plain", "# P\n\n## Standard to Maintain\n", true},
		{"crlf", "# P\r\n\r\n## Standard to Maintain\r\n", true},
		{"up to three spaces of indent", "   ## Standard to Maintain\n", true},
		{"after a closed fence", "```\ncode\n```\n## Standard to Maintain\n", true},
		{"goals only", "# P\n\n## Goals\n", false},
		{"indented code block", "    ## Standard to Maintain\n", false},
		{"inside a fence", "```md\n## Standard to Maintain\n```\n", false},
		{"inside a tilde fence", "~~~\n## Standard to Maintain\n~~~\n", false},
		{"three backticks do not close four", "````md\n```\n## Standard to Maintain\n````\n", false},
		{"a different heading", "## Standard to Maintain later\n", false},
	}
	dir := t.TempDir()
	for i, tt := range tests {
		rel := filepath.Join("p", strings.Repeat("x", i+1), "README.md")
		writeStatusFile(t, dir, rel, tt.body)
		if got := hasStandingHeading(filepath.Join(dir, rel)); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
	if hasStandingHeading(filepath.Join(dir, "missing.md")) {
		t.Error("missing file: got true")
	}
}
