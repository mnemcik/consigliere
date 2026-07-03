package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code settings.json hook schema (the subset contributions touch):
//
//	{ "hooks": { "<Event>": [ { "matcher": "...",
//	    "hooks": [ {"type":"command","command":"..."} ] } ] }, ... }
//
// We parse into a generic map so fields cg doesn't manage (statusLine, the
// framework's own hooks, user additions) are preserved across register/
// unregister. json.Marshal sorts map keys, so output is deterministic.

// claudeProjectDirPrefix makes a workspace-relative command path cwd-independent.
// Claude Code runs hook and statusLine commands in the tool's working directory,
// which drifts as a Bash command `cd`s into a subdirectory. A bare relative path
// like ".claude/hooks/x.sh" then resolves against the wrong directory and fails
// with "No such file or directory". Claude Code expands $CLAUDE_PROJECT_DIR to
// the workspace root at run time, so prefixing it pins the path regardless of cwd.
const claudeProjectDirPrefix = "$CLAUDE_PROJECT_DIR/"

// relHookPrefix identifies the framework-relative command paths cg manages. Only
// commands beginning with it are normalized/prefixed; absolute paths, already
// prefixed commands, and unrelated commands are left untouched.
const relHookPrefix = ".claude/"

func settingsPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(settingsRel))
}

// PinnedCommand renders a workspace-relative hook/statusLine path as a
// cwd-independent settings.json command string, prefixing $CLAUDE_PROJECT_DIR
// (see claudeProjectDirPrefix). filepath.ToSlash normalizes a filesystem path
// and is a no-op on an already-slashed command string, so the one helper serves
// both registration (an FS-joined path) and normalization/reporting (an existing
// command). It is the single source of the pinned form.
func PinnedCommand(path string) string {
	return claudeProjectDirPrefix + filepath.ToSlash(path)
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
			map[string]any{"type": "command", "command": PinnedCommand(commandRel)},
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

// hookHasCommand reports whether an inner hook object invokes commandRel. It
// matches both the bare relative form (".claude/...") and the cwd-independent
// prefixed form ("$CLAUDE_PROJECT_DIR/.claude/...") so a hook registered by an
// older cg (bare) is still found for dedupe and uninstall after the upgrade.
func hookHasCommand(h any, commandRel string) bool {
	hm, ok := h.(map[string]any)
	if !ok {
		return false
	}
	cmd, ok := hm["command"].(string)
	return ok && (cmd == commandRel || cmd == PinnedCommand(commandRel))
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

// NormalizeHookCommands rewrites bare framework-relative hook and statusLine
// command paths in the workspace settings.json to the cwd-independent
// $CLAUDE_PROJECT_DIR form (see claudeProjectDirPrefix). settings.json is
// user-owned and never regenerated by `cg init --force`, so this is how an
// existing workspace picks up the path fix — `cg sync` calls it. It returns the
// original (pre-rewrite) commands it changed, most useful for reporting.
//
// It is idempotent and conservative: only commands beginning with ".claude/"
// are touched; already-prefixed, absolute, and unrelated commands are left as
// is. When apply is false, nothing is written — the returned slice is exactly
// what an apply would change (the dry-run view). A missing settings.json is a
// no-op (nil, nil).
func NormalizeHookCommands(root string, apply bool) ([]string, error) {
	path := settingsPath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	settings, err := loadSettings(path)
	if err != nil {
		return nil, err
	}

	var rewritten []string
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		for _, v := range hooks {
			arr, ok := v.([]any)
			if !ok {
				continue
			}
			for _, item := range arr {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				inner, ok := m["hooks"].([]any)
				if !ok {
					continue
				}
				for _, h := range inner {
					if hm, ok := h.(map[string]any); ok {
						if old, did := normalizeCommandField(hm); did {
							rewritten = append(rewritten, old)
						}
					}
				}
			}
		}
	}
	if sl, ok := settings["statusLine"].(map[string]any); ok {
		if old, did := normalizeCommandField(sl); did {
			rewritten = append(rewritten, old)
		}
	}

	if len(rewritten) == 0 {
		return nil, nil
	}
	if apply {
		if err := saveSettings(path, settings); err != nil {
			return nil, err
		}
	}
	return rewritten, nil
}

// normalizeCommandField rewrites m["command"] in place when it is a bare
// framework-relative path, prefixing $CLAUDE_PROJECT_DIR. It returns the
// original command and whether it changed. Non-string, absent, already-prefixed,
// absolute, and non-".claude/" commands are left untouched.
func normalizeCommandField(m map[string]any) (old string, changed bool) {
	cmd, ok := m["command"].(string)
	if !ok || !strings.HasPrefix(cmd, relHookPrefix) {
		return "", false
	}
	m["command"] = PinnedCommand(cmd)
	return cmd, true
}
