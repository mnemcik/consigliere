package session

import (
	"errors"
	"io/fs"
)

// MarkDirty sets "dirty": true in the session's badge state file, preserving any
// other fields already present. It is a no-op (returns nil) when the file does
// not exist — tracking is opt-in, so the flag never appears for untracked
// sessions. Mirrors the `jq '. + {dirty: true}'` behavior of the shell hook.
func MarkDirty(root, sessionID string) error {
	if !ValidSessionID(sessionID) {
		return nil // hook input; an unsafe ID never names a badge
	}
	return withBadgeLock(root, sessionID, func() error { return markDirty(root, sessionID) })
}

func markDirty(root, sessionID string) error {
	path := ContextFile(root, sessionID)
	m, err := readContextMap(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if dirty, ok := m[keyDirty].(bool); ok && dirty {
		return nil // already dirty; avoid a needless rewrite
	}
	m[keyDirty] = true
	return writeJSONAtomic(path, m)
}
