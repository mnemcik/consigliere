// Package session implements the Claude Code hook bodies promoted from the
// personal-workspace shell hooks: the session-start gate, the dirty-flag
// marker, the pull-latest-main refresh, and the status-line renderer. Each is a
// deterministic, cross-platform port that reads the hook's stdin JSON and emits
// the hook's expected stdout, replacing jq/awk/stat/timeout shell plumbing.
package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ContextDirName is the per-session badge state directory, relative to the
// workspace root.
var ContextDirName = filepath.Join(".claude", "session-context")

// ContextDir returns the session-context directory under the workspace root.
func ContextDir(root string) string { return filepath.Join(root, ContextDirName) }

// ContextFile returns the badge state file path for a session.
func ContextFile(root, sessionID string) string {
	return filepath.Join(ContextDir(root), sessionID+".json")
}

// Context is the per-session badge state the status line renders.
type Context struct {
	Area    string `json:"area"`
	Project string `json:"project"`
	Dirty   bool   `json:"dirty"`
}

// ReadContext loads the badge state for a session. It returns (nil, nil) when
// the file does not exist (tracking is opt-in — no file means no badge).
func ReadContext(root, sessionID string) (*Context, error) {
	data, err := os.ReadFile(ContextFile(root, sessionID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Context
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// IsContextPath reports whether p targets the session-context directory (or a
// file within it) for the given root. Writes there are framework bookkeeping,
// not user work, so the dirty-marker ignores them.
func IsContextPath(root, p string) bool {
	if p == "" {
		return false
	}
	dir := ContextDir(root)
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	return abs == dir || strings.HasPrefix(abs, dir+string(filepath.Separator))
}

// writeJSONAtomic writes v as indented JSON to path via a temp file + rename so
// a concurrent reader never sees a partial file.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cg-ctx-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
