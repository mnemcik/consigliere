package cmd

import (
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
// manifest can't be used, ready to show after the extension's name.
func verifiedManifest(e *workspace.ExtensionRef) (dir string, m *extension.Manifest, err error) {
	sub, err := extension.CleanSubdir(e.Path)
	if err != nil {
		return "", nil, fmt.Errorf("invalid path %q", e.Path)
	}
	dir = filepath.Join(extension.SourceRoot(e), sub)
	m, err = extension.LoadManifest(dir)
	if err == nil {
		err = m.Validate()
	}
	if err != nil {
		return "", nil, fmt.Errorf("can't read its manifest (%w); `cg init` restores the recorded version", err)
	}
	return dir, m, nil
}

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
// ledger, or whose tree can't be read, contributes none, and an entry listed
// twice (a hand-edited .cg.json) counts once.
func installedCommands(root string, exts []workspace.ExtensionRef) map[string][]extension.CommandContribution {
	out := map[string][]extension.CommandContribution{}
	for i := range exts {
		e := &exts[i]
		if _, seen := out[e.Name]; seen {
			continue
		}
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil || led == nil || len(led.Commands) == 0 {
			continue
		}
		dir, m, err := verifiedManifest(e)
		if err != nil {
			continue
		}
		for _, c := range led.Commands {
			if verifiedCommand(dir, m, c) {
				out[e.Name] = append(out[e.Name], c)
			}
		}
	}
	return out
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
	conflicts := extension.CheckCommands(installedCommands(root, installed), incoming, builtinCommandNames())
	if len(conflicts) == 0 {
		return nil
	}
	lines := make([]string, len(conflicts))
	for i, c := range conflicts {
		lines[i] = "  " + c.String()
	}
	return cgerr.New(cgerr.ExitUsage, "extension commands conflict, so nothing was changed:\n%s", strings.Join(lines, "\n"))
}
