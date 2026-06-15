package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

func TestFetchIndexAndFind(t *testing.T) {
	const body = `{
	  "version": 1,
	  "extensions": [
	    {"name":"1password","description":"creds","repo":"https://example.com/cg-ext-1password","latestVersion":"0.1.0","manifestUrl":"https://example.com/m.json"},
	    {"name":"voice","description":"voice","repo":"https://example.com/cg-ext-voice","latestVersion":"0.2.0","manifestUrl":"https://example.com/v.json"}
	  ]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", srv.URL)

	idx, err := FetchIndex(context.Background())
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(idx.Extensions) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx.Extensions))
	}
	e, ok := idx.Find("voice")
	if !ok || e.Repo != "https://example.com/cg-ext-voice" || e.LatestVersion != "0.2.0" {
		t.Errorf("Find(voice) = %+v ok=%v", e, ok)
	}
	if _, ok := idx.Find("absent"); ok {
		t.Error("Find(absent) should be false")
	}
}

func TestFetchIndexHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", srv.URL)

	if _, err := FetchIndex(context.Background()); err == nil {
		t.Fatal("expected error on 404")
	}
}

func TestParseQualifiedName(t *testing.T) {
	cases := []struct {
		in          string
		alias, name string
		ok          bool
	}{
		{"cg/1password", "cg", "1password", true},
		{"visma/vpaas-backlog", "visma", "vpaas-backlog", true},
		{"1password", "", "", false},                  // bare
		{"git@github.com:o/r.git", "", "", false},     // repo url
		{"https://example.com/x.json", "", "", false}, // url
		{"./local/path", "", "", false},               // path
		{"a/b/c", "", "", false},                      // too many segments
		{"Cg/X", "", "", false},                       // uppercase rejected
		{"cg/", "", "", false},                        // empty name
	}
	for _, c := range cases {
		alias, name, ok := ParseQualifiedName(c.in)
		if ok != c.ok || alias != c.alias || name != c.name {
			t.Errorf("ParseQualifiedName(%q) = (%q,%q,%v), want (%q,%q,%v)",
				c.in, alias, name, ok, c.alias, c.name, c.ok)
		}
	}
}

// TestFetchIndexFromGitRepo validates the private-registry transport: a git repo
// holding index.json at its root is cloned and decoded, with no HTTP involved.
func TestFetchIndexFromGitRepo(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "test"},
	} {
		if _, err := gitx.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	const index = `{"version":1,"extensions":[{"name":"vpaas-backlog","description":"d","repo":"git@example:o/r.git","path":"vpaas-backlog","latestVersion":"0.1.0","manifestUrl":"x"}]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(ctx, dir, "commit", "--quiet", "-m", "init"); err != nil {
		t.Fatal(err)
	}

	// dir has no .git/.json suffix and no scheme, so it's treated as a git source.
	if isRawIndexURL(dir) {
		t.Fatalf("local repo path should not be treated as a raw URL: %q", dir)
	}
	idx, err := FetchIndexFrom(ctx, dir)
	if err != nil {
		t.Fatalf("FetchIndexFrom(git repo): %v", err)
	}
	e, ok := idx.Find("vpaas-backlog")
	if !ok || e.Path != "vpaas-backlog" {
		t.Errorf("Find(vpaas-backlog) = %+v ok=%v", e, ok)
	}
}

func TestIsRawIndexURL(t *testing.T) {
	raw := []string{
		"https://raw.githubusercontent.com/o/r/main/index.json",
		"http://127.0.0.1:54321", // httptest-style, no .json suffix
		"https://example.com/registry",
	}
	git := []string{
		"git@github.com:o/r.git",
		"ssh://git@host/o/r.git",
		"https://github.com/o/r.git",
		"/local/bare/repo",
		"./relative/repo",
	}
	for _, s := range raw {
		if !isRawIndexURL(s) {
			t.Errorf("isRawIndexURL(%q) = false, want true", s)
		}
	}
	for _, s := range git {
		if isRawIndexURL(s) {
			t.Errorf("isRawIndexURL(%q) = true, want false", s)
		}
	}
}

func TestRegistryURLOverride(t *testing.T) {
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", "")
	if RegistryURL() != DefaultRegistryURL {
		t.Errorf("empty override should yield default, got %q", RegistryURL())
	}
	t.Setenv("CONSIGLIERE_EXTENSIONS_REGISTRY", "https://x/y.json")
	if RegistryURL() != "https://x/y.json" {
		t.Errorf("override not honoured: %q", RegistryURL())
	}
}
