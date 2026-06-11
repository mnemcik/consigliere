package autoupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
