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
	"strings"

	"github.com/mnemcik/consigliere/internal/githooks"
)

const (
	// SchemaVersion is the newest cg-extension.json manifest schema version
	// this build understands. Version 2 adds commands; a version 1 manifest
	// without commands is still accepted, so extensions that contribute no
	// command don't have to change.
	SchemaVersion = 2

	// ManifestFile is the manifest filename at an extension repo root.
	ManifestFile = "cg-extension.json"
)

// nameRe constrains extension names, section ids, binaries and command words. A
// name is load-bearing: it is the install name, the .cg.json key, the clone
// directory, the ext:<name>:section marker namespace, and the cg-<name> binary
// stem. Keeping it to [a-z0-9-]+ keeps every one of those uses safe.
var nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// Manifest is a parsed cg-extension.json (schema v1 or v2).
type Manifest struct {
	Manifest    int           `json:"manifest"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Contributes Contributions `json:"contributes"`
}

// Contributions are the contribution points an extension can declare. Each
// is optional; an absent or empty array means the extension contributes nothing
// of that type.
type Contributions struct {
	ClaudeMDSections []SectionContribution `json:"claude-md-sections,omitempty"`
	Notes            []CopyContribution    `json:"notes,omitempty"`
	Hooks            []HookContribution    `json:"hooks,omitempty"`
	// Commands are the cg commands the extension provides (manifest v2).
	Commands  []CommandContribution `json:"commands,omitempty"`
	Templates []CopyContribution    `json:"templates,omitempty"`
	GitHooks  []GitHookContribution `json:"git-hooks,omitempty"`
	Gitignore []SectionContribution `json:"gitignore,omitempty"`
	// Launcher wraps the agent command `cg start` runs (at most one entry).
	Launcher []LauncherContribution `json:"launcher,omitempty"`
}

// SectionContribution inserts the body of Path into the workspace CLAUDE.md as
// an ext:<name>:section block identified by ID.
type SectionContribution struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// CopyContribution copies Src (extension-relative) to Dest (workspace-relative).
// Used for both notes and templates. Index, for a note only, is the one-line
// description on its row in the workspace notes index.
type CopyContribution struct {
	Src   string `json:"src"`
	Dest  string `json:"dest"`
	Index string `json:"index,omitempty"`
}

// HookContribution installs the bash Wrapper into .claude/hooks/, registers it
// for Event in .claude/settings.json, and the wrapper execs Command.
type HookContribution struct {
	Event   string `json:"event"`
	Wrapper string `json:"wrapper"`
	Command string `json:"command"`
}

// GitHookContribution installs the executable Script (extension-relative) as
// one of the scripts the cg dispatcher runs for the git Hook (e.g.
// "post-commit"). See package githooks.
type GitHookContribution struct {
	Hook   string `json:"hook"`
	Script string `json:"script"`
}

// CommandContribution declares that `cg <Command> …` runs the extension's
// bin/<Binary>. Command is one or more words, each matching [a-z0-9-]+,
// separated by single spaces; its first word is its namespace. The binary
// receives the arguments after the namespace word.
type CommandContribution struct {
	Command string `json:"command"`
	Binary  string `json:"binary"`
}

// LauncherContribution makes `cg start` (and bare `cg`) run the extension's
// bin/<Binary> with Args, then "--", then the agent command and the user's
// arguments, instead of the agent directly. The binary is expected to exec the
// agent itself (for example inside a terminal multiplexer).
type LauncherContribution struct {
	Binary string   `json:"binary"`
	Args   []string `json:"args,omitempty"`
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

// Validate enforces the schema rules. It rejects an unknown schema version up
// front so a future manifest fails with a clear message rather than silently
// losing fields.
func (m *Manifest) Validate() error {
	if m.Manifest < 1 || m.Manifest > SchemaVersion {
		return fmt.Errorf("unsupported manifest schema version %d (this cg understands 1 to %d)", m.Manifest, SchemaVersion)
	}
	if len(m.Contributes.Commands) > 0 && m.Manifest < 2 {
		return fmt.Errorf(`commands need "manifest": 2 (got %d)`, m.Manifest)
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
		if err := insideDir("path", s.Path, "extension"); err != nil {
			return fmt.Errorf("claude-md-sections[%d]: %w", i, err)
		}
	}
	for i, g := range m.Contributes.Gitignore {
		if !nameRe.MatchString(g.ID) {
			return fmt.Errorf("gitignore[%d]: invalid id %q: must match %s", i, g.ID, nameRe.String())
		}
		if g.Path == "" {
			return fmt.Errorf("gitignore[%d]: path is required", i)
		}
		if err := insideDir("path", g.Path, "extension"); err != nil {
			return fmt.Errorf("gitignore[%d]: %w", i, err)
		}
	}
	for i, c := range m.Contributes.Notes {
		if c.Src == "" || c.Dest == "" {
			return fmt.Errorf("notes[%d]: src and dest are required", i)
		}
		if err := copyPathsInside(c); err != nil {
			return fmt.Errorf("notes[%d]: %w", i, err)
		}
		if strings.ContainsAny(c.Index, "\r\n") {
			return fmt.Errorf("notes[%d]: index must be a single line", i)
		}
	}
	for i, h := range m.Contributes.Hooks {
		if h.Event == "" || h.Wrapper == "" || h.Command == "" {
			return fmt.Errorf("hooks[%d]: event, wrapper, and command are required", i)
		}
		if err := insideDir("wrapper", h.Wrapper, "extension"); err != nil {
			return fmt.Errorf("hooks[%d]: %w", i, err)
		}
	}
	commands := map[string]int{}
	for i, c := range m.Contributes.Commands {
		if err := ValidCommand(c.Command); err != nil {
			return fmt.Errorf("commands[%d]: %w", i, err)
		}
		if !nameRe.MatchString(c.Binary) {
			return fmt.Errorf("commands[%d]: invalid binary %q: must match %s", i, c.Binary, nameRe.String())
		}
		if j, dup := commands[c.Command]; dup {
			return fmt.Errorf("commands[%d]: command %q is already declared by commands[%d]", i, c.Command, j)
		}
		commands[c.Command] = i
	}
	if len(m.Contributes.Launcher) > 1 {
		return fmt.Errorf("launcher: at most one entry, got %d", len(m.Contributes.Launcher))
	}
	for i, l := range m.Contributes.Launcher {
		if !nameRe.MatchString(l.Binary) {
			return fmt.Errorf("launcher[%d]: invalid binary %q: must match %s", i, l.Binary, nameRe.String())
		}
		for j, a := range l.Args {
			if a == "" {
				return fmt.Errorf("launcher[%d]: args[%d] is empty", i, j)
			}
		}
	}
	for i, c := range m.Contributes.Templates {
		if c.Src == "" || c.Dest == "" {
			return fmt.Errorf("templates[%d]: src and dest are required", i)
		}
		if err := copyPathsInside(c); err != nil {
			return fmt.Errorf("templates[%d]: %w", i, err)
		}
		if c.Index != "" {
			return fmt.Errorf("templates[%d]: index is only for notes", i)
		}
	}
	gitHookDests := map[string]int{}
	for i, g := range m.Contributes.GitHooks {
		if !githooks.Valid(g.Hook) {
			return fmt.Errorf("git-hooks[%d]: unsupported hook %q (want one of %s)", i, g.Hook, strings.Join(githooks.Hooks, ", "))
		}
		// The script is copied into the workspace and approved to run, so it
		// must come from inside the extension.
		clean, err := CleanSubdir(g.Script)
		if err != nil || clean == "" {
			return fmt.Errorf("git-hooks[%d]: script %q must be a relative path inside the extension", i, g.Script)
		}
		dest := gitHookDestRel(m.Name, g)
		if j, dup := gitHookDests[dest]; dup {
			return fmt.Errorf("git-hooks[%d]: script %q installs to %s, as git-hooks[%d] does; give it a different file name", i, g.Script, dest, j)
		}
		gitHookDests[dest] = i
	}
	return nil
}

// ValidCommand checks a command's syntax: one or more words, each matching
// [a-z0-9-]+, separated by single spaces.
func ValidCommand(command string) error {
	switch {
	case command == "":
		return fmt.Errorf("command is required")
	case strings.TrimSpace(command) != command:
		return fmt.Errorf("invalid command %q: no leading or trailing space", command)
	case strings.Contains(command, "  "):
		return fmt.Errorf("invalid command %q: words must be separated by single spaces", command)
	}
	for _, w := range strings.Split(command, " ") {
		if !nameRe.MatchString(w) {
			return fmt.Errorf("invalid command %q: word %q must match %s", command, w, nameRe.String())
		}
	}
	return nil
}

// copyPathsInside checks that a copy contribution reads from inside the
// extension and writes inside the workspace.
func copyPathsInside(c CopyContribution) error {
	if err := insideDir("src", c.Src, "extension"); err != nil {
		return err
	}
	return insideDir("dest", c.Dest, "workspace")
}

// insideDir rejects a manifest path that is absolute or climbs out of its base
// directory with "..": cg reads and writes these paths directly, so one that
// escapes would touch files outside the extension clone or the workspace.
func insideDir(field, p, base string) error {
	if clean, err := CleanSubdir(p); err != nil || clean == "" {
		return fmt.Errorf("%s %q must be a relative path inside the %s", field, p, base)
	}
	return nil
}
