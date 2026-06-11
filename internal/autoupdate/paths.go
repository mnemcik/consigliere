// Package autoupdate implements cg's binary self-update subsystem: discovering
// newer releases on GitHub, replacing the running binary in place, a detached
// background freshness check, and a warn-only gate for major (breaking) bumps.
//
// Scope: this is the *binary* side of upgrades — swapping the `cg` executable
// itself. It is distinct from `cg sync` (internal/sync, internal/manifest),
// which reconciles workspace *content* (CLAUDE.md sections + framework notes).
// `cg sync` = content; `cg update` = binary.
//
// The package never imports cmd. The currently-running version is threaded in
// as a parameter by callers (cmd/update.go, the root wiring) so this package
// stays pure and testable.
package autoupdate

import (
	"os"
	"path/filepath"
)

// StateDir returns the directory holding every auto-update state file. It
// honors XDG_CONFIG_HOME (falling back to ~/.config), matching the path
// install.sh writes installed.json to so both sides share one source of truth.
func StateDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Best-effort: callers create the dir lazily and tolerate failure.
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "consigliere")
}

// LastCheckPath is the unix-millis debounce stamp the background worker writes
// to throttle itself to one check per window.
func LastCheckPath() string { return filepath.Join(StateDir(), "last-update-check") }

// LockPath is the worker's pidfile, guarding against concurrent workers.
func LockPath() string { return filepath.Join(StateDir(), "autoupdate.lock") }

// UpdatedMarkerPath is the one-shot "✅ Updated to vX" marker the worker writes
// after a successful background install; the next cg run prints and clears it.
func UpdatedMarkerPath() string { return filepath.Join(StateDir(), "updated.json") }

// MajorAvailablePath is the persistent warn-only marker for an available major
// (breaking) release the worker won't auto-install.
func MajorAvailablePath() string { return filepath.Join(StateDir(), "major-available.json") }

// MajorIgnoredPath records major versions the user has permanently dismissed.
func MajorIgnoredPath() string { return filepath.Join(StateDir(), "major-ignored.json") }

// ErrorLogPath is the append-only best-effort log for detached-worker failures.
func ErrorLogPath() string { return filepath.Join(StateDir(), "autoupdate.log") }

// InstalledStatePath is the file install.sh writes (version, tag, method, …)
// and the updater refreshes after a successful self-update.
func InstalledStatePath() string { return filepath.Join(StateDir(), "installed.json") }
