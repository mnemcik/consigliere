package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `{
  "manifest": 1,
  "name": "1password",
  "version": "0.1.0",
  "description": "1Password as authoritative credential store",
  "contributes": {
    "claude-md-sections": [{ "id": "credentials-1password", "path": "fragments/credentials.md" }],
    "notes": [{ "src": "notes/credentials-policy.md", "dest": "notes/credentials-policy.md" }],
    "hooks": [{ "event": "SessionStart", "wrapper": "hooks/gate.sh", "command": "cg-1password gate" }],
    "subcommands": [{ "namespace": "secret", "binary": "cg-1password" }],
    "templates": [{ "src": "templates/req.md", "dest": "templates/req.md" }]
  }
}`

func TestParseAndValidateValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.Name != "1password" || m.Version != "0.1.0" {
		t.Errorf("unexpected name/version: %q %q", m.Name, m.Version)
	}
	if len(m.Contributes.ClaudeMDSections) != 1 || m.Contributes.ClaudeMDSections[0].ID != "credentials-1password" {
		t.Errorf("claude-md-sections not parsed: %+v", m.Contributes.ClaudeMDSections)
	}
	if len(m.Contributes.Subcommands) != 1 || m.Contributes.Subcommands[0].Binary != "cg-1password" {
		t.Errorf("subcommands not parsed: %+v", m.Contributes.Subcommands)
	}
}

func TestParseManifestBadJSON(t *testing.T) {
	if _, err := ParseManifest([]byte("{not json")); err == nil {
		t.Fatal("expected parse error on malformed JSON")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
		want string // substring expected in the error
	}{
		{"wrong schema version", Manifest{Manifest: 2, Name: "a", Version: "1.0.0", Description: "d"}, "schema version"},
		{"empty name", Manifest{Manifest: 1, Name: "", Version: "1.0.0", Description: "d"}, "invalid name"},
		{"bad name chars", Manifest{Manifest: 1, Name: "Has_Caps", Version: "1.0.0", Description: "d"}, "invalid name"},
		{"missing version", Manifest{Manifest: 1, Name: "a", Version: "", Description: "d"}, "version is required"},
		{"missing description", Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: ""}, "description is required"},
		{
			"bad section id",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{ClaudeMDSections: []SectionContribution{{ID: "Bad ID", Path: "p"}}}},
			"invalid id",
		},
		{
			"section missing path",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{ClaudeMDSections: []SectionContribution{{ID: "ok", Path: ""}}}},
			"path is required",
		},
		{
			"note missing dest",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Notes: []CopyContribution{{Src: "s", Dest: ""}}}},
			"notes[0]",
		},
		{
			"hook missing command",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Hooks: []HookContribution{{Event: "e", Wrapper: "w", Command: ""}}}},
			"hooks[0]",
		},
		{
			"subcommand bad binary",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Subcommands: []SubcommandContribution{{Namespace: "ns", Binary: "Bad"}}}},
			"invalid binary",
		},
		{
			"template missing src",
			Manifest{Manifest: 1, Name: "a", Version: "1.0.0", Description: "d",
				Contributes: Contributions{Templates: []CopyContribution{{Src: "", Dest: "d"}}}},
			"templates[0]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Name != "1password" {
		t.Errorf("name = %q", m.Name)
	}

	if _, err := LoadManifest(t.TempDir()); err == nil {
		t.Error("expected error loading manifest from dir without one")
	}
}
