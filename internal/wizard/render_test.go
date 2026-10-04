package wizard

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Pension Calc":         "pension-calc",
		"  SPACES  ":           "spaces",
		"foo/bar_baz":          "foo-bar-baz",
		"--leading-trailing--": "leading-trailing",
		"UPPER":                "upper",
		"":                     "",
		"!!!":                  "",
		"foo--bar":             "foo-bar",
		"foo - bar":            "foo-bar",
		"a___b___c":            "a-b-c",
	}
	for in, want := range cases {
		if got := SanitizeSlug(in); got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	cases := map[string]string{
		"":                                       "",
		"  ":                                     "",
		"microservice":                           "microservice",
		"Microservice":                           "microservice",
		"microservice, compliance":               "microservice, compliance",
		"  microservice ,compliance  ":           "microservice, compliance",
		"microservice,microservice,MICROSERVICE": "microservice",
		"a,,b":                                   "a, b",
		"a, , b":                                 "a, b",
		"foo, bar, foo":                          "foo, bar",
	}
	for in, want := range cases {
		if got := normalizeTags(in); got != want {
			t.Errorf("normalizeTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderProfile_WithAnswers(t *testing.T) {
	a := Answers{
		ProfileName:  "Matus",
		ProfileRole:  "Staff engineer at Acme",
		ProfileFocus: "API design\nBacklog hygiene",
	}
	got := RenderProfile(&a)
	for _, s := range []string{
		"Staff engineer at Acme",
		"- API design",
		"- Backlog hygiene",
		"- Owner: Matus",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("RenderProfile missing %q\n--- output ---\n%s", s, got)
		}
	}
}

func TestRenderProfile_EmptyAnswersFallsBackToPlaceholders(t *testing.T) {
	got := RenderProfile(&Answers{})
	for _, s := range []string{
		"[Your role and organization]",
		"[Primary responsibility]",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("RenderProfile missing placeholder %q", s)
		}
	}
	if strings.Contains(got, "- Owner:") {
		t.Errorf("RenderProfile should not include Owner line when name is empty")
	}
}

func TestRenderArea(t *testing.T) {
	a := Answers{
		AreaSlug:     "pension-calc",
		AreaName:     "Pension Calculation",
		AreaTags:     "microservice, compliance",
		AreaOverview: "Computes pension benefits.",
	}
	got := RenderArea(&a, "2026-04-24")
	for _, s := range []string{
		"# Pension Calculation",
		"## Review History\n",
		"- 2026-04-24 — created.",
		"Computes pension benefits.",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("RenderArea missing %q", s)
		}
	}
	if strings.Contains(got, "## Meta") {
		t.Errorf("RenderArea still writes a ## Meta block:\n%s", got)
	}
	fm := areaFM(t, got)
	want := map[string]any{
		"title": "Pension Calculation",
		"slug":  "pension-calc",
		"tags":  []any{"microservice", "compliance"},
		// Bare, so YAML reads them as dates, matching the templates.
		"created":       time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
		"last_reviewed": time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(fm, want) {
		t.Errorf("frontmatter = %#v\nwant %#v", fm, want)
	}
}

// A name a hand-formatted block would break: a colon, quotes and a leading
// brace all need quoting to stay valid YAML.
func TestRenderAreaFrontmatterQuotesAwkwardNames(t *testing.T) {
	a := Answers{AreaSlug: "x", AreaName: `{Odd}: "quoted" name`, AreaTags: "B, b, a"}
	fm := areaFM(t, RenderArea(&a, "2026-04-24"))
	if fm["title"] != `{Odd}: "quoted" name` {
		t.Errorf("title = %#v", fm["title"])
	}
	if !reflect.DeepEqual(fm["tags"], []any{"b", "a"}) {
		t.Errorf("tags = %#v, want lower-cased and de-duplicated", fm["tags"])
	}
}

func TestRenderAreaNoTagsIsEmptyList(t *testing.T) {
	fm := areaFM(t, RenderArea(&Answers{AreaSlug: "x", AreaName: "X"}, "2026-04-24"))
	if tags, ok := fm["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("tags = %#v, want an explicit empty list", fm["tags"])
	}
}

// areaFM parses the frontmatter block at the top of a rendered area file.
func areaFM(t *testing.T, doc string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(doc, "---\n") {
		t.Fatalf("no frontmatter at the top:\n%s", doc)
	}
	end := strings.Index(doc[4:], "\n---\n")
	if end < 0 {
		t.Fatalf("unterminated frontmatter:\n%s", doc)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(doc[4:4+end]), &fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v\n%s", err, doc)
	}
	return fm
}

func TestInsertAreaIndexRow_EmptyTable(t *testing.T) {
	index := `# Areas Index

## Areas

| Area | Slug | Tags | Description |
|------|------|------|-------------|
`
	a := Answers{
		AreaSlug: "pension-calc", AreaName: "Pension Calc",
		AreaTags: "microservice", AreaOverview: "Pensions. Done.",
	}
	got := InsertAreaIndexRow(index, &a)
	wantRow := "| [Pension Calc](pension-calc.md) | `pension-calc` | microservice | Pensions |"
	if !strings.Contains(got, wantRow) {
		t.Errorf("expected row %q in output, got:\n%s", wantRow, got)
	}
}

func TestInsertAreaIndexRow_AppendsAfterExistingRows(t *testing.T) {
	index := `## Areas

| Area | Slug | Tags | Description |
|------|------|------|-------------|
| [Existing](existing.md) | ` + "`existing`" + ` | practice | First |
`
	a := Answers{
		AreaSlug: "new-one", AreaName: "New",
		AreaTags: "microservice", AreaOverview: "Second",
	}
	got := InsertAreaIndexRow(index, &a)
	lines := strings.Split(got, "\n")
	existingIdx, newIdx := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "existing.md") {
			existingIdx = i
		}
		if strings.Contains(l, "new-one.md") {
			newIdx = i
		}
	}
	if existingIdx == -1 || newIdx == -1 {
		t.Fatalf("rows not found: existing=%d new=%d\n%s", existingIdx, newIdx, got)
	}
	if newIdx != existingIdx+1 {
		t.Errorf("new row should follow existing row; existing=%d new=%d", existingIdx, newIdx)
	}
}

