package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runUpdateCheck(t *testing.T, version string) string {
	t.Helper()
	// Isolate auto-update state so the root PersistentPreRunE's updated-notice
	// read doesn't touch (or print from) the real ~/.config.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old := Version
	Version = version
	t.Cleanup(func() { Version = old })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"update", "check"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return buf.String()
}

// fakeGitHub points discovery at a local server returning the given latest tag.
func fakeGitHub(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CONSIGLIERE_GITHUB_API_BASE", srv.URL)
	t.Setenv("CONSIGLIERE_AUTO_UPDATE_REPO", "mnemcik/consigliere")
}

func TestUpdateCheckDevBuildSkips(t *testing.T) {
	out := runUpdateCheck(t, "dev")
	if !strings.Contains(out, "development build") {
		t.Errorf("dev build output missing notice: %q", out)
	}
}

func TestUpdateCheckReportsAvailable(t *testing.T) {
	fakeGitHub(t, "v9.9.9")
	out := runUpdateCheck(t, "1.0.0")
	if !strings.Contains(out, "v9.9.9") || !strings.Contains(out, "cg update upgrade") {
		t.Errorf("expected update-available output, got: %q", out)
	}
}

func TestUpdateCheckReportsUpToDate(t *testing.T) {
	fakeGitHub(t, "v1.0.0")
	out := runUpdateCheck(t, "1.0.0")
	if !strings.Contains(out, "latest version") {
		t.Errorf("expected up-to-date output, got: %q", out)
	}
}

func runUpdate(t *testing.T, args ...string) string {
	t.Helper()
	old := Version
	Version = "1.0.0"
	// Reset flag-bound vars: cobra does not clear them between Execute() calls.
	snoozeMajor, ignoreMajor = false, false
	t.Cleanup(func() { Version = old })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return buf.String()
}

func TestUpdateSnoozeRequiresMajorFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out := runUpdate(t, "update", "snooze")
	if !strings.Contains(out, "--major") {
		t.Errorf("snooze without --major should prompt for it, got: %q", out)
	}
}

func TestUpdateSnoozeNoPending(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out := runUpdate(t, "update", "snooze", "--major")
	if !strings.Contains(out, "No pending major") {
		t.Errorf("snooze with no marker should say so, got: %q", out)
	}
}

func TestUpdateIgnoreNoPending(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out := runUpdate(t, "update", "ignore", "--major")
	if !strings.Contains(out, "No pending major") {
		t.Errorf("ignore with no marker should say so, got: %q", out)
	}
}
