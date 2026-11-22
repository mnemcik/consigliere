package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// commandsManifest is a v2 manifest for extension name at version, declaring
// commands served by bin/cg-<name>, and one note, so a test can tell whether
// anything was applied.
func commandsManifest(name, version string, commands ...string) string {
	entries := make([]string, len(commands))
	for i, c := range commands {
		entries[i] = fmt.Sprintf(`{ "command": %q, "binary": "cg-%s" }`, c, name)
	}
	return fmt.Sprintf(`{
  "manifest": 2,
  "name": %[1]q,
  "version": %[2]q,
  "description": "declares commands",
  "contributes": {
    "notes": [{ "src": "notes/%[1]s.md", "dest": "notes/%[1]s.md" }],
    "commands": [%[3]s]
  }
}`, name, version, strings.Join(entries, ", "))
}

// snapshotDir maps every file under dir (slash paths) to its content.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test's own temp workspace
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertUnchanged(t *testing.T, ws string, before map[string]string) {
	t.Helper()
	after := snapshotDir(t, ws)
	for p, b := range before {
		if a, ok := after[p]; !ok {
			t.Errorf("%s was removed", p)
		} else if a != b {
			t.Errorf("%s changed:\n%s\n---\n%s", p, b, a)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("%s was added", p)
		}
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available")
	}
}

// installRepo installs a fresh repo with manifest into ws and returns the repo.
func installRepo(t *testing.T, cfgHome, ws, manifest string) string {
	t.Helper()
	repo := makeExtRepo(t, manifest)
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return repo
}

func wantConflict(t *testing.T, err error, out string, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a conflict, got success:\n%s", out)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not name %q", err, p)
		}
	}
}

// S6: a command another installed extension declares is refused, and nothing
// in the workspace changes.
func TestInstallRefusesADeclaredCommand(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	before := snapshotDir(t, ws)

	repo := makeExtRepo(t, commandsManifest("beta", "1.0.0", "shop list", "shop add"))
	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	wantConflict(t, err, out, `"shop add"`, `"alpha"`, `"beta"`)
	assertUnchanged(t, ws, before)
}

// S7: nesting is a conflict in either direction.
func TestInstallRefusesNestedCommands(t *testing.T) {
	requireGit(t)
	for _, tc := range []struct{ installed, incoming string }{
		{"pr", "pr merge"},
		{"pr merge", "pr"},
	} {
		t.Run(tc.incoming, func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", tc.installed))
			before := snapshotDir(t, ws)
			repo := makeExtRepo(t, commandsManifest("beta", "1.0.0", tc.incoming))
			out, err := runExtCfg(t, cfgHome, ws, "install", repo)
			wantConflict(t, err, out, fmt.Sprintf("%q", tc.installed), fmt.Sprintf("%q", tc.incoming), `"alpha"`, `"beta"`)
			assertUnchanged(t, ws, before)
		})
	}
}

// S8: two extensions may share a namespace with different commands.
func TestInstallSharesANamespace(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "pr merge"))
	installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "pr watch"))
	for name, want := range map[string]string{"alpha": "pr merge", "beta": "pr watch"} {
		led := ledgerOf(t, ws, name)
		if len(led.Commands) != 1 || led.Commands[0].Command != want {
			t.Errorf("%s ledger commands = %+v, want %q", name, led.Commands, want)
		}
	}
}

func ledgerOf(t *testing.T, ws, name string) *extension.Ledger {
	t.Helper()
	led, err := extension.LoadLedger(ws, name)
	if err != nil || led == nil {
		t.Fatalf("ledger %s: %v %v", name, led, err)
	}
	return led
}

