//go:build windows

package autoupdate

// processAlive is conservative on Windows: probing an arbitrary PID's liveness
// portably is awkward, so we report "alive" and rely on the lockfile-mtime
// staleness check (lockMaxAge) to recover from a crashed worker's stale lock.
func processAlive(_ int) bool {
	return true
}
