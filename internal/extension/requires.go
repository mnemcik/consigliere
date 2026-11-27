package extension

import (
	"fmt"
	"regexp"

	"golang.org/x/mod/semver"
)

// UpdateHint tells the user how to get a newer cg. It ends every message that
// refuses an extension because this cg is too old.
const UpdateHint = "Update cg (cg update upgrade, or brew upgrade --cask cg), then run this again."

// versionRe is the form of requires.cg: MAJOR, MAJOR.MINOR or
// MAJOR.MINOR.PATCH, no leading "v", no leading zeros, no pre-release or build
// suffix. The registry schema uses the same pattern (plus a guard against
// whitespace, which Python's re lets through before a final newline; Go's $
// without the m flag matches only at the very end).
var versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*)){0,2}$`)

// runningRe matches a running cg version: an optional "v", MAJOR.MINOR.PATCH
// in the requirement's part form, then the end, a pre-release ("-") or a build
// suffix ("+"). `git describe` builds (v2.1.0-4-gabc1234-dirty) and GoReleaser
// builds (2.1.0) both match.
var runningRe = regexp.MustCompile(`^v?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))(?:[-+].*)?$`)

// RunningCore returns the MAJOR.MINOR.PATCH of the running cg version v, and
// false when v has none (a development build, a bare commit hash, empty): then
// no requirement is checked.
func RunningCore(v string) (string, bool) {
	sm := runningRe.FindStringSubmatch(v)
	if sm == nil {
		return "", false
	}
	return sm[1], true
}

// UnmetRequirement reports whether m's requires.cg is greater than the
// running cg version, and returns the requirement and the running core. A
// manifest without requires.cg, or a running version with no core, never has
// an unmet requirement. m must have passed Validate.
func UnmetRequirement(m *Manifest, running string) (need, have string, unmet bool) {
	if m == nil || m.Requires == nil || m.Requires.CG == nil {
		return "", "", false
	}
	core, ok := RunningCore(running)
	if !ok {
		return "", "", false
	}
	need = *m.Requires.CG
	// semver.Compare reads "v2" and "v2.1" as v2.0.0 and v2.1.0.
	if semver.Compare("v"+need, "v"+core) > 0 {
		return need, core, true
	}
	return "", "", false
}

// RequirementMessage says that extension name needs cg need or newer while
// this is cg have, and how to update.
func RequirementMessage(name, need, have string) string {
	return fmt.Sprintf("extension %q %s. %s", name, RequirementReason(need, have), UpdateHint)
}

// RequirementReason is the requirement message without the extension's name
// or the update hint: "needs cg <need> or newer; this is cg <have>".
func RequirementReason(need, have string) string {
	return fmt.Sprintf("needs cg %s or newer; this is cg %s", need, have)
}
