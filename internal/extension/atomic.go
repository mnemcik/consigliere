package extension

import (
	"io/fs"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path so that a concurrent reader sees either
// the previous content or the new content, never a partial file: data goes to a
// temp file in the same directory, which is then renamed over path. A failed
// write leaves the previous file intact.
//
// An existing file keeps its mode; a new file gets perm exactly (the umask is
// not applied, unlike os.WriteFile). An existing file without its owner write
// bit is refused, as writing through it would have been (a mode-bit check, not
// an access check). A symlink at path is
// followed, so its target is replaced and the link itself survives (a
// CLAUDE.md linked to AGENTS.md stays linked). The rename gives the file a new
// inode, so hard links to the previous file keep the previous content.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, false)
}

// writeFileAtomicMode is writeFileAtomic with mode always applied, existing
// file or not. The mode is set on the temp file before the rename, so an
// executable never appears without its executable bit.
func writeFileAtomicMode(path string, data []byte, mode os.FileMode) error {
	return writeAtomic(path, data, mode, true)
}

func writeAtomic(path string, data []byte, perm os.FileMode, force bool) error {
	target, err := resolveTarget(path)
	if err != nil {
		return err
	}
	mode := perm
	info, err := os.Stat(target)
	switch {
	case err == nil:
		if info.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "write", Path: target, Err: fs.ErrPermission}
		}
		if !force {
			mode = info.Mode().Perm()
		}
	case !os.IsNotExist(err):
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

// WriteFileAtomic is writeFileAtomic for callers outside the package, such as
// `cg extension resolve` writing a workspace file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeFileAtomic(path, data, perm)
}
