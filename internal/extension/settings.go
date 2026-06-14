package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Claude Code settings.json hook schema (the subset contributions touch):
//
//	{ "hooks": { "<Event>": [ { "matcher": "...",
//	    "hooks": [ {"type":"command","command":"..."} ] } ] }, ... }
//
// We parse into a generic map so fields cg doesn't manage (statusLine, the
// framework's own hooks, user additions) are preserved across register/
// unregister. json.Marshal sorts map keys, so output is deterministic.

func settingsPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(settingsRel))
}

// registerHook appends a command hook for event, pointing at commandRel. It is
// idempotent: any prior registration of the same command (in any event) is
// removed first, so re-applying an extension can't accumulate duplicate hooks.
func registerHook(root, event, commandRel string) error {
	settings, err := loadSettings(settingsPath(root))
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	removeHookCommand(hooks, commandRel)
	entry := map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{"type": "command", "command": commandRel},
		},
	}
	existing, _ := hooks[event].([]any)
	hooks[event] = append(existing, entry)
	return saveSettings(settingsPath(root), settings)
}

// unregisterHook removes the command hook invoking commandRel wherever it
// appears, across all events. Missing file or absent hook is a no-op.
func unregisterHook(root, commandRel string) error {
	settings, err := loadSettings(settingsPath(root))
	if err != nil {
		return err
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	if !removeHookCommand(hooks, commandRel) {
		return nil
	}
	return saveSettings(settingsPath(root), settings)
}

// removeHookCommand strips the command hook invoking commandRel from every event
// entry, keeping any sibling hooks in the same entry. An entry whose inner hooks
// all drop is removed; an event whose entries all drop is deleted. Returns
// whether anything changed.
func removeHookCommand(hooks map[string]any, commandRel string) bool {
	changed := false
	for event, v := range hooks {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		keptEntries := make([]any, 0, len(arr))
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				keptEntries = append(keptEntries, item)
				continue
			}
			inner, ok := m["hooks"].([]any)
			if !ok {
				keptEntries = append(keptEntries, item)
				continue
			}
			keptInner := make([]any, 0, len(inner))
			for _, h := range inner {
				if hookHasCommand(h, commandRel) {
					changed = true
					continue
				}
				keptInner = append(keptInner, h)
			}
			if len(keptInner) == 0 {
				// Whole entry's hooks removed — drop the entry.
				continue
			}
			m["hooks"] = keptInner
			keptEntries = append(keptEntries, m)
		}
		if len(keptEntries) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keptEntries
		}
	}
	return changed
}

// hookHasCommand reports whether an inner hook object invokes commandRel.
func hookHasCommand(h any, commandRel string) bool {
	hm, ok := h.(map[string]any)
	if !ok {
		return false
	}
	cmd, ok := hm["command"].(string)
	return ok && cmd == commandRel
}

// entryHasCommand reports whether a hooks-array entry contains a command hook
// invoking commandRel.
func entryHasCommand(item any, commandRel string) bool {
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		if hookHasCommand(h, commandRel) {
			return true
		}
	}
	return false
}

// loadSettings reads settings.json into a generic map. A missing file yields an
// empty map (not an error) so registration works on a workspace that has none.
func loadSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func saveSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // workspace dir
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // workspace settings
}
