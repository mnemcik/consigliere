package extension

import (
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
