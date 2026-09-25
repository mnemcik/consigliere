package share

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mnemcik/consigliere/internal/gitx"
)

// ManifestFile is the manifest at the root of a share repo. cg writes it on
// every publish and reads it back to tell what is already published.
const ManifestFile = ".cg-share.json"

// ManifestVersion is the manifest schema version written by this cg.
const ManifestVersion = 1

// Manifest records what a share repo holds. The repo's content is a pure
// function of config plus workspace, so the manifest is the only state
// publishing needs, and it lives with the published copy rather than in the
// owner's workspace.
type Manifest struct {
	Version int `json:"version"`
	// Generator names the cg that wrote the manifest (e.g. "cg 1.18.0"), for
	// diagnosing a publish; compatibility is decided by Version alone.
	Generator string                      `json:"generator,omitempty"`
	Owner     string                      `json:"owner"`
	Audience  string                      `json:"audience"`
	Projects  map[string]PublishedProject `json:"projects"`
}

// PublishedProject is one project as last published.
type PublishedProject struct {
	// SourceCommit and SourceDate are the stamp the export was rendered from.
	SourceCommit string `json:"sourceCommit"`
	SourceDate   string `json:"sourceDate"`
	// Files are the published file names, relative to the project folder.
	Files []string `json:"files"`
	// ContentHash covers the rendered files. It catches changes the source
	// commit cannot, such as a status edit in the project index.
	ContentHash string `json:"contentHash"`
}

// ContentHash is the SHA-256 over a rendered export's paths and contents, in
// path order, so it is independent of map iteration.
func ContentHash(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		// Length-prefix both parts so no two different exports can hash alike.
		_, _ = fmt.Fprintf(h, "%d:%s%d:%s", len(p), p, len(files[p]), files[p]) // hash.Hash writes never fail
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PublishedEntry describes an export the way the manifest records it.
func PublishedEntry(res *Result) PublishedProject {
	files := make([]string, 0, len(res.Files))
	for p := range res.Files {
		if _, name, ok := strings.Cut(p, "/"); ok {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return PublishedProject{
		SourceCommit: res.Stamp.SHA,
		SourceDate:   res.Stamp.Date,
		Files:        files,
		ContentHash:  ContentHash(res.Files),
	}
}

// userinfoRe matches the userinfo of a URL (scheme://user:secret@).
var userinfoRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)

// RedactURL hides URL userinfo in s, so a token embedded in a repo URL never
// reaches a terminal or an error message.
func RedactURL(s string) string {
	return userinfoRe.ReplaceAllString(s, "${1}***@")
}

// readEnv makes remote reads fail instead of waiting on a prompt: no
// credential prompt over https, and ssh in batch mode (an unknown host key or
// a key passphrase fails). A user's own GIT_SSH_COMMAND or core.sshCommand is
// left alone, since overriding it could bypass their key or agent setup.
func readEnv(ctx context.Context) []string {
	env := make([]string, 0, 3)
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	if os.Getenv("GIT_SSH_COMMAND") != "" {
		return env
	}
	if cmd, _ := gitx.Run(ctx, "", "config", "--get", "core.sshCommand"); cmd != "" {
		return env
	}
	return append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
}

// ReadPublished returns the manifest on branch of the share repo. It returns
// (nil, nil) when nothing has been published yet: an empty repo, a branch
// that does not exist, or a branch without a manifest. Reachability and auth
// errors are returned as they are. Only the branch tip is fetched, and git
// never prompts for credentials.
func ReadPublished(ctx context.Context, repo, branch string) (*Manifest, error) {
	if strings.HasPrefix(repo, "-") {
		return nil, fmt.Errorf("share repo %q: a repo URL cannot start with '-'", repo)
	}
	env := readEnv(ctx)
	heads, err := gitx.RunEnv(ctx, "", env, "ls-remote", "--heads", repo, "refs/heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("reading share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	if strings.TrimSpace(heads) == "" {
		return nil, nil
	}

	tmp, err := os.MkdirTemp("", "cg-share-read-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dest := filepath.Join(tmp, "repo")
	if _, err := gitx.RunEnv(ctx, "", env, "clone", "--quiet", "--depth", "1", "--single-branch", "--branch", branch, "--", repo, dest); err != nil {
		return nil, fmt.Errorf("reading share repo %s: %s", RedactURL(repo), RedactURL(err.Error()))
	}
	if _, err := gitx.Run(ctx, dest, "cat-file", "-e", "HEAD:"+ManifestFile); err != nil {
		return nil, nil
	}
	data, err := gitx.Run(ctx, dest, "show", "HEAD:"+ManifestFile)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil, fmt.Errorf("share repo %s: %s is not valid: %w", RedactURL(repo), ManifestFile, err)
	}
	switch {
	case m.Version < 1:
		return nil, fmt.Errorf("share repo %s: %s has no schema version; it was not written by cg", RedactURL(repo), ManifestFile)
	case m.Version > ManifestVersion:
		return nil, fmt.Errorf("share repo %s: manifest version %d is newer than this cg understands (%d); update cg", RedactURL(repo), m.Version, ManifestVersion)
	}
	return &m, nil
}
