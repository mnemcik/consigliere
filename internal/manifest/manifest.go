// Package manifest tracks the framework-managed artifacts in a Consigliere
// workspace — the CLAUDE.md sections and framework notes that `cg` owns — so a
// future `cg sync` can distinguish what `cg` last wrote from what the user has
// since changed.
//
// It records, per managed artifact, a content hash of what `cg` last wrote plus
// the framework version, in `.cg/manifest.json`. This is durable workspace
// state (committed, not gitignored): it must survive a re-clone so `cg sync`
// can reconcile content after the binary self-updates.
//
// Scope: this is the *content* side of upgrades. Replacing the `cg` binary
// itself is a separate concern (`cg update`, the release pipeline). The
// ownership contract and reconcile model are documented in docs/workspace-sync.md.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// Dir is the workspace-relative directory holding cg-managed state.
	Dir = ".cg"
	// File is the manifest filename within Dir.
	File = "manifest.json"
	// SchemaVersion is the manifest format version; bump on incompatible changes.
	SchemaVersion = 1
)

// Artifact records what `cg` last wrote for one managed artifact.
type Artifact struct {
	// Hash is the SHA-256 (hex) of the artifact's canonical content as `cg`
	// last wrote it. For a CLAUDE.md section this is the inner content between
	// the section's sentinel markers, with surrounding newlines trimmed.
	Hash string `json:"hash"`
}

// Manifest is the set of framework-managed artifacts in a workspace.
type Manifest struct {
	SchemaVersion    int    `json:"schemaVersion"`
	FrameworkVersion string `json:"frameworkVersion"`
	// Sections maps a CLAUDE.md sentinel section id to its artifact record.
	Sections map[string]Artifact `json:"sections"`
	// Notes maps a workspace-relative framework-note path to its artifact
	// record. Empty until the load-on-demand project ships framework notes.
	Notes map[string]Artifact `json:"notes"`
}

// sectionStartRe matches a framework-section start sentinel and captures its id.
// Only `cg:section` blocks are framework-managed; `user:section` blocks are the
// user's and are deliberately never parsed here.
var sectionStartRe = regexp.MustCompile(`(?m)^<!-- cg:section:start=([a-z0-9-]+) -->[ \t]*$`)

// HashContent returns the SHA-256 (hex) of s.
func HashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ParseSections extracts every framework `cg:section` block from CLAUDE.md
// content, returning a map of section id to its inner content (surrounding
// newlines trimmed). A start marker with no matching end marker is skipped.
func ParseSections(content string) map[string]string {
	out := make(map[string]string)
	for _, m := range sectionStartRe.FindAllStringSubmatchIndex(content, -1) {
		id := content[m[2]:m[3]]
		afterStart := content[m[1]:]
		endMarker := fmt.Sprintf("<!-- cg:section:end=%s -->", id)
		idx := strings.Index(afterStart, endMarker)
		if idx < 0 {
			continue
		}
		out[id] = strings.Trim(afterStart[:idx], "\n")
	}
	return out
}

// FromCLAUDE builds a Manifest from CLAUDE.md content at the given framework
// version. Notes are initialized empty; they are registered by the projects
// that ship framework notes.
func FromCLAUDE(claudeMD, frameworkVersion string) *Manifest {
	sections := make(map[string]Artifact)
	for id, inner := range ParseSections(claudeMD) {
		sections[id] = Artifact{Hash: HashContent(inner)}
	}
	return &Manifest{
		SchemaVersion:    SchemaVersion,
		FrameworkVersion: frameworkVersion,
		Sections:         sections,
		Notes:            make(map[string]Artifact),
	}
}

// NotesFromFS walks fsys — a tree of framework notes — and returns a manifest
// note map keyed by workspace-relative path. Each key is destPrefix joined with
// the note's path within fsys (forward-slash separated, matching the manifest's
// portable path convention); the value records a SHA-256 hash of the note's
// content as the framework ships it.
//
// Directories and dotfiles (any path element whose base name begins with ".")
// are skipped, so VCS/editor artifacts like `.gitkeep` — which keep the
// otherwise-empty framework-notes directory present in the embed tree — never
// become managed notes. The result is a non-nil (possibly empty) map.
func NotesFromFS(fsys fs.FS, destPrefix string) (map[string]Artifact, error) {
	out := make(map[string]Artifact)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasPrefix(path.Base(p), ".") {
			return nil
		}
		data, rerr := fs.ReadFile(fsys, p)
		if rerr != nil {
			return rerr
		}
		out[path.Join(destPrefix, p)] = Artifact{Hash: HashContent(string(data))}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Path returns the manifest path for a workspace directory.
func Path(dir string) string {
	return filepath.Join(dir, Dir, File)
}

// Save writes the manifest to .cg/manifest.json under dir, creating .cg/ if needed.
func (m *Manifest) Save(dir string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", Dir, err)
	}
	if err := os.WriteFile(Path(dir), data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// Load reads the manifest from dir. It returns (nil, nil) if none exists.
func Load(dir string) (*Manifest, error) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}
