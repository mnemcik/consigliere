package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexedProject(t *testing.T) {
	index := filepath.Join(t.TempDir(), "TODO.md")
	body := "| # | Project | Status | Areas | Folder |\n" +
		"|---|---|---|---|---|\n" +
		"| 1 | Pilot | In Progress | `a`, `b` | [pilot](pilot/README.md) |\n"
	if err := os.WriteFile(index, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := indexedProject(index, "pilot")
	if err != nil {
		t.Fatalf("indexedProject: %v", err)
	}
	if p.Slug != "pilot" || p.Status != "In Progress" || strings.Join(p.Areas, ",") != "a,b" {
		t.Errorf("got %+v", p)
	}

	if _, err := indexedProject(index, "missing"); err == nil || !strings.Contains(err.Error(), "no row") {
		t.Errorf("want no-row error, got %v", err)
	}
}

func TestParseAcks(t *testing.T) {
	acks, err := parseAcks([]string{"local-path:6c54c54b471a3e54", "placeholder:abc"})
	if err != nil {
		t.Fatalf("parseAcks: %v", err)
	}
	if len(acks) != 2 || acks[0].Rule != "local-path" || acks[0].Hash != "6c54c54b471a3e54" {
		t.Errorf("got %+v", acks)
	}
	for _, bad := range []string{"nohash", ":abc", "rule:"} {
		if _, err := parseAcks([]string{bad}); err == nil {
			t.Errorf("parseAcks(%q): expected an error", bad)
		}
	}
}
