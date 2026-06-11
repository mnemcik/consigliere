package autoupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// DefaultRepo is the GitHub owner/name the updater queries for releases.
const DefaultRepo = "mnemcik/consigliere"

const defaultAPIBase = "https://api.github.com"

const defaultDownloadBase = "https://github.com"

// httpClient bounds discovery so a hung connection can't stall a foreground
// `cg update check` (or the detached worker) indefinitely.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// Repo resolves the repo to query, honoring the CONSIGLIERE_AUTO_UPDATE_REPO
// override (used by tests and power users / forks).
func Repo() string {
	if r := strings.TrimSpace(os.Getenv("CONSIGLIERE_AUTO_UPDATE_REPO")); r != "" {
		return r
	}
	return DefaultRepo
}

// apiBase resolves the GitHub REST API base URL. It is overridable via
// CONSIGLIERE_GITHUB_API_BASE so tests can point discovery at an httptest
// server (and so GitHub Enterprise forks can be supported later).
func apiBase() string {
	if b := strings.TrimSpace(os.Getenv("CONSIGLIERE_GITHUB_API_BASE")); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultAPIBase
}

// downloadBase resolves the host serving release assets
// (github.com/<repo>/releases/download/...). It is overridable via
// CONSIGLIERE_GITHUB_DOWNLOAD_BASE so tests can point asset downloads at an
// httptest server (production uses github.com, distinct from the API host).
func downloadBase() string {
	if b := strings.TrimSpace(os.Getenv("CONSIGLIERE_GITHUB_DOWNLOAD_BASE")); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultDownloadBase
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
}

// LatestVersion fetches the latest published release tag for repo via the
// anonymous GitHub REST API and returns it normalized to a leading-"v" semver
// string (e.g. "v1.2.3"). The repo is public, so no auth token is required.
// Returns an error on network failure, a non-200 status, or an empty tag.
func LatestVersion(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase(), repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("github api %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}

	var rel releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("decode release response: %w", err)
	}
	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", fmt.Errorf("github api %s: empty tag_name in latest release", url)
	}
	return Normalize(tag), nil
}

// Normalize ensures a leading "v" so the value compares cleanly with
// golang.org/x/mod/semver, which requires the prefix. GoReleaser injects
// versions without the "v" (e.g. "1.1.0") while `git describe` keeps it
// ("v1.1.0"), so callers must not assume either form.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// IsReleaseVersion reports whether v looks like a real released semver tag, so
// the updater should engage. Development / snapshot builds ("dev", a dirty
// `git describe`, etc.) return false and never auto-prompt or self-replace.
func IsReleaseVersion(v string) bool {
	return semver.IsValid(Normalize(v))
}

// IsNewer reports whether latest is strictly newer than current. Both are
// normalized first; if either is not valid semver it returns false.
func IsNewer(current, latest string) bool {
	c, l := Normalize(current), Normalize(latest)
	if !semver.IsValid(c) || !semver.IsValid(l) {
		return false
	}
	return semver.Compare(l, c) > 0
}

// CheckResult is the outcome of a foreground update check.
type CheckResult struct {
	Current   string // normalized, e.g. "v1.1.0"
	Latest    string // normalized, e.g. "v1.2.0"
	Available bool   // Latest is strictly newer than Current
}

// Check resolves the latest release for repo and compares it to current.
func Check(ctx context.Context, current, repo string) (CheckResult, error) {
	latest, err := LatestVersion(ctx, repo)
	if err != nil {
		return CheckResult{Current: Normalize(current)}, err
	}
	return CheckResult{
		Current:   Normalize(current),
		Latest:    latest,
		Available: IsNewer(current, latest),
	}, nil
}
