package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatuslineNoBadgeWithoutContext(t *testing.T) {
	root := t.TempDir()
	// No context file → base output passed through unchanged (empty, no upstream).
	got := Statusline(context.Background(), root, StatuslineInput{SessionID: "s"}, "", "")
	if got != "" {
		t.Errorf("expected empty output without context, got %q", got)
	}
}

func TestStatuslineBadge(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"consigliere","project":"cg-subcommands","dirty":true}`)
	// Project README with an explicit color.
	projDir := filepath.Join(root, "projects", "cg-subcommands")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "README.md"),
		[]byte("# x\n\n- **Color:** bright-yellow\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Statusline(context.Background(), root, StatuslineInput{SessionID: "s1"}, "", "")
	if !strings.Contains(got, "[consigliere/cg-subcommands]") {
		t.Errorf("badge label missing: %q", got)
	}
	if !strings.Contains(got, "\033[1;93m") { // 93 is the bright-yellow ANSI foreground
		t.Errorf("expected bright-yellow color code, got %q", got)
	}
	if !strings.Contains(got, "●") {
		t.Errorf("expected dirty dot, got %q", got)
	}
}

func TestStatuslineSingleSide(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s2", `{"area":"consigliere"}`)
	got := Statusline(context.Background(), root, StatuslineInput{SessionID: "s2"}, "", "")
	if !strings.Contains(got, "[consigliere]") {
		t.Errorf("expected collapsed single-side label, got %q", got)
	}
}

func TestReadColorField(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"- **Color:** bright-cyan\n": "bright-cyan",
		"**Color:** red\n":           "red",
		"- **Color:** `blue`\n":      "blue",
		"- **Color:** {color}\n":     "", // placeholder
		"no color here\n":            "",
	}
	i := 0
	for content, want := range cases {
		f := filepath.Join(dir, "f"+string(rune('a'+i))+".md")
		i++
		if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readColorField(f); got != want {
			t.Errorf("readColorField(%q) = %q, want %q", content, got, want)
		}
	}
}

func TestColorFromSlugDeterministic(t *testing.T) {
	a := colorFromSlug("my-project")
	b := colorFromSlug("my-project")
	if a != b {
		t.Errorf("colorFromSlug not deterministic: %d vs %d", a, b)
	}
	found := false
	for _, c := range brightPalette {
		if c == a {
			found = true
		}
	}
	if !found {
		t.Errorf("color %d not in palette", a)
	}
}
