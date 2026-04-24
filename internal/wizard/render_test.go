package wizard

import (
	"strings"
	"testing"
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
	}
	for in, want := range cases {
		if got := SanitizeSlug(in); got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
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
		AreaCategory: "Service/System",
		AreaOverview: "Computes pension benefits.",
	}
	got := RenderArea(&a, "2026-04-24")
	for _, s := range []string{
		"# Pension Calculation",
		"- **Slug:** `pension-calc`",
		"- **Category:** Service/System",
		"- **Created:** 2026-04-24",
		"Computes pension benefits.",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("RenderArea missing %q", s)
		}
	}
}

func TestInsertAreaIndexRow_EmptyTable(t *testing.T) {
	index := `# Areas Index

## Service/System Areas

| Area | Slug | Description |
|------|------|-------------|

## Practice/Platform Areas

| Area | Slug | Description |
|------|------|-------------|
`
	a := Answers{
		AreaSlug: "pension-calc", AreaName: "Pension Calc",
		AreaCategory: "Service/System", AreaOverview: "Pensions. Done.",
	}
	got := InsertAreaIndexRow(index, &a)
	wantRow := "| [Pension Calc](pension-calc.md) | `pension-calc` | Pensions |"
	if !strings.Contains(got, wantRow) {
		t.Errorf("expected row %q in output, got:\n%s", wantRow, got)
	}
	// Must appear in Service/System section, not Practice/Platform.
	ssIdx := strings.Index(got, "## Service/System Areas")
	ppIdx := strings.Index(got, "## Practice/Platform Areas")
	rowIdx := strings.Index(got, wantRow)
	if rowIdx < ssIdx || rowIdx > ppIdx {
		t.Errorf("row inserted in wrong section; ss=%d row=%d pp=%d", ssIdx, rowIdx, ppIdx)
	}
}

func TestInsertAreaIndexRow_PracticePlatformCategory(t *testing.T) {
	index := `# Areas Index

## Service/System Areas

| Area | Slug | Description |
|------|------|-------------|

## Practice/Platform Areas

| Area | Slug | Description |
|------|------|-------------|
`
	a := Answers{
		AreaSlug: "devops", AreaName: "DevOps",
		AreaCategory: "Practice/Platform", AreaOverview: "CI/CD",
	}
	got := InsertAreaIndexRow(index, &a)
	ppIdx := strings.Index(got, "## Practice/Platform Areas")
	rowIdx := strings.Index(got, "| [DevOps](devops.md) | `devops` |")
	if rowIdx <= ppIdx {
		t.Errorf("row should be after Practice/Platform header; pp=%d row=%d", ppIdx, rowIdx)
	}
}

func TestInsertAreaIndexRow_AppendsAfterExistingRows(t *testing.T) {
	index := `## Service/System Areas

| Area | Slug | Description |
|------|------|-------------|
| [Existing](existing.md) | ` + "`existing`" + ` | First |
`
	a := Answers{
		AreaSlug: "new-one", AreaName: "New",
		AreaCategory: "Service/System", AreaOverview: "Second",
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
