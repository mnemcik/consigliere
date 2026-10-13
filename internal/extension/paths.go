package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CleanSubdir validates and normalises a monorepo subdir — the directory within
// a cloned extension repo that holds cg-extension.json and its payload files. An
// empty path means the repo root (the single-extension-per-repo default). The
// result is cleaned to the host separator and rejected if it is absolute or
// escapes the repo via "..", so a registry entry or a hand-edited .cg.json can
// never point the install/apply machinery outside the clone.
func CleanSubdir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	// Either separator works on every OS: `..\x` is rejected on Unix too, and a
	// subdir recorded in .cg.json on Windows still resolves on Unix. (Manifest
	// src/dest/script paths are only validated here; the code that uses them
	// reads them in slash form.)
	slash := strings.ReplaceAll(p, `\`, "/")
	cleaned := filepath.Clean(filepath.FromSlash(slash))
	// "." cleans to the repo root, which the rest of the flow represents as the
	// empty sentinel; returning "." would wrongly look like subdir mode.
	if cleaned == "." {
		return "", nil
	}
	if rooted(slash) || filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid subdir %q: must be a relative path inside the repo", p)
	}
	return cleaned, nil
}

// rooted reports whether p, in slash form, starts at a root or names a drive,
// whatever the OS, so a manifest is valid or invalid the same way everywhere.
// filepath.IsAbs alone differs: on Windows "/etc/passwd" isn't absolute (it has
// no drive) and "C:x" is relative to drive C's current directory, and on Unix
// "C:x" is a plain relative name.
func rooted(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 2 && p[1] == ':' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

// configBase returns cg's config root. It mirrors internal/autoupdate.StateDir:
// honor $XDG_CONFIG_HOME, else ~/.config, with a temp-dir fallback when the home
// directory can't be resolved. Extensions deliberately share the single
// ~/.config/consigliere root with the auto-update state rather than introducing
// a second ~/.config/cg tree (project DEC-002, 2026-06-14).
func configBase() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".config")
	}
	// git runs some store commands from inside the mirror, so a relative
	// XDG_CONFIG_HOME must not stay relative.
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	return filepath.Join(base, "consigliere")
}

// ExtensionsDir is the machine-shared root holding cloned extension sources:
// ~/.config/consigliere/extensions/. One clone may back several workspaces; what
// each workspace actually received is tracked per-workspace in its ledger.
func ExtensionsDir() string {
	return filepath.Join(configBase(), "extensions")
}

// CloneDir is the legacy, name-keyed install directory, read only for entries
// recorded before the store (see store.go). name must be a
// validated extension name (matching the manifest's nameRe — [a-z0-9-]+); the
// install flow validates m.Name via Manifest.Validate before calling this, so
// name never contains path separators or "..".
func CloneDir(name string) string {
	return filepath.Join(ExtensionsDir(), name)
}
