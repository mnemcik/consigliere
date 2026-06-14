package extension

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ledgerSubdir is the per-workspace directory holding one ledger file per
// installed extension: <workspace>/.cg/ext/<name>.json.
const ledgerSubdir = ".cg/ext"

// Ledger records exactly what an install applied to a given workspace so remove
// is precise and update can diff. It lives in the workspace (not the shared
// clone) because one clone may back several workspaces (DEC-002).
//
// M2 defines and round-trips the ledger; the contribution-point fields are
// populated when contribution application lands (M4). An install that applies no
// contributions writes no ledger, so there is never a ledger with nothing to
// reverse.
type Ledger struct {
	Name             string                   `json:"name"`
	Version          string                   `json:"version"`
	ClaudeMDSections []string                 `json:"claudeMdSections,omitempty"`
	Notes            []string                 `json:"notes,omitempty"`
	Hooks            []LedgerHook             `json:"hooks,omitempty"`
	Templates        []string                 `json:"templates,omitempty"`
	Subcommands      []SubcommandContribution `json:"subcommands,omitempty"`
	IndexRows        []LedgerIndexRow         `json:"indexRows,omitempty"`
}

// LedgerHook records an installed hook wrapper so remove can delete it and
// unregister it from settings.json.
type LedgerHook struct {
	Event   string `json:"event"`
	Wrapper string `json:"wrapper"`
}

// LedgerIndexRow records an INDEX.md pointer region added for an extension so
// remove can excise exactly that region.
type LedgerIndexRow struct {
	File   string `json:"file"`
	Marker string `json:"marker"`
}

// LedgerPath is the ledger file path for the named extension in a workspace.
// name must be a validated extension name (matching the manifest's nameRe —
// [a-z0-9-]+); callers obtain it from a manifest passed through
// Manifest.Validate, so it never contains path separators or "..".
func LedgerPath(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(ledgerSubdir), name+".json")
}

// LoadLedger reads the ledger for name. A missing ledger is not an error:
// it returns (nil, nil), meaning "nothing recorded for this extension".
func LoadLedger(root, name string) (*Ledger, error) {
	data, err := os.ReadFile(LedgerPath(root, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Save writes the ledger, creating .cg/ext/ as needed.
func (l *Ledger) Save(root string) error {
	p := LedgerPath(root, l.Name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { //nolint:gosec // workspace-local dir
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644) //nolint:gosec // workspace-local ledger
}
