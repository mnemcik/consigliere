package autoupdate

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPrintUpdatedNoticePrintsAndClears(t *testing.T) {
	isolatedState(t)
	writeUpdatedMarker("v1.0.0", "v1.2.0")

	var buf bytes.Buffer
	PrintUpdatedNoticeIfAny(&buf)

	out := buf.String()
	if !strings.Contains(out, "v1.2.0") || !strings.Contains(out, "from v1.0.0") {
		t.Errorf("notice missing version info: %q", out)
	}
	if _, err := os.Stat(UpdatedMarkerPath()); !os.IsNotExist(err) {
		t.Error("marker should be cleared after printing")
	}
}

func TestPrintUpdatedNoticeNoMarker(t *testing.T) {
	isolatedState(t)
	var buf bytes.Buffer
	PrintUpdatedNoticeIfAny(&buf)
	if buf.Len() != 0 {
		t.Errorf("expected no output with no marker, got %q", buf.String())
	}
}

func TestPrintUpdatedNoticeSilenced(t *testing.T) {
	isolatedState(t)
	writeUpdatedMarker("v1.0.0", "v1.2.0")
	t.Setenv("CONSIGLIERE_NO_UPDATE_NOTICE", "1")

	var buf bytes.Buffer
	PrintUpdatedNoticeIfAny(&buf)
	if buf.Len() != 0 {
		t.Errorf("expected silenced output, got %q", buf.String())
	}
	if _, err := os.Stat(UpdatedMarkerPath()); err != nil {
		t.Error("silenced notice should leave the marker in place")
	}
}

func TestPrintUpdatedNoticeCorruptMarker(t *testing.T) {
	isolatedState(t)
	if err := os.WriteFile(UpdatedMarkerPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	PrintUpdatedNoticeIfAny(&buf)
	if buf.Len() != 0 {
		t.Errorf("corrupt marker should produce no output, got %q", buf.String())
	}
	if _, err := os.Stat(UpdatedMarkerPath()); !os.IsNotExist(err) {
		t.Error("corrupt marker should be dropped")
	}
}
