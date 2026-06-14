package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultRegistryURL is the raw URL of the central extension catalogue. It is
// overridable via CONSIGLIERE_EXTENSIONS_REGISTRY so tests can point resolution
// at an httptest server (and forks can host their own catalogue).
const DefaultRegistryURL = "https://raw.githubusercontent.com/mnemcik/cg-extensions-registry/main/index.json"

// registryClient bounds the catalogue fetch so a hung connection can't stall
// `cg extension install <name>` indefinitely.
var registryClient = &http.Client{Timeout: 15 * time.Second}

// RegistryEntry is one extension in the catalogue.
type RegistryEntry struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Repo          string `json:"repo"`
	LatestVersion string `json:"latestVersion"`
	ManifestURL   string `json:"manifestUrl"`
}

// Index is the registry catalogue (index.json).
type Index struct {
	Schema     string          `json:"$schema,omitempty"`
	Version    int             `json:"version"`
	Extensions []RegistryEntry `json:"extensions"`
}

// RegistryURL resolves the catalogue URL, honoring the env override.
func RegistryURL() string {
	if u := strings.TrimSpace(os.Getenv("CONSIGLIERE_EXTENSIONS_REGISTRY")); u != "" {
		return u
	}
	return DefaultRegistryURL
}

// FetchIndex retrieves and decodes the registry catalogue over anonymous HTTPS.
func FetchIndex(ctx context.Context) (*Index, error) {
	url := RegistryURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := registryClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching registry %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fetching registry %s: status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var idx Index
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("decoding registry %s: %w", url, err)
	}
	return &idx, nil
}

// Find returns the entry with the given name, or (nil, false).
func (idx *Index) Find(name string) (*RegistryEntry, bool) {
	for i := range idx.Extensions {
		if idx.Extensions[i].Name == name {
			return &idx.Extensions[i], true
		}
	}
	return nil, false
}
