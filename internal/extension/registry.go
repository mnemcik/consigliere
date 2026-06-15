package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// DefaultRegistryURL is the raw URL of the central public catalogue, served
// under the built-in "cg" registry alias. It is overridable via
// CONSIGLIERE_EXTENSIONS_REGISTRY so tests can point the "cg" alias at an
// httptest server (and forks can host their own catalogue).
const DefaultRegistryURL = "https://raw.githubusercontent.com/mnemcik/cg-extensions-registry/main/index.json"

// BuiltinRegistryAlias is the well-known alias for the public catalogue. It
// always resolves (to DefaultRegistryURL, or the env override) even when a
// workspace's .cg.json predates the registries map; `cg init` also seeds it
// explicitly. It is a named registry, addressed as "cg/<extension>" — not a
// nameless default that bare names fall through to.
const BuiltinRegistryAlias = "cg"

// qualifiedNameRE matches a fully-qualified reference "<alias>/<name>", both
// segments using the same lowercase-alphanumeric-plus-dash vocabulary as
// extension names. Exactly two segments — distinguishing it from repo URLs and
// local paths.
var qualifiedNameRE = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)

// ParseQualifiedName splits "alias/name" into its parts. ok is false for any
// input that is not exactly two slash-separated [a-z0-9-]+ segments (a bare
// name, a repo URL, a path, or anything with extra segments).
func ParseQualifiedName(s string) (alias, name string, ok bool) {
	if !qualifiedNameRE.MatchString(s) {
		return "", "", false
	}
	i := strings.IndexByte(s, '/')
	return s[:i], s[i+1:], true
}

// registryClient bounds the catalogue fetch so a hung connection can't stall
// `cg extension install <name>` indefinitely.
var registryClient = &http.Client{Timeout: 15 * time.Second}

// RegistryEntry is one extension in the catalogue.
type RegistryEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Repo        string `json:"repo"`
	// Path is the subdir within Repo holding cg-extension.json, for catalogue
	// entries that co-locate several extensions in one repo (a monorepo). Absent
	// means the repo root.
	Path          string `json:"path,omitempty"`
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

// FetchIndex retrieves and decodes the built-in "cg" catalogue over anonymous
// HTTPS (honoring the env override). Retained for callers that want the public
// registry without going through alias resolution.
func FetchIndex(ctx context.Context) (*Index, error) {
	return FetchIndexFrom(ctx, RegistryURL())
}

// FetchIndexFrom retrieves and decodes a registry catalogue from a source. A
// source ending in ".json" is fetched as a raw index over anonymous HTTPS; any
// other source is treated as a git repo, cloned (over whatever transport/auth
// git is configured with — so a private repo works), and its root index.json
// read. The git path is what lets a private repo serve as a registry.
func FetchIndexFrom(ctx context.Context, source string) (*Index, error) {
	if isRawIndexURL(source) {
		return fetchRawIndex(ctx, source)
	}
	return cloneAndReadIndex(ctx, source)
}

// isRawIndexURL reports whether source is a direct index URL (fetched over
// HTTP) rather than a git repo to clone. A git source is scp-like (git@…),
// ssh://, or any URL ending in .git; everything else with an http(s) scheme is
// a raw index endpoint (which need not end in .json — e.g. a server route).
func isRawIndexURL(source string) bool {
	if strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") || strings.HasSuffix(source, ".git") {
		return false
	}
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func fetchRawIndex(ctx context.Context, url string) (*Index, error) {
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

// cloneAndReadIndex clones a git registry repo into a temp dir and decodes its
// root index.json. The clone uses git's own auth, so a private registry repo is
// reachable over SSH (the 1Password agent) the same way private extension
// content is.
func cloneAndReadIndex(ctx context.Context, repo string) (*Index, error) {
	tmp, err := os.MkdirTemp("", "cg-registry-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir for registry %s: %w", repo, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := gitx.Clone(ctx, repo, tmp, ""); err != nil {
		return nil, fmt.Errorf("cloning registry %s: %w", repo, err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("reading index.json from registry %s: %w", repo, err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("decoding index.json from registry %s: %w", repo, err)
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
