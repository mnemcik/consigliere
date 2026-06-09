package cmd

import (
	"bufio"
	"regexp"
	"strings"
	"testing"
)

// The two-zone ownership contract (workspace-sync DEC-002): every framework
// rule in the embedded workspace CLAUDE.md must live inside a `cg:section`
// block, every user-customizable area inside a `user:section` block, and
// nothing of substance may sit *between* zones unowned. The single allowed
// exception is the document's H1 title. This test enforces the contract so a
// later edit can't silently reintroduce orphan content that `cg sync` would
// then leave frozen forever.
func TestEmbeddedCLAUDEHasNoOrphanContent(t *testing.T) {
	const path = "embed_templates/workspace/CLAUDE.md"
	data, err := embeddedFS.ReadFile(path)
	if err != nil {
		t.Fatalf("reading embedded %s: %v", path, err)
	}

	var (
		startRe = regexp.MustCompile(`^<!-- (cg|user):section:start=([a-z0-9-]+) -->[ \t]*$`)
		endRe   = regexp.MustCompile(`^<!-- (cg|user):section:end=([a-z0-9-]+) -->[ \t]*$`)
		// Allowed outside any zone: the H1 title only.
		titleRe = regexp.MustCompile(`^# \S`)
	)

	zone := "" // "" = between zones; otherwise "kind:id" of the open block
	lineNo := 0
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		lineNo++
		line := sc.Text()

		if m := startRe.FindStringSubmatch(line); m != nil {
			if zone != "" {
				t.Fatalf("line %d: nested section start %q inside open zone %q", lineNo, m[1]+":"+m[2], zone)
			}
			zone = m[1] + ":" + m[2]
			continue
		}
		if m := endRe.FindStringSubmatch(line); m != nil {
			want := m[1] + ":" + m[2]
			if zone != want {
				t.Fatalf("line %d: section end %q does not match open zone %q", lineNo, want, zone)
			}
			zone = ""
			continue
		}

		if zone != "" {
			continue // inside an owned zone — fine
		}
		if strings.TrimSpace(line) == "" || titleRe.MatchString(line) {
			continue // blank line or the H1 title — the only allowed orphans
		}
		t.Errorf("line %d: orphan content outside any cg:/user:section zone: %q", lineNo, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning template: %v", err)
	}
	if zone != "" {
		t.Errorf("unclosed section zone %q at end of file", zone)
	}
}
