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
