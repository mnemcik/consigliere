package autoupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// debounceWindow throttles the background check to at most once per window.
	debounceWindow = 24 * time.Hour
	// workerTimeout bounds the whole detached run so a hung network call can't
	// hold the lock forever.
	workerTimeout = 5 * time.Minute
	// lockMaxAge is a cross-platform safety net: a lockfile older than this is
	// treated as stale even if we can't probe the owning PID (e.g. on Windows).
	lockMaxAge = 1 * time.Hour
)

// updatedMarker is written after a successful background install and consumed
// (printed + cleared) by PrintUpdatedNoticeIfAny on the next cg run.
type updatedMarker struct {
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
	UpdatedAt   string `json:"updatedAt"`
}

// RunWorker is the detached background entrypoint (see Bootstrap). It runs with
// stdio closed, so every step logs-and-swallows rather than surfacing errors.
// It self-throttles via a debounce stamp and a pidfile lock, then checks for a
// newer release and, for install.sh-managed binaries, installs it in place.
func RunWorker(currentVersion string) {
	defer func() {
		if r := recover(); r != nil {
			logError("worker panic", fmt.Errorf("%v", r))
		}
	}()

	if !IsReleaseVersion(currentVersion) {
		return // dev/snapshot build — nothing to update to
	}
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		logError("mkdir state dir", err)
		return
	}
	if !debounceElapsed() {
		return
	}
	if !acquireLock() {
		return
	}
	defer releaseLock()

	doCheckAndInstall(currentVersion)
}

func doCheckAndInstall(currentVersion string) {
	defer writeDebounceStamp()

	ctx, cancel := context.WithTimeout(context.Background(), workerTimeout)
	defer cancel()

	repo := Repo()
	latest, err := LatestVersion(ctx, repo)
	if err != nil {
		logError("check latest release", err)
		return
	}
	if !IsNewer(currentVersion, latest) {
		ClearStaleMajorMarker(currentVersion)
		return
	}

	// Only install.sh-managed binaries are self-replaced; brew/unknown installs
	// are left to their package manager (DEC-011).
	m := DetectManagement()
	if !m.SelfManaged {
		return
	}

	// Major (breaking) bumps are never auto-installed: record a warn-only
	// marker instead, so the next cg run nudges the user to upgrade explicitly
	// via `cg update upgrade` (DEC — major gate).
	if IsMajorBump(currentVersion, latest) {
		if err := handleMajorAvailable(repo, Normalize(currentVersion), latest); err != nil {
			logError("write major-available marker", err)
		}
		return
	}

	if err := InstallRelease(ctx, repo, latest, m.BinaryPath); err != nil {
		logError("install "+latest, err)
		return
	}
	if err := RefreshInstalledState(latest, nowISO()); err != nil {
		logError("refresh installed.json", err)
	}
	// A minor/patch that moved past a previously-flagged major clears the marker.
	ClearStaleMajorMarker(latest)
	writeUpdatedMarker(Normalize(currentVersion), latest)
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// --- debounce ---------------------------------------------------------------

func debounceElapsed() bool {
	raw, err := os.ReadFile(LastCheckPath())
	if err != nil {
		return true // no prior check recorded
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return true // corrupt stamp — treat as elapsed
	}
	return time.Since(time.UnixMilli(ms)) >= debounceWindow
}

func writeDebounceStamp() {
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := os.WriteFile(LastCheckPath(), []byte(stamp), 0o644); err != nil {
		logError("write debounce stamp", err)
	}
}

// --- pidfile lock -----------------------------------------------------------

func acquireLock() bool {
	if writeLock() {
		return true
	}
	// Lock exists — take it over only if the owner is gone or it's stale.
	if lockIsStale() {
		if err := os.Remove(LockPath()); err != nil && !os.IsNotExist(err) {
			logError("remove stale lock", err)
			return false
		}
		return writeLock()
	}
	return false
}

func writeLock() bool {
	f, err := os.OpenFile(LockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			logError("create lock", err)
		}
		return false
	}
	_, _ = fmt.Fprintf(f, "%d", os.Getpid())
	_ = f.Close()
	return true
}

func lockIsStale() bool {
	info, err := os.Stat(LockPath())
	if err != nil {
		return true // vanished between checks — treat as free
	}
	if time.Since(info.ModTime()) > lockMaxAge {
		return true
	}
	raw, err := os.ReadFile(LockPath())
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return true // corrupt content
	}
	return !processAlive(pid)
}

func releaseLock() {
	if err := os.Remove(LockPath()); err != nil && !os.IsNotExist(err) {
		logError("release lock", err)
	}
}

// --- markers + error log ----------------------------------------------------

func writeUpdatedMarker(from, to string) {
	raw, err := json.MarshalIndent(updatedMarker{FromVersion: from, ToVersion: to, UpdatedAt: nowISO()}, "", "  ")
	if err != nil {
		logError("marshal updated marker", err)
		return
	}
	if err := os.WriteFile(UpdatedMarkerPath(), append(raw, '\n'), 0o644); err != nil {
		logError("write updated marker", err)
	}
}

func logError(msg string, err error) {
	line := fmt.Sprintf("[%s] %s: %v\n", nowISO(), msg, err)
	f, e := os.OpenFile(ErrorLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if e != nil {
		return // best-effort; the log itself must never break the worker
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}
