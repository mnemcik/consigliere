package autoupdate

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestIsMajorBump(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"v1.2.3", "v2.0.0", true},
		{"1.9.9", "2.0.0", true},
		{"v1.2.3", "v1.3.0", false}, // minor
		{"v1.2.3", "v1.2.4", false}, // patch
		{"v2.0.0", "v1.0.0", false}, // downgrade
		{"dev", "v2.0.0", false},    // invalid current
	}
	for _, c := range cases {
		if got := IsMajorBump(c.from, c.to); got != c.want {
			t.Errorf("IsMajorBump(%q,%q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestHandleMajorAvailableWritesAndRespectsIgnore(t *testing.T) {
	isolatedState(t)

	if err := handleMajorAvailable("mnemcik/consigliere", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	m, ok := readMajorMarker()
	if !ok || m.ToVersion != "v2.0.0" || m.FromVersion != "v1.0.0" {
		t.Fatalf("marker not written correctly: %+v ok=%v", m, ok)
	}
	if m.ChangelogURL == "" {
		t.Error("changelog URL should be populated")
	}

	// Ignoring v2.0.0 then re-handling must not re-create the marker.
	if _, _, err := IgnoreMajor(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMajorMarker(); ok {
		t.Error("IgnoreMajor should clear the marker")
	}
	if err := handleMajorAvailable("mnemcik/consigliere", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, ok := readMajorMarker(); ok {
		t.Error("ignored version must not re-arm the marker")
	}
}

func TestHandleMajorAvailablePreservesSnooze(t *testing.T) {
	isolatedState(t)
	if err := handleMajorAvailable("r", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	to, pending, err := SnoozeMajor(MajorSnoozeDuration)
	if err != nil || !pending || to != "v2.0.0" {
		t.Fatalf("snooze: to=%q pending=%v err=%v", to, pending, err)
	}
	// Re-handling the same target must keep the snooze.
	if err := handleMajorAvailable("r", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	m, _ := readMajorMarker()
	if m.SnoozeUntil == "" {
		t.Error("re-handling same target should preserve SnoozeUntil")
	}
}

func TestClearStaleMajorMarker(t *testing.T) {
	isolatedState(t)
	if err := handleMajorAvailable("r", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}
	// Still on v1 → marker stays.
	ClearStaleMajorMarker("v1.5.0")
	if _, ok := readMajorMarker(); !ok {
		t.Error("marker should remain while current major < target")
	}
	// Reached v2 → marker cleared.
	ClearStaleMajorMarker("v2.0.0")
	if _, ok := readMajorMarker(); ok {
		t.Error("marker should clear once current major >= target")
	}
}

func TestSnoozeAndIgnoreNoPending(t *testing.T) {
	isolatedState(t)
	if _, pending, err := SnoozeMajor(MajorSnoozeDuration); err != nil {
		t.Fatalf("SnoozeMajor: %v", err)
	} else if pending {
		t.Error("SnoozeMajor should report not-pending with no marker")
	}
	if _, pending, err := IgnoreMajor(); err != nil {
		t.Fatalf("IgnoreMajor: %v", err)
	} else if pending {
		t.Error("IgnoreMajor should report not-pending with no marker")
	}
}

func TestDoCheckAndInstallMajorBumpWritesMarkerNoInstall(t *testing.T) {
	isolatedState(t)
	// Mark as install.sh-managed so DetectManagement returns self-managed; the
	// major branch must still NOT install (so the test binary is never touched).
	js := `{"version":"1.0.0","tag":"v1.0.0","method":"install.sh","path":"/tmp/cg"}`
	if err := os.WriteFile(InstalledStatePath(), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	newAPIServer(t, "v2.0.0", 200) // sets CONSIGLIERE_GITHUB_API_BASE + cleanup
	t.Setenv("CONSIGLIERE_AUTO_UPDATE_REPO", "mnemcik/consigliere")

	doCheckAndInstall("v1.0.0")

	if _, ok := readMajorMarker(); !ok {
		t.Error("major bump should write a major-available marker")
	}
	if _, err := os.Stat(UpdatedMarkerPath()); !os.IsNotExist(err) {
		t.Error("major bump must NOT write an updated marker (no auto-install)")
	}
}

func TestPrintMajorNoticePrintsSnoozesSilences(t *testing.T) {
	isolatedState(t)
	if err := handleMajorAvailable("mnemcik/consigliere", "v1.0.0", "v2.0.0"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	PrintMajorNoticeIfAny(&buf)
	if !strings.Contains(buf.String(), "v2.0.0") || !strings.Contains(buf.String(), "major release") {
		t.Errorf("expected major notice, got %q", buf.String())
	}

	// Snoozed → silent.
	if _, _, err := SnoozeMajor(MajorSnoozeDuration); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	PrintMajorNoticeIfAny(&buf)
	if buf.Len() != 0 {
		t.Errorf("snoozed notice should be silent, got %q", buf.String())
	}

	// Env silence overrides everything.
	t.Setenv("CONSIGLIERE_NO_UPDATE_NOTICE", "1")
	buf.Reset()
	PrintMajorNoticeIfAny(&buf)
	if buf.Len() != 0 {
		t.Errorf("NO_UPDATE_NOTICE should silence, got %q", buf.String())
	}
}
