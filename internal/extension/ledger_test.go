package extension

import (
	"path/filepath"
	"testing"
)

func TestLedgerSaveLoadRoundtrip(t *testing.T) {
	root := t.TempDir()
	l := &Ledger{
		Name:             "1password",
		Version:          "0.1.0",
		ClaudeMDSections: []string{"credentials-1password"},
		Notes:            []string{"notes/credentials-policy.md"},
		Hooks:            []LedgerHook{{Event: "SessionStart", Wrapper: ".claude/hooks/gate.sh"}},
		Templates:        []string{"templates/req.md"},
		Subcommands:      []SubcommandContribution{{Namespace: "secret", Binary: "cg-1password"}},
		IndexRows:        []LedgerIndexRow{{File: "notes/INDEX.md", Marker: "ext:1password"}},
	}
	if err := l.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}

	want := filepath.Join(root, ".cg", "ext", "1password.json")
	if LedgerPath(root, "1password") != want {
		t.Errorf("LedgerPath = %q, want %q", LedgerPath(root, "1password"), want)
	}

	got, err := LoadLedger(root, "1password")
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if got == nil {
		t.Fatal("LoadLedger returned nil for an existing ledger")
	}
	if got.Name != l.Name || got.Version != l.Version {
		t.Errorf("name/version mismatch: %+v", got)
	}
	if len(got.ClaudeMDSections) != 1 || got.ClaudeMDSections[0] != "credentials-1password" {
		t.Errorf("sections roundtrip: %+v", got.ClaudeMDSections)
	}
	if len(got.Hooks) != 1 || got.Hooks[0].Event != "SessionStart" {
		t.Errorf("hooks roundtrip: %+v", got.Hooks)
	}
	if len(got.Subcommands) != 1 || got.Subcommands[0].Binary != "cg-1password" {
		t.Errorf("subcommands roundtrip: %+v", got.Subcommands)
	}
}

func TestLoadLedgerMissingIsNotError(t *testing.T) {
	got, err := LoadLedger(t.TempDir(), "absent")
	if err != nil {
		t.Fatalf("expected no error for a missing ledger, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil ledger for a missing file, got %+v", got)
	}
}