func TestInsertAreaIndexRow_Idempotent(t *testing.T) {
	index := `## Areas

| Area | Slug | Tags | Description |
|------|------|------|-------------|
| [Pension Calc](pension-calc.md) | ` + "`pension-calc`" + ` | microservice | First |
`
	a := Answers{
		AreaSlug: "pension-calc", AreaName: "Pension Calc",
		AreaTags: "microservice", AreaOverview: "Duplicate attempt",
	}
	got := InsertAreaIndexRow(index, &a)
	if got != index {
		t.Errorf("expected index unchanged when slug already present; got diff:\n%s", got)
	}
	if strings.Count(got, "pension-calc.md") != 1 {
		t.Errorf("slug appears more than once: %d", strings.Count(got, "pension-calc.md"))
	}
}

func TestInsertAreaIndexRow_EscapesPipesAndNewlines(t *testing.T) {
	index := `## Areas

| Area | Slug | Tags | Description |
|------|------|------|-------------|
`
	a := Answers{
		AreaSlug: "weird-one", AreaName: "Weird | Name",
		AreaTags:     "has | pipe",
		AreaOverview: "first line\nsecond line with | pipe.",
	}
	got := InsertAreaIndexRow(index, &a)
	// Raw pipes from user input must be escaped so the table still parses.
	if !strings.Contains(got, `Weird \| Name`) {
		t.Errorf("pipe in AreaName not escaped; got:\n%s", got)
	}
	if !strings.Contains(got, `has \| pipe`) {
		t.Errorf("pipe in AreaTags not escaped; got:\n%s", got)
	}
	// Each output row must be exactly one line.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "weird-one.md") {
			if strings.Contains(line, "\n") {
				t.Errorf("row contains an embedded newline: %q", line)
			}
		}
	}
}

func TestRenderers_NilSafe(t *testing.T) {
	if got := RenderProfile(nil); !strings.Contains(got, "[Your role and organization]") {
		t.Errorf("RenderProfile(nil) should render placeholder profile, got:\n%s", got)
	}
	if got := RenderArea(nil, "2026-04-24"); got != "" {
		t.Errorf("RenderArea(nil) = %q, want empty string", got)
	}
	index := "## Areas\n\n| A | B | C | D |\n|---|---|---|---|\n"
	if got := InsertAreaIndexRow(index, nil); got != index {
		t.Errorf("InsertAreaIndexRow(_, nil) should return input unchanged")
	}
	var a *Answers
	if a.HasFirstArea() {
		t.Errorf("(*Answers)(nil).HasFirstArea() should be false")
	}
}

func TestAnswersHasFirstArea(t *testing.T) {
	cases := []struct {
		a    Answers
		want bool
	}{
		{Answers{AreaSlug: "x", AreaName: "X"}, true},
		{Answers{AreaSlug: "", AreaName: "X"}, false},
		{Answers{AreaSlug: "x", AreaName: ""}, false},
		{Answers{}, false},
	}
	for _, c := range cases {
		if got := c.a.HasFirstArea(); got != c.want {
			t.Errorf("HasFirstArea(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}
