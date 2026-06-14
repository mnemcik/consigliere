package extension

import (
	"os"
	"path/filepath"
)

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
	return filepath.Join(base, "consigliere")
}

// ExtensionsDir is the machine-shared root holding cloned extension sources:
// ~/.config/consigliere/extensions/. One clone may back several workspaces; what
// each workspace actually received is tracked per-workspace in its ledger.
func ExtensionsDir() string {
	return filepath.Join(configBase(), "extensions")
}

// CloneDir is the install directory for the named extension.
func CloneDir(name string) string {
	return filepath.Join(ExtensionsDir(), name)
}

// StagingDir is the transient clone target used before the manifest is read:
// install clones here, validates, then renames to CloneDir(name) (the name is
// only known after the manifest is parsed). A deterministic, dot-prefixed name
// keeps it out of the way of real extensions and avoids any randomness.
func StagingDir() string {
	return filepath.Join(ExtensionsDir(), ".staging")
}
