package autoupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAPIServer stands up a fake GitHub REST API returning the given latest tag
// for /repos/{repo}/releases/latest, and points discovery at it via the
// CONSIGLIERE_GITHUB_API_BASE override.
func newAPIServer(t *testing.T, tag string, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
		} else {
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CONSIGLIERE_GITHUB_API_BASE", srv.URL)
}

func TestLatestVersionStripsAndNormalizes(t *testing.T) {
	newAPIServer(t, "v1.4.2", http.StatusOK)

	got, err := LatestVersion(context.Background(), "mnemcik/consigliere")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if got != "v1.4.2" {
		t.Errorf("LatestVersion = %q, want %q", got, "v1.4.2")
	}
}

func TestLatestVersionAddsMissingVPrefix(t *testing.T) {
	newAPIServer(t, "2.0.0", http.StatusOK) // tag without leading v

	got, err := LatestVersion(context.Background(), "mnemcik/consigliere")
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if got != "v2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", got, "v2.0.0")
	}
}

func TestLatestVersionErrorsOnNon200(t *testing.T) {
	newAPIServer(t, "", http.StatusNotFound)

	if _, err := LatestVersion(context.Background(), "mnemcik/consigliere"); err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

func TestRepoOverride(t *testing.T) {
	if Repo() != DefaultRepo {
		t.Errorf("Repo() default = %q, want %q", Repo(), DefaultRepo)
	}
	t.Setenv("CONSIGLIERE_AUTO_UPDATE_REPO", "someone/fork")
	if Repo() != "someone/fork" {
		t.Errorf("Repo() override = %q, want %q", Repo(), "someone/fork")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"1.0.0", "1.1.0", true}, // missing-v on both sides
		{"v1.2.0", "v1.2.0", false},
		{"v2.0.0", "v1.9.9", false}, // current is newer
		{"dev", "v1.0.0", false},    // dev build never reports an update
		{"v1.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestIsReleaseVersion(t *testing.T) {
	cases := map[string]bool{
		"v1.0.0":         true,
		"1.0.0":          true,
		"v1.2.3-alpha.1": true,
		"dev":            false,
		"":               false,
	}
	for v, want := range cases {
		if got := IsReleaseVersion(v); got != want {
			t.Errorf("IsReleaseVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCheck(t *testing.T) {
	newAPIServer(t, "v1.5.0", http.StatusOK)

	res, err := Check(context.Background(), "1.4.0", "mnemcik/consigliere")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Current != "v1.4.0" || res.Latest != "v1.5.0" || !res.Available {
		t.Errorf("Check = %+v, want {v1.4.0 v1.5.0 true}", res)
	}
}
