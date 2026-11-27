package extension

import (
	"fmt"
	"strings"
	"testing"
)

// requiresManifest is a valid manifest of schema version v whose "requires"
// member is the raw JSON requires ("" leaves it out).
func requiresManifest(v int, requires string) string {
	r := ""
	if requires != "" {
		r = `"requires": ` + requires + `, `
	}
	return fmt.Sprintf(`{"manifest": %d, "name": "a", "version": "1.0.0", "description": "d", %s"contributes": {}}`, v, r)
}

func parseValid(t *testing.T, data string) (*Manifest, error) {
	t.Helper()
	m, err := ParseManifest([]byte(data))
	if err != nil {
		t.Fatalf("ParseManifest(%s): %v", data, err)
	}
	return m, m.Validate()
}

// validVersionSamples and invalidVersionSamples are the requires.cg values
// of #212 S1 and S2; the registry schema's tests use the same samples.
var (
	validVersionSamples   = []string{"0", "2", "2.1", "2.1.0", "10.20.30", "0.0.0", "2.0.1"}
	invalidVersionSamples = []string{"", "v2.1.0", "v2", "2.1.0-rc.1", "2.1.0+build", "02.1", "02", "2.01", "2.1.00",
		"2.1.0.0", "2.x", "x", " 2.1.0", "2.1.0 ", "2.1.0\n", "2.1\t", "2.\x1b1", "2.", ".2", "2..1", "-1", "+2"}
)

// #212 S1: requires.cg in the version pattern validates, in v1 and v2.
func TestRequiresValid(t *testing.T) {
	for _, v := range []int{1, 2} {
		for _, s := range validVersionSamples {
			m, err := parseValid(t, requiresManifest(v, `{"cg": "`+s+`"}`))
			if err != nil {
				t.Errorf("v%d requires.cg %q: %v", v, s, err)
				continue
			}
			if m.Requires == nil || m.Requires.CG == nil || *m.Requires.CG != s {
				t.Errorf("v%d requires.cg %q parsed as %+v", v, s, m.Requires)
			}
		}
	}
}

// #212 S2: any other requires.cg fails, naming the value quoted.
func TestRequiresInvalid(t *testing.T) {
	for _, v := range []int{1, 2} {
		for _, s := range invalidVersionSamples {
			cg := s
			m := Manifest{Manifest: v, Name: "a", Version: "1", Description: "d", Requires: &Requires{CG: &cg}}
			err := m.Validate()
			if err == nil {
				t.Errorf("v%d requires.cg %q validated", v, s)
				continue
			}
			want := `requires.cg: invalid version ` + fmt.Sprintf("%q", s) + `: want MAJOR, MAJOR.MINOR or MAJOR.MINOR.PATCH`
			if err.Error() != want {
				t.Errorf("v%d requires.cg %q: error %q, want %q", v, s, err, want)
			}
			if strings.ContainsAny(err.Error(), "\n\t\x1b") {
				t.Errorf("error %q carries a control character", err)
			}
		}
	}
}

// #212 S3: no requires, an empty one, other keys or "cg": null: no
// requirement, and the manifest validates.
func TestRequiresAbsentOrEmpty(t *testing.T) {
	for _, r := range []string{"", `{}`, `{"python": "3.12"}`, `{"cg": null}`, `null`, `{"cg": null, "x": {"y": 1}}`} {
		m, err := parseValid(t, requiresManifest(2, r))
		if err != nil {
			t.Errorf("requires %s: %v", r, err)
			continue
		}
		if _, _, unmet := UnmetRequirement(m, "1.0.0"); unmet {
			t.Errorf("requires %s: unmet against 1.0.0", r)
		}
	}
	// The other keys are ignored next to a cg that is set.
	m, err := parseValid(t, requiresManifest(1, `{"cg": "2.1", "python": "3"}`))
	if err != nil || m.Requires.CG == nil || *m.Requires.CG != "2.1" {
		t.Errorf("requires with another key: %+v, %v", m.Requires, err)
	}
}

// #212 S4: a requires that isn't an object, or a cg that isn't a string,
// fails parsing.
func TestRequiresWrongType(t *testing.T) {
	for _, r := range []string{`"2.1"`, `[]`, `2`, `true`, `{"cg": 2}`, `{"cg": ["2"]}`, `{"cg": {}}`, `{"cg": true}`} {
		_, err := ParseManifest([]byte(requiresManifest(2, r)))
		if err == nil || !strings.HasPrefix(err.Error(), "parsing cg-extension.json: ") {
			t.Errorf("requires %s: error %v, want a parsing error", r, err)
		}
	}
}

