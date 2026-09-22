package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/workspace"
)

// writeUserSettings writes a ~/.claude/settings.json with the given statusLine
// command under home and returns home.
func writeUserSettings(t *testing.T, home, statusLineCommand string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"statusLine":{"type":"command","command":"` + statusLineCommand + `","padding":1}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadStatusLineCommand(t *testing.T) {
	home := t.TempDir()
	writeUserSettings(t, home, "bash ~/.claude/statusline.sh")
	if got := readStatusLineCommand(filepath.Join(home, ".claude", "settings.json")); got != "bash ~/.claude/statusline.sh" {
		t.Errorf("readStatusLineCommand = %q", got)
	}
	// Missing file → "".
	if got := readStatusLineCommand(filepath.Join(home, "nope.json")); got != "" {
		t.Errorf("missing file: got %q, want empty", got)
	}
	// Malformed JSON → "".
	bad := filepath.Join(home, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readStatusLineCommand(bad); got != "" {
		t.Errorf("malformed: got %q, want empty", got)
	}
	// No statusLine field → "".
	none := filepath.Join(home, "none.json")
	if err := os.WriteFile(none, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readStatusLineCommand(none); got != "" {
		t.Errorf("no statusLine: got %q, want empty", got)
	}
}

func TestIsRecursiveStatusline(t *testing.T) {
	cases := map[string]bool{
		"":                                true, // nothing to preserve
		"cg session statusline":           true,
		"bash -c 'cg session statusline'": true,
		// The user's real status line shares the wrapper basename but is NOT the
		// cg renderer — it must be preserved.
		"bash ~/.claude/statusline.sh":        false,
		"bash /Users/x/.claude/statusline.sh": false,
		"node ~/.claude/line.js":              false,
	}
	for cmd, want := range cases {
		if got := isRecursiveStatusline(cmd); got != want {
			t.Errorf("isRecursiveStatusline(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestDetectStatuslineUpstream(t *testing.T) {
	// Prior value wins (re-init preserves a user-customized upstream).
	prior := &workspace.SessionConfig{StatuslineUpstream: "bash /custom.sh"}
	if got := detectStatuslineUpstream(prior, t.TempDir()); got != "bash /custom.sh" {
		t.Errorf("prior not preserved: %q", got)
	}

	// No prior → read the user's global statusLine command.
	home := t.TempDir()
	writeUserSettings(t, home, "bash ~/.claude/statusline.sh")
	if got := detectStatuslineUpstream(nil, home); got != "bash ~/.claude/statusline.sh" {
		t.Errorf("did not detect global statusLine: %q", got)
	}

	// No prior + a recursive global command → "" (never self-reference).
	rec := t.TempDir()
	writeUserSettings(t, rec, "cg session statusline")
	if got := detectStatuslineUpstream(nil, rec); got != "" {
		t.Errorf("recursive command not skipped: %q", got)
	}

	// No prior + no settings file → "".
	if got := detectStatuslineUpstream(nil, t.TempDir()); got != "" {
		t.Errorf("missing settings: got %q, want empty", got)
	}

	// Empty home → "" (no detection possible).
	if got := detectStatuslineUpstream(nil, ""); got != "" {
		t.Errorf("empty home: got %q, want empty", got)
	}
}
