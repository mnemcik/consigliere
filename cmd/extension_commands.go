package cmd

import (
	"errors"
	"fmt"
	"io"
	"maps"
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
// command, unless nothing incoming declares a command (anyIncoming false), when
// it can't conflict.
func installedCommands(root string, exts []workspace.ExtensionRef, anyIncoming bool) (map[string][]extension.CommandContribution, error) {
	out := map[string][]extension.CommandContribution{}
	for i := range exts {
		e := &exts[i]
		led, err := extension.LoadLedger(root, e.Name)
		if err != nil {
			if !anyIncoming {
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
// A built-in-namespace command the extension already owns (its installed
// version's manifest at the recorded commit declares it) isn't refused but
// returned as a warning; restoring (cg init, which restores the recorded
// versions) owns every command it restores, so it only warns. Each incoming
// extension's own ledger must be readable too: applying it reads that ledger,
// and failing there would leave a multi-extension operation half applied.
func checkCommands(root string, installed []workspace.ExtensionRef, incoming map[string][]extension.CommandContribution, restoring bool) ([]string, error) {
	anyIncoming := false
	for _, name := range slices.Sorted(maps.Keys(incoming)) {
		if _, err := extension.LoadLedger(root, name); err != nil {
			return nil, unreadableLedgerError(root, name, err)
		}
		anyIncoming = anyIncoming || len(incoming[name]) > 0
	}
	have, err := installedCommands(root, installed, anyIncoming)
	if err != nil {
		return nil, err
	}
	owned := incoming
	if !restoring {
		owned = ownedCommands(installed, incoming)
	}
	conflicts, shadowed := extension.CheckCommands(have, incoming, owned, builtinCommandNames())
	var warnings []string
	for _, c := range shadowed {
		warnings = append(warnings, fmt.Sprintf("command %q of extension %q can't run: the built-in \"cg %s\" wins", c.Command, c.Extension, c.Builtin))
	}
	if len(conflicts) == 0 {
		return warnings, nil
	}
	lines := make([]string, len(conflicts))
	for i, c := range conflicts {
		lines[i] = "  " + c.String()
	}
	return nil, cgerr.New(cgerr.ExitUsage, "extension commands conflict, so nothing was changed:\n%s", strings.Join(lines, "\n"))
}

// ownedCommands returns, for each incoming extension that is installed, the
// commands it owns: those its installed version's manifest, read and validated
// at the recorded commit, declares. The ledger isn't consulted, and a missing
// binary doesn't matter; an extension whose tree can't be read owns nothing.
func ownedCommands(installed []workspace.ExtensionRef, incoming map[string][]extension.CommandContribution) map[string][]extension.CommandContribution {
	out := map[string][]extension.CommandContribution{}
	for i := range installed {
		e := &installed[i]
		if _, in := incoming[e.Name]; !in {
			continue
		}
		if _, m, err := verifiedManifest(e); err == nil {
			out[e.Name] = append(out[e.Name], m.Contributes.Commands...)
		}
	}
	return out
}

// printCommandWarnings writes checkCommands' warnings.
func printCommandWarnings(w io.Writer, warnings []string) {
	for _, m := range warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", m)
	}
}