func TestRunningCore(t *testing.T) {
	for v, want := range map[string]string{
		"2.1.0":                   "2.1.0",
		"v2.1.0":                  "2.1.0",
		"v2.1.0-4-gabc1234-dirty": "2.1.0",
		"2.1.0-rc.1":              "2.1.0",
		"2.1.0+meta":              "2.1.0",
		"v10.0.3":                 "10.0.3",
		"0.0.0":                   "0.0.0",
	} {
		if got, ok := RunningCore(v); !ok || got != want {
			t.Errorf("RunningCore(%q) = %q, %v; want %q", v, got, ok, want)
		}
	}
	// #212 S6: no core.
	for _, v := range []string{"dev", "", "abc1234", "1234567", "1234567-dirty", "v2", "2.1", "v2.1-dirty", "2.1.0.4",
		"02.1.0", "2.01.0", "2.1.00", "vv2.1.0", " 2.1.0", "2.1.0 ", "2.1.0\n", "2.1.0x"} {
		if got, ok := RunningCore(v); ok {
			t.Errorf("RunningCore(%q) = %q, true; want no core", v, got)
		}
	}
}

// #212 S5, S6: the comparison.
func TestUnmetRequirement(t *testing.T) {
	cases := []struct {
		need, running string
		unmet         bool
	}{
		{"2.1", "2.1.0", false},
		{"2.0.0", "v2.0.0", false},
		{"2.1.0", "v2.1.0-4-gabc1234-dirty", false},
		{"2", "2.0.1", false},
		{"2", "2.0.0", false},
		{"2.1.0", "2.1.0-rc.1", false}, // a release candidate counts as its release (plan, Risks)
		{"1.32", "2.0.0", false},
		{"2.1", "2.0.9", true},
		{"2.0.1", "2.0.0", true},
		{"3", "2.99.99", true},
		{"2.10", "2.9.0", true},
		{"10", "9.0.0", true},
		{"2.1.1", "v2.1.0-4-gabc1234-dirty", true},
		// #212 S6: no core, no check.
		{"99", "dev", false},
		{"99", "", false},
		{"99", "abc1234", false},
		{"99", "1234567-dirty", false},
	}
	for _, c := range cases {
		need := c.need
		m := &Manifest{Manifest: 2, Name: "a", Version: "1", Description: "d", Requires: &Requires{CG: &need}}
		gotNeed, have, unmet := UnmetRequirement(m, c.running)
		if unmet != c.unmet {
			t.Errorf("need %s, running %q: unmet = %v, want %v", c.need, c.running, unmet, c.unmet)
			continue
		}
		if unmet {
			core, _ := RunningCore(c.running)
			if gotNeed != c.need || have != core {
				t.Errorf("need %s, running %q: got (%q, %q)", c.need, c.running, gotNeed, have)
			}
		}
	}
	if _, _, unmet := UnmetRequirement(&Manifest{Manifest: 2}, "1.0.0"); unmet {
		t.Error("a manifest without requires is unmet")
	}
}

func TestRequirementMessage(t *testing.T) {
	got := RequirementMessage("shop", "2.1.0", "2.0.0")
	want := `extension "shop" needs cg 2.1.0 or newer; this is cg 2.0.0. Update cg (cg update upgrade, or brew upgrade --cask cg), then run this again.`
	if got != want {
		t.Errorf("RequirementMessage = %q\nwant %q", got, want)
	}
}

// #212 S14: a newer schema version says it needs a newer cg and how to get
// it; one below 1 doesn't.
func TestValidateSchemaVersionHint(t *testing.T) {
	m := Manifest{Manifest: SchemaVersion + 1, Name: "a", Version: "1", Description: "d"}
	want := fmt.Sprintf("unsupported manifest schema version %d (this cg understands 1 to %d): it needs a newer cg. %s",
		SchemaVersion+1, SchemaVersion, UpdateHint)
	if err := m.Validate(); err == nil || err.Error() != want {
		t.Errorf("Validate(manifest 3) = %v\nwant %q", err, want)
	}
	m.Manifest = 0
	want = fmt.Sprintf("unsupported manifest schema version 0 (this cg understands 1 to %d)", SchemaVersion)
	if err := m.Validate(); err == nil || err.Error() != want {
		t.Errorf("Validate(manifest 0) = %v, want %q", err, want)
	}
}
