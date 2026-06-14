// Package extension implements the cg extension system: the cg-extension.json
// manifest, the per-workspace install ledger, the on-disk install layout, and
// the logic backing the `cg extension` subcommands.
//
// Scope split mirrors the rest of cg: this package stays pure (no cmd imports,
// no cobra), so the orchestration in cmd/extension.go threads in side effects
// (git clone, the wall clock) as parameters. The authoritative design is
// docs/extensions.md; DEC-001 (manifest schema), DEC-002 (install layout +
// per-workspace ledger), and DEC-003 (ext:<name>:section namespace) in the
// consigliere-extension-system project decision log govern it.
package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const (
	// SchemaVersion is the cg-extension.json manifest schema version this build
	// understands. The manifest's own "manifest" field must equal this.
	SchemaVersion = 1

	// ManifestFile is the manifest filename at an extension repo root.
	ManifestFile = "cg-extension.json"
)

// nameRe constrains extension names, section ids, and subcommand namespaces. A
// name is load-bearing: it is the install name, the .cg.json key, the clone
// directory, the ext:<name>:section marker namespace, and the cg-<name> binary
// stem. Keeping it to [a-z0-9-]+ keeps every one of those uses safe.
var nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Manifest is a parsed cg-extension.json (schema v1).
type Manifest struct {
	Manifest    int           `json:"manifest"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Contributes Contributions `json:"contributes"`
}

// Contributions are the five contribution points an extension can declare. Each
// is optional; an absent or empty array means the extension contributes nothing
// of that type.
type Contributions struct {
	ClaudeMDSections []SectionContribution    `json:"claude-md-sections,omitempty"`
	Notes            []CopyContribution       `json:"notes,omitempty"`
	Hooks            []HookContribution       `json:"hooks,omitempty"`
	Subcommands      []SubcommandContribution `json:"subcommands,omitempty"`
	Templates        []CopyContribution       `json:"templates,omitempty"`
}

// SectionContribution inserts the body of Path into the workspace CLAUDE.md as
// an ext:<name>:section block identified by ID.
type SectionContribution struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// CopyContribution copies Src (extension-relative) to Dest (workspace-relative).
// Used for both notes and templates.
type CopyContribution struct {
	Src  string `json:"src"`
	Dest string `json:"dest"`
}

// HookContribution installs the bash Wrapper into .claude/hooks/, registers it
// for Event in .claude/settings.json, and the wrapper execs Command.
type HookContribution struct {
	Event   string `json:"event"`
	Wrapper string `json:"wrapper"`
	Command string `json:"command"`
}

// SubcommandContribution exposes the extension's bin/<Binary> as cg <Namespace>.
type SubcommandContribution struct {
	Namespace string `json:"namespace"`
	Binary    string `json:"binary"`
}

// ParseManifest decodes manifest bytes. It does not validate; call Validate.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ManifestFile, err)
	}
	return &m, nil
}

// LoadManifest reads and parses <dir>/cg-extension.json.
func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ManifestFile, err)
	}
	return ParseManifest(data)
}

// Validate enforces the schema-v1 rules. It rejects an unknown schema version
// up front so a future v2 manifest fails with a clear message rather than
// silently losing fields.
func (m *Manifest) Validate() error {
	if m.Manifest != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema version %d (this cg understands %d)", m.Manifest, SchemaVersion)
	}
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("invalid name %q: must match %s", m.Name, nameRe.String())
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Description == "" {
		return fmt.Errorf("description is required")
	}
	for i, s := range m.Contributes.ClaudeMDSections {
		if !nameRe.MatchString(s.ID) {
			return fmt.Errorf("claude-md-sections[%d]: invalid id %q: must match %s", i, s.ID, nameRe.String())
		}
		if s.Path == "" {
			return fmt.Errorf("claude-md-sections[%d]: path is required", i)
		}
	}
	for i, c := range m.Contributes.Notes {
		if c.Src == "" || c.Dest == "" {
			return fmt.Errorf("notes[%d]: src and dest are required", i)
		}
	}
	for i, h := range m.Contributes.Hooks {
		if h.Event == "" || h.Wrapper == "" || h.Command == "" {
			return fmt.Errorf("hooks[%d]: event, wrapper, and command are required", i)
		}
	}
	for i, s := range m.Contributes.Subcommands {
		if !nameRe.MatchString(s.Namespace) {
			return fmt.Errorf("subcommands[%d]: invalid namespace %q: must match %s", i, s.Namespace, nameRe.String())
		}
		if !nameRe.MatchString(s.Binary) {
			return fmt.Errorf("subcommands[%d]: invalid binary %q: must match %s", i, s.Binary, nameRe.String())
		}
	}
	for i, c := range m.Contributes.Templates {
		if c.Src == "" || c.Dest == "" {
			return fmt.Errorf("templates[%d]: src and dest are required", i)
		}
	}
	return nil
}
