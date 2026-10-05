package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SetVersion rewrites the top-level "version" value in dir's .cg.json and
// leaves every other byte alone.
//
// It deliberately does not go through Config.Save. Save re-marshals the typed
// struct, so it drops any key this binary does not model: a block a newer cg
// added, an extra field inside a known one. cg sync writes the version on every
// upgrade, so a shared, committed .cg.json would lose that content the first
// time an older binary synced it. Patching the one value in place also keeps
// the file's key order, formatting and escaping, so the diff is one line.
func SetVersion(dir, version string) error {
	path := filepath.Join(dir, ConfigFile)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	// A symlinked .cg.json would have the write land outside the workspace.
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; not writing through it", ConfigFile)
	}
	data, err := os.ReadFile(path) //nolint:gosec // ConfigFile is a fixed name under dir
	if err != nil {
		return err
	}
	out, err := setTopLevelString(data, "version", version)
	if err != nil {
		return fmt.Errorf("%s: %w", ConfigFile, err)
	}
	return replaceFile(path, out, info.Mode().Perm())
}

// replaceFile writes data next to path and renames it into place, so a failed
// write leaves the original intact instead of truncated. The rename replaces
// the directory entry itself and never follows a link.
func replaceFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func(e error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return e
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return cleanup(err)
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// setTopLevelString replaces the value of key in the top-level JSON object
// with the string val. It walks the token stream so a nested key of the same
// name (an extension's "version") is never touched, and fails rather than
// guessing when the key is missing or not a string.
func setTopLevelString(data []byte, key, val string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	found := false
	var start, end int64
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		keyEnd := dec.InputOffset() // just after the key
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if name != key {
			continue
		}
		// encoding/json keeps the last of duplicate keys, so patching one of
		// several would leave the value Detect reads unchanged.
		if found {
			return nil, fmt.Errorf("duplicate top-level %q keys", key)
		}
		if len(raw) == 0 || raw[0] != '"' {
			return nil, fmt.Errorf("%q is not a string", key)
		}
		found, start, end = true, keyEnd, dec.InputOffset()
	}
	if _, err := dec.Token(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no top-level %q key", key)
	}
	// The span holds `: "old"` plus whatever whitespace precedes the value;
	// keep everything up to the opening quote.
	q := bytes.IndexByte(data[start:end], '"')
	enc, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(data)+len(enc))
	out = append(out, data[:start+int64(q)]...)
	out = append(out, enc...)
	out = append(out, data[end:]...)
	return out, nil
}
