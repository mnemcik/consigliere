package extension

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterUnregisterHookPreservesOtherKeys(t *testing.T) {
	root := t.TempDir()
	// Seed a settings.json with an unmanaged key and a pre-existing framework hook.
	seed := `{
	  "statusLine": {"type":"command","command":".claude/statusline.sh"},
	  "hooks": {"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":".claude/hooks/pull-latest-main.sh"}]}]}
	}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(root), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registerHook(root, "SessionStart", ".claude/hooks/demo.sh"); err != nil {
		t.Fatalf("registerHook: %v", err)
	}

	settings := readSettings(t, root)
	// Unmanaged key preserved.
	if _, ok := settings["statusLine"]; !ok {
		t.Error("statusLine must be preserved")
	}
	hooks := settings["hooks"].(map[string]any)
	ss := hooks["SessionStart"].([]any)
	if len(ss) != 2 {
		t.Fatalf("expected the framework hook plus the new one, got %d", len(ss))
	}

	// Unregister removes only our entry; the framework hook remains.
	if err := unregisterHook(root, ".claude/hooks/demo.sh"); err != nil {
		t.Fatalf("unregisterHook: %v", err)
	}
	settings = readSettings(t, root)
	hooks = settings["hooks"].(map[string]any)
	ss = hooks["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Fatalf("expected only the framework hook to remain, got %d", len(ss))
	}
	if entryHasCommand(ss[0], ".claude/hooks/demo.sh") {
		t.Error("our hook should be gone")
	}
	if !entryHasCommand(ss[0], ".claude/hooks/pull-latest-main.sh") {
		t.Error("framework hook must survive")
	}
}

func TestRegisterHookCreatesSettingsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := registerHook(root, "UserPromptSubmit", ".claude/hooks/x.sh"); err != nil {
		t.Fatalf("registerHook on fresh workspace: %v", err)
	}
	settings := readSettings(t, root)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok || len(hooks["UserPromptSubmit"].([]any)) != 1 {
		t.Errorf("hook not created: %+v", settings)
	}
}

func TestUnregisterDropsEmptyEvent(t *testing.T) {
	root := t.TempDir()
	if err := registerHook(root, "PostToolUse", ".claude/hooks/only.sh"); err != nil {
		t.Fatal(err)
	}
	if err := unregisterHook(root, ".claude/hooks/only.sh"); err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, root)
	hooks, _ := settings["hooks"].(map[string]any)
	if _, present := hooks["PostToolUse"]; present {
		t.Errorf("emptied event key should be dropped: %+v", hooks)
	}
}

func TestUnregisterKeepsSiblingHooksInEntry(t *testing.T) {
	root := t.TempDir()
	// One entry containing two hooks; only one matches the command we remove.
	seed := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[
	  {"type":"command","command":".claude/hooks/ours.sh"},
	  {"type":"command","command":".claude/hooks/theirs.sh"}
	]}]}}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(root), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := unregisterHook(root, ".claude/hooks/ours.sh"); err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, root)
	entries := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entry should survive with its sibling hook, got %d entries", len(entries))
	}
	if entryHasCommand(entries[0], ".claude/hooks/ours.sh") {
		t.Error("our hook should be removed")
	}
	if !entryHasCommand(entries[0], ".claude/hooks/theirs.sh") {
		t.Error("the sibling hook in the same entry must be preserved")
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := registerHook(root, "SessionStart", ".claude/hooks/dup.sh"); err != nil {
			t.Fatal(err)
		}
	}
	settings := readSettings(t, root)
	entries := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(entries) != 1 {
		t.Errorf("re-registering the same command must not duplicate it, got %d entries", len(entries))
	}
}

func TestRegisterHookStoresCwdIndependentCommand(t *testing.T) {
	root := t.TempDir()
	if err := registerHook(root, "PreToolUse", ".claude/hooks/x.sh"); err != nil {
		t.Fatal(err)
	}
	got := firstCommand(t, root, "PreToolUse")
	want := "$CLAUDE_PROJECT_DIR/.claude/hooks/x.sh"
	if got != want {
		t.Errorf("stored command = %q, want %q", got, want)
	}
}

func TestUnregisterRemovesLegacyBareCommand(t *testing.T) {
	root := t.TempDir()
	// A hook installed by an older cg — stored in the bare relative form.
	seed := `{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":".claude/hooks/legacy.sh"}]}]}}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(root), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unregister is called with the bare relpath; it must still match the legacy entry.
	if err := unregisterHook(root, ".claude/hooks/legacy.sh"); err != nil {
		t.Fatal(err)
	}
	hooks, _ := readSettings(t, root)["hooks"].(map[string]any)
	if _, present := hooks["PreToolUse"]; present {
		t.Errorf("legacy bare-form hook should have been removed: %+v", hooks)
	}
}

func TestReregisterCollapsesLegacyBareForm(t *testing.T) {
	root := t.TempDir()
	// Legacy bare-form registration from an older cg.
	seed := `{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":".claude/hooks/dup.sh"}]}]}}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(root), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	// Re-applying the extension must replace (not duplicate) the legacy entry.
	if err := registerHook(root, "PreToolUse", ".claude/hooks/dup.sh"); err != nil {
		t.Fatal(err)
	}
	entries := readSettings(t, root)["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected the legacy entry to be replaced, got %d entries", len(entries))
	}
	if got := firstCommand(t, root, "PreToolUse"); got != "$CLAUDE_PROJECT_DIR/.claude/hooks/dup.sh" {
		t.Errorf("re-registration should store the prefixed form, got %q", got)
	}
}

func TestNormalizeHookCommands(t *testing.T) {
	root := t.TempDir()
	seed := `{
	  "statusLine": {"type":"command","command":".claude/statusline.sh"},
	  "hooks": {
	    "SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":".claude/hooks/pull.sh"}]}],
	    "PreToolUse":[{"matcher":"Bash","hooks":[
	      {"type":"command","command":"$CLAUDE_PROJECT_DIR/.claude/hooks/already.sh"},
	      {"type":"command","command":"/usr/local/bin/absolute.sh"},
	      {"type":"command","command":"echo hi"}
	    ]}]
	  }
	}`
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(root), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	// Dry run: reports the two bare paths, writes nothing.
	before, err := os.ReadFile(settingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NormalizeHookCommands(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("dry run should report 2 bare commands, got %d: %v", len(pending), pending)
	}
	if after, _ := os.ReadFile(settingsPath(root)); !bytes.Equal(after, before) {
		t.Error("dry run must not modify settings.json")
	}

	// Apply: rewrites the two bare paths only.
	got, err := NormalizeHookCommands(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("apply should rewrite 2 commands, got %d: %v", len(got), got)
	}
	settings := readSettings(t, root)
	if c := firstCommand(t, root, "SessionStart"); c != "$CLAUDE_PROJECT_DIR/.claude/hooks/pull.sh" {
		t.Errorf("SessionStart hook not pinned: %q", c)
	}
	if sl := settings["statusLine"].(map[string]any)["command"].(string); sl != "$CLAUDE_PROJECT_DIR/.claude/statusline.sh" {
		t.Errorf("statusLine not pinned: %q", sl)
	}
	pre := settings["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)
	cmds := map[string]bool{}
	for _, h := range pre {
		cmds[h.(map[string]any)["command"].(string)] = true
	}
	if !cmds["$CLAUDE_PROJECT_DIR/.claude/hooks/already.sh"] {
		t.Error("already-prefixed command must be left unchanged")
	}
	if !cmds["/usr/local/bin/absolute.sh"] {
		t.Error("absolute command must be left unchanged")
	}
	if !cmds["echo hi"] {
		t.Error("non-.claude command must be left unchanged")
	}

	// Idempotent: a second apply changes nothing.
	again, err := NormalizeHookCommands(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("second apply should be a no-op, got %v", again)
	}
}

func TestNormalizeHookCommandsMissingFile(t *testing.T) {
	root := t.TempDir()
	got, err := NormalizeHookCommands(root, true)
	if err != nil {
		t.Fatalf("missing settings.json must be a no-op, got err: %v", err)
	}
	if got != nil {
		t.Errorf("missing settings.json should return nil, got %v", got)
	}
}

// firstCommand returns the command string of the first hook registered under event.
func firstCommand(t *testing.T, root, event string) string {
	t.Helper()
	hooks := readSettings(t, root)["hooks"].(map[string]any)
	entry := hooks[event].([]any)[0].(map[string]any)
	inner := entry["hooks"].([]any)[0].(map[string]any)
	return inner["command"].(string)
}

func readSettings(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(settingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
