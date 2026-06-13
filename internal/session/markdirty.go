package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

// MarkDirty sets "dirty": true in the session's badge state file, preserving any
// other fields already present. It is a no-op (returns nil) when the file does
// not exist — tracking is opt-in, so the flag never appears for untracked
// sessions. Mirrors the `jq '. + {dirty: true}'` behavior of the shell hook.
func MarkDirty(root, sessionID string) error {
	path := ContextFile(root, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m == nil {
		m = map[string]any{}
	}
	if dirty, ok := m["dirty"].(bool); ok && dirty {
		return nil // already dirty; avoid a needless rewrite
	}
	m["dirty"] = true
	return writeJSONAtomic(path, m)
}
