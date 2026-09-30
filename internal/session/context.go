// Package session implements the Claude Code hook bodies promoted from the
// personal-workspace shell hooks: the session-start gate, the dirty-flag
// marker, the pull-latest-main refresh, and the status-line renderer. Each is a
// deterministic, cross-platform port that reads the hook's stdin JSON and emits
// the hook's expected stdout, replacing jq/awk/stat/timeout shell plumbing.
package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	// Paused marks the badge of the session that paused its project
	// (cg session pause). Claiming a project clears it.
	Paused bool `json:"paused,omitempty"`
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

// sessionIDPattern is the allowlist for session IDs used as badge file names.
// Agents issue UUID-like IDs; anything else (separators, dots, drive or stream
// colons, spaces) could escape the directory or misbehave on some filesystem.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidSessionID reports whether id is safe to use as a badge file name: one or
// more ASCII letters, digits, underscores or hyphens.
func ValidSessionID(id string) bool {
	return sessionIDPattern.MatchString(id)
}

// WriteContext records the area and project for a session in its badge state
// file, creating the session-context directory and file when absent. Fields
// already present (dirty, or keys added by other tools) are preserved, so it
// is safe to call again when a session switches area or project. The pause
// marker is cleared.
func WriteContext(root, sessionID, area, project string) error {
	if !ValidSessionID(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	if err := os.MkdirAll(ContextDir(root), 0o755); err != nil {
		return err
	}
	path := ContextFile(root, sessionID)
	m, err := readContextMap(path)
	if errors.Is(err, fs.ErrNotExist) {
		m, err = map[string]any{}, nil
	}
	if err != nil {
		return err
	}
	m["area"] = area
	m["project"] = project
	// Claiming a project makes this session the active one on it, including a
	// session resuming its own pause (session IDs survive a resume).
	delete(m, "paused")
	delete(m, "pausedAt")
	if _, ok := m["dirty"]; !ok {
		m["dirty"] = false
	}
	return writeJSONAtomic(path, m)
}

// readContextMap loads a badge file as a generic map so writers can update
// their own fields and keep the rest. UseNumber keeps numeric fields written
// by other tools exact instead of round-tripping them through float64. A
// missing file returns an error satisfying errors.Is(err, fs.ErrNotExist).
func readContextMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
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
