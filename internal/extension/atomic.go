package extension

import (
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path so that a concurrent reader sees either
// the previous content or the new content, never a partial file: data goes to a
// temp file in the same directory, which is then renamed over path. A failed
// write leaves the previous file intact.
//
// Like os.WriteFile, perm applies only when path does not exist; an existing
// file keeps its mode. A symlink at path is followed, so its target is replaced
// and the link itself survives (a CLAUDE.md linked to AGENTS.md stays linked).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	target, err := resolveTarget(path)
	if err != nil {
		return err
	}
	mode := perm
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return replaceFile(target, data, mode)
}

// writeFileAtomicMode is writeFileAtomic with mode always applied, existing
// file or not. The mode is set on the temp file before the rename, so an
// executable never appears without its executable bit.
func writeFileAtomicMode(path string, data []byte, mode os.FileMode) error {
	target, err := resolveTarget(path)
	if err != nil {
		return err
	}
	return replaceFile(target, data, mode)
}

// resolveTarget follows a symlink at path to the file it names. A missing path,
// or a dangling link, resolves to path itself.
func resolveTarget(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if os.IsNotExist(err) {
		return path, nil
	}
	return "", err
}

// replaceFile writes data to a temp file beside target, syncs it, and renames
// it over target, creating target's directory if needed.
func replaceFile(target string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // workspace dir
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".cg-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(e error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return e
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
