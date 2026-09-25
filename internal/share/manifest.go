package share

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Version  int                         `json:"version"`
	Owner    string                      `json:"owner"`
	Audience string                      `json:"audience"`
	Projects map[string]PublishedProject `json:"projects"`
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

// ReadPublished clones the share repo into a temporary directory and returns
// the manifest on branch. It returns (nil, nil) when nothing has been
// published yet: an empty repo, a branch that does not exist, or a branch
// without a manifest. Clone and auth errors are returned as they are.
func ReadPublished(ctx context.Context, repo, branch string) (*Manifest, error) {
	tmp, err := os.MkdirTemp("", "cg-share-read-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	dest := filepath.Join(tmp, "repo")
	if err := gitx.Clone(ctx, repo, dest, ""); err != nil {
		return nil, fmt.Errorf("reading share repo %s: %w", repo, err)
	}
	ref := "origin/" + branch
	if !gitx.CommitishExists(ctx, dest, ref) {
		return nil, nil
	}
	if _, err := gitx.Run(ctx, dest, "cat-file", "-e", ref+":"+ManifestFile); err != nil {
		return nil, nil
	}
	data, err := gitx.Run(ctx, dest, "show", ref+":"+ManifestFile)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil, fmt.Errorf("share repo %s: %s is not valid: %w", repo, ManifestFile, err)
	}
	if m.Version > ManifestVersion {
		return nil, fmt.Errorf("share repo %s: manifest version %d is newer than this cg understands (%d); update cg", repo, m.Version, ManifestVersion)
	}
	return &m, nil
}

// ExportedNames returns the file names an export of the project folder dir
// would select, with the given includes. Callers use it to map the files of
// other projects shared with the same audience, so links between them survive.
func ExportedNames(dir string, include []string) ([]string, error) {
	return selectFiles(dir, include)
}
