package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
