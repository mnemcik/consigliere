package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// S35: recording the worktree keeps the file's other fields and creates the
// file when absent.
func TestSetWorktree(t *testing.T) {
	root := t.TempDir()
	if err := SetWorktree(root, "s1", "/w/one"); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContext(root, "s1")
	if err != nil || c == nil || c.Worktree != "/w/one" {
		t.Fatalf("after create: %+v, %v", c, err)
	}
	writeCtx(t, root, "s2", `{"area":"a","project":"p","dirty":true,"extra":7}`)
	if err := SetWorktree(root, "s2", "/w/two"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ContextFile(root, "s2"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["area"] != "a" || m["project"] != "p" || m["dirty"] != true || m["extra"] != float64(7) || m["worktree"] != "/w/two" {
		t.Errorf("fields after SetWorktree: %v", m)
	}
	// Claiming a project afterwards keeps the worktree.
	if err := WriteContext(root, "s2", Claim{Area: "b", Project: "q"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := ReadContext(root, "s2"); c == nil || c.Worktree != "/w/two" {
		t.Errorf("WriteContext dropped the worktree: %+v", c)
	}
}

// S37: clearing removes the record only when it names that worktree.
func TestClearWorktree(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"area":"a","project":"p","worktree":"/w/one"}`)
	if cleared, err := ClearWorktree(root, "s1", "/w/other"); err != nil || cleared {
		t.Errorf("another worktree: cleared=%v err=%v", cleared, err)
	}
	if cleared, err := ClearWorktree(root, "s1", "/w/one"); err != nil || !cleared {
		t.Errorf("the recorded worktree: cleared=%v err=%v", cleared, err)
	}
	c, _ := ReadContext(root, "s1")
	if c == nil || c.Worktree != "" || c.Area != "a" {
		t.Errorf("after clear: %+v", c)
	}
	// No file: nothing to clear, and none is created.
	if cleared, err := ClearWorktree(root, "none", "/w/one"); err != nil || cleared {
		t.Errorf("no file: cleared=%v err=%v", cleared, err)
	}
	if _, err := os.Stat(ContextFile(root, "none")); !os.IsNotExist(err) {
		t.Error("ClearWorktree created a state file")
	}
}

// S39: an invalid session ID reads and writes nothing, whoever calls.
func TestInvalidSessionIDReadsAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "x.json")
	if err := os.WriteFile(outside, []byte(`{"area":"leak","project":"p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// "../x" would name root/.claude/x.json from session-context: still
	// refused, as is a separator of any kind.
	for _, id := range []string{"../../x", "../x", "a/b", `a\b`, "", "."} {
		if c, err := ReadContext(root, id); err == nil || c != nil {
			t.Errorf("ReadContext(%q) = %+v, %v; want an error", id, c, err)
		}
		if err := SetWorktree(root, id, "/w"); err == nil {
			t.Errorf("SetWorktree(%q) succeeded", id)
		}
		if _, err := ClearWorktree(root, id, "/w"); err == nil {
			t.Errorf("ClearWorktree(%q) succeeded", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Errorf("a session-context directory was created: %v", err)
	}
}

// S40: a state file with only a worktree shows as no badge and no session.
func TestWorktreeOnlyStateShowsNothing(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"worktree":"/w/one"}`)
	if got := Statusline(context.Background(), root, StatuslineInput{SessionID: "s1"}, "", ""); got != "" {
		t.Errorf("status line = %q, want none", got)
	}
	active, err := ActiveProjects(root, ActiveOptions{Now: time.Now(), ActiveWindow: time.Hour, DirtyWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Errorf("active = %+v, want none", active)
	}
}