// S9: a command in a built-in's namespace is refused.
func TestInstallRefusesABuiltinNamespace(t *testing.T) {
	requireGit(t)
	for _, c := range []string{"start now", "extension x", "ext", "help me", "completion"} {
		t.Run(c, func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			before := snapshotDir(t, ws)
			repo := makeExtRepo(t, commandsManifest("beta", "1.0.0", c))
			out, err := runExtCfg(t, cfgHome, ws, "install", repo)
			wantConflict(t, err, out, fmt.Sprintf("%q", c), "built-in", fmt.Sprintf("cg %s", extension.CommandNamespace(c)))
			assertUnchanged(t, ws, before)
		})
	}
}

// The built-in list covers every top-level command name and alias, hidden
// commands included, and the commands cobra adds itself.
func TestBuiltinCommandNames(t *testing.T) {
	hidden := &cobra.Command{Use: "zz-hidden-test", Aliases: []string{"zz-alias-test"}, Hidden: true, Run: func(*cobra.Command, []string) {}}
	rootCmd.AddCommand(hidden)
	t.Cleanup(func() { rootCmd.RemoveCommand(hidden) })
	got := builtinCommandNames()
	for _, want := range []string{"start", "extension", "ext", "worktree", "version", "help", "completion", "__complete", "zz-hidden-test", "zz-alias-test"} {
		if !slices.Contains(got, want) {
			t.Errorf("built-in names %v lack %q", got, want)
		}
	}
}

// S12: re-installing or updating an extension that keeps its command is not a
// conflict with itself.
func TestReinstallAndUpdateKeepOwnCommands(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	repo := installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop", "shop add"))
	if out, err := runExtCfg(t, cfgHome, ws, "install", repo); err != nil {
		t.Fatalf("reinstall: %v\n%s", err, out)
	}
	updateExtRepo(t, repo, commandsManifest("alpha", "1.1.0", "shop", "shop add", "shop list"), "")
	if out, err := runExtCfg(t, cfgHome, ws, "update", "alpha"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if led := ledgerOf(t, ws, "alpha"); len(led.Commands) != 3 {
		t.Errorf("ledger commands after update = %+v", led.Commands)
	}
}

// S13: a removed extension's commands are free.
func TestRemoveFreesCommands(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	if out, err := runExtCfg(t, cfgHome, ws, "remove", "alpha"); err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "shop add"))
}

// The check reads installed extensions' verified commands: a ledger entry the
// pinned manifest doesn't declare (a hand-edited ledger) doesn't block an
// install.
func TestInstallChecksVerifiedCommands(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	led := ledgerOf(t, ws, "alpha")
	led.Commands = append(led.Commands, extension.CommandContribution{Command: "pr", Binary: "cg-alpha"})
	if err := led.Save(ws); err != nil {
		t.Fatal(err)
	}
	installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "pr"))
}

