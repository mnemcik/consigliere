package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// verifiedManifest loads extension e's own manifest from its tree at the
// recorded commit and validates it, returning the extension's directory in the
// tree. The ledger under .cg/ext/ is committed in the workspace, so whatever it
// claims (a launcher, a command) is checked against this manifest before cg
// runs anything from it or lets it claim a command. The error says why the
// manifest can't be used, ready to show after the extension's name; unless it
// wraps errBadPath, `cg init` restoring the recorded version is the fix.
func verifiedManifest(e *workspace.ExtensionRef) (dir string, m *extension.Manifest, err error) {
	sub, err := extension.CleanSubdir(e.Path)
	if err != nil {
		return "", nil, fmt.Errorf("%w %q in %s", errBadPath, e.Path, workspace.ConfigFile)
	}
	dir = filepath.Join(extension.SourceRoot(e), sub)
	m, err = extension.LoadManifest(dir)
	if err == nil {
		err = m.Validate()
	}
	if err != nil {
		return "", nil, fmt.Errorf("can't read its manifest (%w)", err)
	}
	return dir, m, nil
}

// errBadPath marks a .cg.json entry whose path is invalid: no restore fixes
// that, only correcting the entry.
var errBadPath = errors.New("invalid path")

// verifiedCommand reports whether the ledger entry c is declared, with the same
// binary, by the manifest m read from dir, and that binary exists in dir/bin.
func verifiedCommand(dir string, m *extension.Manifest, c extension.CommandContribution) bool {
	if !slices.Contains(m.Contributes.Commands, c) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "bin", c.Binary))
	return err == nil && !info.IsDir()
}

// installedCommands returns, for each extension in exts, its verified commands:
// the entries of its ledger in the workspace at root that its pinned manifest
// declares with the same binary, whose binary exists. An extension with no
// ledger, or whose tree can't be read, contributes none. An extension listed
// twice (a hand-edited .cg.json) contributes the union of what each entry
// verifies, each command once: for a refusal check, counting more is the safe
// side. A ledger that can't be read is an error, since it might declare any
// command, unless nothing incoming declares a command or the extension is
// itself being replaced (skip), when it can't conflict.
func installedCommands(root string, exts []workspace.ExtensionRef, skip map[string][]extension.CommandContribution) (map[string][]extension.CommandContribution, error) {
	out := map[string][]extension.CommandContribution{}
	anyIncoming := false
	for _, cs := range skip {
		anyIncoming = anyIncoming || len(cs) > 0
	}
	for i := range exts {
		e := &exts[i]
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil {
			if _, replaced := skip[e.Name]; replaced || !anyIncoming {
				continue
			}
			return nil, unreadableLedgerError(root, e.Name, err)
		}
		if led == nil || len(led.Commands) == 0 {
			continue
		}
		dir, m, err := verifiedManifest(e)
		if err != nil {
			continue
		}
		for _, c := range led.Commands {
			if verifiedCommand(dir, m, c) && !slices.Contains(out[e.Name], c) {
				out[e.Name] = append(out[e.Name], c)
			}
		}
	}
	return out, nil
}

// builtinCommandNames lists every name cobra resolves as a top-level cg
// command: each command's name and aliases, hidden ones included, and the
// help and completion commands cobra adds itself.
func builtinCommandNames() []string {
	names := []string{cmdHelp, "completion", "__complete"}
	for _, c := range rootCmd.Commands() {
		names = append(names, c.Name())
		names = append(names, c.Aliases...)
	}
	return names
}

// builtinFor returns the built-in command whose name is command's namespace,
// or "".
func builtinFor(command string) string {
	if ns := extension.CommandNamespace(command); slices.Contains(builtinCommandNames(), ns) {
		return ns
	}
	return ""
}

// checkCommands refuses, before anything is applied, incoming commands (keyed
// by extension name) that conflict with each other, with the verified commands
// of the extensions installed in the workspace at root, or with a built-in.
func checkCommands(root string, installed []workspace.ExtensionRef, incoming map[string][]extension.CommandContribution) error {
	have, err := installedCommands(root, installed, incoming)
	if err != nil {
		return err
	}
	conflicts := extension.CheckCommands(have, incoming, builtinCommandNames())
	if len(conflicts) == 0 {
		return nil
	}
	lines := make([]string, len(conflicts))
	for i, c := range conflicts {
		lines[i] = "  " + c.String()
	}
	return cgerr.New(cgerr.ExitUsage, "extension commands conflict, so nothing was changed:\n%s", strings.Join(lines, "\n"))
}
