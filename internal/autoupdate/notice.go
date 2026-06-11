package autoupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// PrintUpdatedNoticeIfAny prints a one-line "cg updated to vX" notice on the
// first run after the background worker installed an update, then clears the
// marker so it shows only once. Written to w (stderr by convention, so it never
// contaminates a command's stdout). Silenced by CONSIGLIERE_NO_UPDATE_NOTICE=1.
// Any read/parse error is swallowed — the notice is cosmetic.
func PrintUpdatedNoticeIfAny(w io.Writer) {
	if os.Getenv("CONSIGLIERE_NO_UPDATE_NOTICE") == "1" {
		return
	}
	raw, err := os.ReadFile(UpdatedMarkerPath())
	if err != nil {
		return
	}

	var m updatedMarker
	if err := json.Unmarshal(raw, &m); err != nil {
		// Corrupt marker — drop it so it doesn't resurface every run.
		_ = os.Remove(UpdatedMarkerPath())
		return
	}

	if m.ToVersion != "" {
		from := ""
		if m.FromVersion != "" {
			from = fmt.Sprintf(" (from %s)", m.FromVersion)
		}
		_, _ = fmt.Fprintf(w, "✅ cg updated to %s%s\n", m.ToVersion, from)
	}
	_ = os.Remove(UpdatedMarkerPath())
}

// PrintMajorNoticeIfAny emits a persistent, warn-only two-line notice when a
// major (breaking) release is available that the worker declined to auto-install.
// Unlike the updated notice it does NOT clear the marker — it keeps nudging
// until the user upgrades, ignores, or snoozes. Silenced by a future-dated
// snooze and by CONSIGLIERE_NO_UPDATE_NOTICE=1. Written to stderr.
func PrintMajorNoticeIfAny(w io.Writer) {
	if os.Getenv("CONSIGLIERE_NO_UPDATE_NOTICE") == "1" {
		return
	}
	m, ok := readMajorMarker()
	if !ok {
		return
	}
	if m.SnoozeUntil != "" {
		if until, err := time.Parse(time.RFC3339, m.SnoozeUntil); err == nil && time.Now().Before(until) {
			return
		}
	}
	_, _ = fmt.Fprintf(w, "⚠ cg %s is available (major release — review the changelog: %s)\n", m.ToVersion, m.ChangelogURL)
	_, _ = fmt.Fprintln(w, "  Upgrade with: cg update upgrade   (or: cg update snooze --major / cg update ignore --major)")
}
