package session

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if err != nil || c == nil || c.Worktree != filepath.FromSlash("/w/one") {
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
	if c, _ := ReadContext(root, "s2"); c == nil || c.Worktree != filepath.FromSlash("/w/two") {
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

// The worktree is stored with '/' separators (files cg stores use them on
// every OS) and read back in the OS's form.
func TestWorktreeStoredWithSlashes(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "ws--x")
	if err := SetWorktree(root, "s1", p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ContextFile(root, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["worktree"] != filepath.ToSlash(p) {
		t.Errorf("stored %v, want %s", m["worktree"], filepath.ToSlash(p))
	}
	if c, _ := ReadContext(root, "s1"); c == nil || c.Worktree != p {
		t.Errorf("read back %+v, want %s", c, p)
	}
	if cleared, err := ClearWorktree(root, "s1", p); err != nil || !cleared {
		t.Errorf("clear: %v %v", cleared, err)
	}
}

// ClearWorktreeAll clears every session's record of a worktree, even under a
// root whose path has glob metacharacters.
func TestClearWorktreeAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj[2]")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := SetWorktree(root, id, "/w/x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetWorktree(root, "c", "/w/y"); err != nil {
		t.Fatal(err)
	}
	if err := ClearWorktreeAll(root, "/w/x"); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"a": "", "b": "", "c": filepath.FromSlash("/w/y")} {
		if c, _ := ReadContext(root, id); c == nil || c.Worktree != want {
			t.Errorf("%s: %+v, want worktree %q", id, c, want)
		}
	}
}

// A dirty state file keeps its worktree record at session end, claim or not:
// a resumed session (same ID) still writes into its worktree.
func TestEndSessionKeepsDirtyWorktreeRecord(t *testing.T) {
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"worktree":"/w/x","dirty":true}`)
	if released, err := EndSession(root, root, "s1"); err != nil || released {
		t.Errorf("EndSession: %v %v", released, err)
	}
	if c, _ := ReadContext(root, "s1"); c == nil || c.Worktree != filepath.FromSlash("/w/x") {
		t.Errorf("worktree record lost: %+v", c)
	}
}

// A record written in the OS's form (by an earlier build) is still cleared.
func TestClearWorktreeOSFormRecord(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "ws--x")
	data, _ := json.Marshal(map[string]any{"worktree": p})
	writeCtx(t, root, "s1", string(data))
	if cleared, err := ClearWorktree(root, "s1", p); err != nil || !cleared {
		t.Errorf("clear: %v %v", cleared, err)
	}
}

// ClearWorktreeAll reports a state file it can't read, and goes on with the
// others.
func TestClearWorktreeAllReportsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := t.TempDir()
	if err := SetWorktree(root, "a", "/w/x"); err != nil {
		t.Fatal(err)
	}
	if err := SetWorktree(root, "b", "/w/x"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ContextFile(root, "a"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ContextFile(root, "a"), 0o644) }) //nolint:gosec // restore the fixture
	err := ClearWorktreeAll(root, "/w/x")
	if err == nil || !strings.Contains(err.Error(), "a.json") {
		t.Errorf("err = %v, want one naming a.json", err)
	}
	if c, _ := ReadContext(root, "b"); c == nil || c.Worktree != "" {
		t.Errorf("b not cleared: %+v", c)
	}
}

// On a case-sensitive volume (possible on macOS and Windows too), a record
// that differs only in case is another worktree's and stays: there, the
// record's directory exists and the removed one doesn't.
func TestClearWorktreeCaseSensitiveVolume(t *testing.T) {
	orig := statPath
	t.Cleanup(func() { statPath = orig })
	statPath = func(p string) (fs.FileInfo, error) {
		if filepath.Base(p) == "Task" {
			return os.Stat(t.TempDir())
		}
		return nil, fs.ErrNotExist
	}
	root := t.TempDir()
	writeCtx(t, root, "s1", `{"worktree":"/w/Task"}`)
	if cleared, err := ClearWorktree(root, "s1", "/w/task"); err != nil || cleared {
		t.Errorf("cleared %v (%v), want the record of /w/Task kept", cleared, err)
	}
	if cleared, err := ClearWorktree(root, "s1", "/w/Task"); err != nil || !cleared {
		t.Errorf("cleared %v (%v), want the same path cleared", cleared, err)
	}
}

// Two spellings of one existing directory are one worktree where the volume
// folds case, and two where it doesn't; with neither left, they fold.
func TestSameEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(dir), "tASK")
	_, err := os.Stat(other)
	if got, want := sameEntry(dir, other), err == nil; got != want {
		t.Errorf("sameEntry(existing, other case) = %v, want %v (the volume's behaviour)", got, want)
	}
	gone := filepath.Join(filepath.Dir(dir), "gone")
	if !sameEntry(gone, filepath.Join(filepath.Dir(dir), "GONE")) {
		t.Error("sameEntry of two missing spellings = false, want true")
	}
}