// S10: an update whose commands conflict fails before any target is applied.
func TestUpdateRefusesConflictingCommands(t *testing.T) {
	requireGit(t)
	cases := []struct {
		name    string
		args    []string
		alphaV2 string // "" leaves alpha's repo alone
		betaV2  string
		parts   []string
	}{
		{
			name:   "one target against an extension not updated",
			args:   []string{"update", "beta"},
			betaV2: commandsManifest("beta", "1.1.0", "pr watch", "shop add"),
			parts:  []string{`"shop add"`, `"alpha"`, `"beta"`},
		},
		{
			name:   "all targets against an extension not changed",
			args:   []string{"update"},
			betaV2: commandsManifest("beta", "1.1.0", "shop"),
			parts:  []string{`"shop"`, `"shop add"`},
		},
		{
			name:    "two targets against each other",
			args:    []string{"update"},
			alphaV2: commandsManifest("alpha", "1.1.0", "shop add", "x y"),
			betaV2:  commandsManifest("beta", "1.1.0", "pr watch", "x"),
			parts:   []string{`"x y"`, `"x"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			alpha := installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
			beta := installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "pr watch"))
			if tc.alphaV2 != "" {
				// A non-conflicting change too, so a partial apply would show.
				updateExtRepo(t, alpha, tc.alphaV2, "")
			} else {
				commitFile(t, alpha, "notes/alpha.md", "alpha v1.0.1 note")
			}
			updateExtRepo(t, beta, tc.betaV2, "")
			before := snapshotDir(t, ws)
			out, err := runExtCfg(t, cfgHome, ws, tc.args...)
			wantConflict(t, err, out, tc.parts...)
			assertUnchanged(t, ws, before)
		})
	}
}

// S10: moving a command from one extension to another in one update succeeds,
// whichever of them .cg.json lists first.
func TestUpdateMovesACommandBetweenExtensions(t *testing.T) {
	requireGit(t)
	for _, order := range [][]string{{"alpha", "beta"}, {"beta", "alpha"}} {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			manifests := map[string]string{
				"alpha": commandsManifest("alpha", "1.0.0", "pr merge"),
				"beta":  commandsManifest("beta", "1.0.0", "pr watch"),
			}
			repos := map[string]string{}
			for _, n := range order {
				repos[n] = installRepo(t, cfgHome, ws, manifests[n])
			}
			updateExtRepo(t, repos["alpha"], commandsManifest("alpha", "1.1.0", "pr list"), "")
			updateExtRepo(t, repos["beta"], commandsManifest("beta", "1.1.0", "pr watch", "pr merge"), "")
			if out, err := runExtCfg(t, cfgHome, ws, "update"); err != nil {
				t.Fatalf("update: %v\n%s", err, out)
			}
			if led := ledgerOf(t, ws, "beta"); len(led.Commands) != 2 || led.Commands[1].Command != "pr merge" {
				t.Errorf("beta commands = %+v", led.Commands)
			}
			if led := ledgerOf(t, ws, "alpha"); len(led.Commands) != 1 || led.Commands[0].Command != "pr list" {
				t.Errorf("alpha commands = %+v", led.Commands)
			}
		})
	}
}

// runInitForce runs `cg init --force` in ws with config home cfgHome.
func runInitForce(t *testing.T, cfgHome, ws string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Chdir(ws)
	forceInit = true
	t.Cleanup(func() { forceInit = false })
	buf := new(bytes.Buffer)
	rootCmd.SetArgs([]string{"init", "--force"})
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	err := rootCmd.Execute()
	return buf.String(), err
}

// conflictingWorkspace returns a workspace whose .cg.json records alpha
// ("shop add"), beta ("shop add") and gamma ("other") at commits installed in
// separate workspaces, as a hand-edited or pulled .cg.json could. It returns
// the entries and the ledgers those installs wrote.
func conflictingWorkspace(t *testing.T, cfgHome string) (ws string, refs map[string]workspace.ExtensionRef, ledgers map[string][]byte) {
	t.Helper()
	refs, ledgers = map[string]workspace.ExtensionRef{}, map[string][]byte{}
	for _, e := range []struct{ name, cmd string }{{"alpha", "shop add"}, {"beta", "shop add"}, {"gamma", "other"}} {
		var data []byte
		src := newWorkspace(t)
		installRepo(t, cfgHome, src, commandsManifest(e.name, "1.0.0", e.cmd))
		cfg, err := workspace.Detect(src)
		if err != nil {
			t.Fatal(err)
		}
		refs[e.name] = *findRef(cfg.Extensions, e.name)
		data, err = os.ReadFile(extension.LedgerPath(src, e.name))
		if err != nil {
			t.Fatal(err)
		}
		ledgers[e.name] = data
	}
	ws = newWorkspace(t)
	cfg, _ := workspace.Detect(ws)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		r := refs[n]
		cfg.UpsertExtension(&r)
	}
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	return ws, refs, ledgers
}

// S11: `cg init` checks every re-installed extension, and those already
// present, before applying any; on a conflict it reports every conflict and
// applies none.
func TestInitRefusesConflictingReinstalls(t *testing.T) {
	requireGit(t)
	t.Run("all missing", func(t *testing.T) {
		cfgHome := t.TempDir()
		ws, _, _ := conflictingWorkspace(t, cfgHome)
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		out, err := runInitForce(t, cfgHome, ws)
		wantConflict(t, err, out, `"shop add"`, `"alpha"`, `"beta"`)
		for _, n := range []string{"alpha", "beta", "gamma"} {
			if _, serr := os.Stat(filepath.Join(ws, "notes", n+".md")); serr == nil {
				t.Errorf("%s was applied despite the conflict", n)
			}
			if led, _ := extension.LoadLedger(ws, n); led != nil {
				t.Errorf("%s has a ledger despite the conflict", n)
			}
		}
	})
	t.Run("one present", func(t *testing.T) {
		cfgHome := t.TempDir()
		ws, refs, ledgers := conflictingWorkspace(t, cfgHome)
		// alpha is installed here (its tree is present and its ledger
		// recorded); beta and gamma are missing.
		if err := os.MkdirAll(filepath.Dir(extension.LedgerPath(ws, "alpha")), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(extension.LedgerPath(ws, "alpha"), ledgers["alpha"], 0o644); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"beta", "gamma"} {
			r := refs[n]
			if err := os.RemoveAll(extension.SourceRoot(&r)); err != nil {
				t.Fatal(err)
			}
		}
		out, err := runInitForce(t, cfgHome, ws)
		wantConflict(t, err, out, `"shop add"`, `"alpha"`, `"beta"`)
		for _, n := range []string{"beta", "gamma"} {
			if led, _ := extension.LoadLedger(ws, n); led != nil {
				t.Errorf("%s has a ledger despite the conflict", n)
			}
		}
	})
}

// S14: list shows each extension's commands, and flags one whose namespace is
// now a built-in.
func TestListShowsCommands(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop", "shop add"))
	// An extension installed by an older cg, before `start` was a built-in.
	cfg, _ := workspace.Detect(ws)
	cfg.UpsertExtension(&workspace.ExtensionRef{Name: "old", Version: "0.1.0", Source: workspace.ExtSourceDirect, Repo: "x"})
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := (&extension.Ledger{Name: "old", Version: "0.1.0",
		Commands: []extension.CommandContribution{{Command: "start x", Binary: "cg-old"}}}).Save(ws); err != nil {
		t.Fatal(err)
	}

	out, err := runExtCfg(t, cfgHome, ws, "list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	for _, want := range []string{"commands: shop, shop add", `commands: start x (can't run: the built-in "cg start" wins)`} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}

	out, err = runExtCfg(t, cfgHome, ws, "list", "--json")
	if err != nil {
		t.Fatalf("list --json: %v\n%s", err, out)
	}
	var got []struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Commands []struct {
			Command string `json:"command"`
			Binary  string `json:"binary"`
			Builtin string `json:"shadowedByBuiltin"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("list --json: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[0].Version != "1.0.0" || len(got[0].Commands) != 2 ||
		got[0].Commands[1].Command != "shop add" || got[0].Commands[1].Binary != "cg-alpha" || got[0].Commands[0].Builtin != "" {
		t.Errorf("alpha in --json: %+v", got)
	}
	if len(got) == 2 && (len(got[1].Commands) != 1 || got[1].Commands[0].Builtin != "start") {
		t.Errorf("old in --json: %+v", got[1])
	}
}

// A .cg.json that lists an extension twice (hand-edited) reports each
// conflict once.
func TestInstallConflictWithDuplicateEntryReportedOnce(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, commandsManifest("alpha", "1.0.0", "shop add"))
	cfg, _ := workspace.Detect(ws)
	cfg.Extensions = append(cfg.Extensions, cfg.Extensions[0])
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
	repo := makeExtRepo(t, commandsManifest("beta", "1.0.0", "shop add"))
	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	wantConflict(t, err, out, `"shop add"`)
	if n := strings.Count(err.Error(), `already declared by extension "alpha"`); n != 1 {
		t.Errorf("conflict reported %d times, want once:\n%v", n, err)
	}
}
