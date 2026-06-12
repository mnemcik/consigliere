package autoupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/mod/semver"
)

// MajorSnoozeDuration is how long `cg update snooze --major` silences the notice.
const MajorSnoozeDuration = 7 * 24 * time.Hour

// majorMarker records an available major (breaking) release the worker won't
// auto-install. It persists until the user upgrades, ignores it, or installs a
// version at/past the target major. SnoozeUntil (RFC3339) silences the notice
// for a while without dismissing it.
type majorMarker struct {
	FromVersion  string `json:"fromVersion"`
	ToVersion    string `json:"toVersion"`
	ChangelogURL string `json:"changelogUrl"`
	SnoozeUntil  string `json:"snoozeUntil,omitempty"`
}

type ignoredList struct {
	Ignored []string `json:"ignored"`
}

// IsMajorBump reports whether to is a higher major version than from.
func IsMajorBump(from, to string) bool {
	mf, mt := semver.Major(Normalize(from)), semver.Major(Normalize(to))
	if mf == "" || mt == "" {
		return false
	}
	return semver.Compare(mt, mf) > 0
}

// readMajorMarker returns the pending major marker, or (zero, false) if none /
// unreadable. Callers treat absence as "no pending major".
func readMajorMarker() (majorMarker, bool) {
	raw, err := os.ReadFile(MajorAvailablePath())
	if err != nil {
		return majorMarker{}, false
	}
	var m majorMarker
	if err := json.Unmarshal(raw, &m); err != nil {
		return majorMarker{}, false
	}
	return m, m.ToVersion != ""
}

func writeMajorMarker(m majorMarker) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MajorAvailablePath(), append(raw, '\n'), 0o600)
}

func clearMajorMarker() error {
	if err := os.Remove(MajorAvailablePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readIgnored() ignoredList {
	var l ignoredList
	raw, err := os.ReadFile(MajorIgnoredPath())
	if err != nil {
		return l
	}
	_ = json.Unmarshal(raw, &l)
	return l
}

func isIgnored(version string) bool {
	for _, v := range readIgnored().Ignored {
		if v == version {
			return true
		}
	}
	return false
}

// changelogURL builds the GitHub release page URL for a version tag.
func changelogURL(repo, version string) string {
	return fmt.Sprintf("%s/%s/releases/tag/%s", downloadBase(), repo, Normalize(version))
}

// handleMajorAvailable records (or refreshes) the major marker. It skips
// versions the user permanently ignored and preserves an existing snooze for
// the same target so a background re-check doesn't reset it.
func handleMajorAvailable(repo, from, to string) error {
	to = Normalize(to) // keep the ignore list + marker in one consistent format
	if isIgnored(to) {
		return nil
	}
	snooze := ""
	if existing, ok := readMajorMarker(); ok && existing.ToVersion == to {
		snooze = existing.SnoozeUntil
	}
	return writeMajorMarker(majorMarker{
		FromVersion:  from,
		ToVersion:    to,
		ChangelogURL: changelogURL(repo, to),
		SnoozeUntil:  snooze,
	})
}

// ClearStaleMajorMarker removes the marker once the installed version's major
// has reached or passed the marker's target — e.g. after the user upgraded,
// or a newer non-major release moved past the boundary.
func ClearStaleMajorMarker(currentVersion string) {
	m, ok := readMajorMarker()
	if !ok {
		return
	}
	cur, tgt := semver.Major(Normalize(currentVersion)), semver.Major(Normalize(m.ToVersion))
	if cur == "" || tgt == "" {
		return
	}
	if semver.Compare(cur, tgt) >= 0 {
		_ = clearMajorMarker()
	}
}

// addIgnored appends version to the permanent-ignore list (idempotent).
func addIgnored(version string) error {
	l := readIgnored()
	for _, v := range l.Ignored {
		if v == version {
			return writeIgnored(l) // already present
		}
	}
	l.Ignored = append(l.Ignored, version)
	return writeIgnored(l)
}

func writeIgnored(l ignoredList) error {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MajorIgnoredPath(), append(raw, '\n'), 0o600)
}

// PendingMajor returns the pending major target version (normalized), if any.
func PendingMajor() (string, bool) {
	m, ok := readMajorMarker()
	return m.ToVersion, ok
}

// SnoozeMajor silences the pending major notice for d. Returns the target
// version and whether a major was actually pending.
func SnoozeMajor(d time.Duration) (toVersion string, pending bool, err error) {
	m, ok := readMajorMarker()
	if !ok {
		return "", false, nil
	}
	m.SnoozeUntil = time.Now().Add(d).Format(time.RFC3339)
	return m.ToVersion, true, writeMajorMarker(m)
}

// IgnoreMajor permanently dismisses the pending major (it won't re-arm until a
// newer major appears) and clears the marker. Returns the target version and
// whether a major was pending.
func IgnoreMajor() (toVersion string, pending bool, err error) {
	m, ok := readMajorMarker()
	if !ok {
		return "", false, nil
	}
	if err := addIgnored(m.ToVersion); err != nil {
		return m.ToVersion, true, err
	}
	return m.ToVersion, true, clearMajorMarker()
}
