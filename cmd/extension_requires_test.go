package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/cgerr"
	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// Tests for #212: an extension states the cg version it needs (requires.cg).
// Scenario numbers are those of specs/gh-212-extension-cg-version/spec.md.

// setVersion makes v the running cg version for the test, with the background
// update check off.
func setVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
	t.Setenv("CONSIGLIERE_AUTO_UPDATE", "0")
}

// withRequires adds "requires": {"cg": need} to a manifest.
func withRequires(manifest, need string) string {
	return strings.Replace(manifest, `"name":`, `"requires": {"cg": "`+need+`"}, "name":`, 1)
}

// wantRequirementErr fails unless err is a usage error that carries the
// requirement message for each extension in names (needing need, on have),
// one per line.
func wantRequirementErr(t *testing.T, err error, out, need, have string, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected the requirement to be refused, got success:\n%s", out)
	}
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Errorf("error %v is not a usage error", err)
	}
	lines := strings.Split(err.Error(), "\n")
	for _, n := range names {
		want := extension.RequirementMessage(n, need, have)
		found := false
		for _, l := range lines {
			found = found || l == want
		}
		if !found {
			t.Errorf("error %q has no line %q", err, want)
		}
	}
	if len(lines) != len(names) {
		t.Errorf("error has %d lines, want %d:\n%s", len(lines), len(names), err)
	}
}

// S7, U3: install refuses an extension this cg is too old for, from a direct
// URL, a --path subdirectory and with --force, and changes nothing.
func TestInstallRefusesUnmetRequirement(t *testing.T) {
	requireGit(t)
	setVersion(t, "v2.0.0-3-gabc1234-dirty")
	for _, tc := range []struct {
		name  string
		extra []string
		sub   bool
	}{
		{"direct", nil, false},
		{"force", []string{"--force"}, false},
		{"path", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			installRepo(t, cfgHome, ws, commandsManifest("other", "1.0.0", "other"))
			before := snapshotDir(t, ws)
			manifest := withRequires(commandsManifest("shop", "1.0.0", "shop"), "2.1")
			var repo string
			args := []string{"install"}
			if tc.sub {
				repo = makeMonorepo(t, map[string]string{"exts/shop": manifest})
				args = append(args, repo, "--path", "exts/shop")
			} else {
				repo = makeExtRepo(t, manifest)
				args = append(args, repo)
			}
			out, err := runExtCfg(t, cfgHome, ws, append(args, tc.extra...)...)
			wantRequirementErr(t, err, out, "2.1", "2.0.0", "shop")
			assertUnchanged(t, ws, before)
		})
	}
}

// S5, S6, S11: a met requirement, or a running version with no core,
// installs as today.
func TestInstallMetOrSkippedRequirement(t *testing.T) {
	requireGit(t)
	for _, tc := range []struct{ running, need string }{
		{"2.1.0", "2.1"},
		{"v2.1.0", "2.1.0"},
		{"v2.1.0-4-gabc1234-dirty", "2"},
		{"dev", "99"},
		{"abc1234", "99"},
	} {
		t.Run(tc.running, func(t *testing.T) {
			setVersion(t, tc.running)
			cfgHome, ws := t.TempDir(), newWorkspace(t)
			installRepo(t, cfgHome, ws, withRequires(commandsManifest("shop", "1.0.0", "shop"), tc.need))
			ledgerOf(t, ws, "shop")
		})
	}
}

// S8, S10: update refuses before applying any target when a target's new
// version needs a newer cg, naming every such target; extensions that aren't
// targets aren't checked.
func TestUpdateRefusesUnmetRequirement(t *testing.T) {
	requireGit(t)
	setup := func(t *testing.T) (cfgHome, ws string, repos map[string]string) {
		t.Helper()
		setVersion(t, "2.0.0")
		cfgHome, ws, repos = t.TempDir(), newWorkspace(t), map[string]string{}
		for _, n := range []string{"alpha", "beta", "gamma"} {
			repos[n] = installRepo(t, cfgHome, ws, commandsManifest(n, "1.0.0", n))
		}
		return cfgHome, ws, repos
	}
	t.Run("one unmet target of all", func(t *testing.T) {
		cfgHome, ws, repos := setup(t)
		updateExtRepo(t, repos["alpha"], commandsManifest("alpha", "1.1.0", "alpha"), "")
		updateExtRepo(t, repos["beta"], withRequires(commandsManifest("beta", "1.1.0", "beta"), "2.0.1"), "")
		before := snapshotDir(t, ws)
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantRequirementErr(t, err, out, "2.0.1", "2.0.0", "beta")
		assertUnchanged(t, ws, before)
	})
	t.Run("two unmet", func(t *testing.T) {
		cfgHome, ws, repos := setup(t)
		updateExtRepo(t, repos["alpha"], withRequires(commandsManifest("alpha", "1.1.0", "alpha"), "2.1"), "")
		updateExtRepo(t, repos["gamma"], withRequires(commandsManifest("gamma", "1.1.0", "gamma"), "2.1"), "")
		before := snapshotDir(t, ws)
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantRequirementErr(t, err, out, "2.1", "2.0.0", "alpha", "gamma")
		assertUnchanged(t, ws, before)
	})
	t.Run("the one target", func(t *testing.T) {
		cfgHome, ws, repos := setup(t)
		updateExtRepo(t, repos["beta"], withRequires(commandsManifest("beta", "1.1.0", "beta"), "3"), "")
		before := snapshotDir(t, ws)
		out, err := runExtCfg(t, cfgHome, ws, "update", "beta")
		wantRequirementErr(t, err, out, "3", "2.0.0", "beta")
		assertUnchanged(t, ws, before)
	})
	t.Run("a neighbour isn't checked", func(t *testing.T) {
		cfgHome, ws, repos := setup(t)
		// beta's next version, and the one installed, need a newer cg; alpha,
		// the target, doesn't.
		updateExtRepo(t, repos["beta"], withRequires(commandsManifest("beta", "1.1.0", "beta"), "3"), "")
		updateExtRepo(t, repos["alpha"], commandsManifest("alpha", "1.1.0", "alpha"), "")
		if out, err := runExtCfg(t, cfgHome, ws, "update", "alpha"); err != nil {
			t.Fatalf("update alpha: %v\n%s", err, out)
		}
		cfg, _ := workspace.Detect(ws)
		if got := findRef(cfg.Extensions, "alpha").Version; got != "1.1.0" {
			t.Errorf("alpha at %s, want 1.1.0", got)
		}
	})
}

// S9, S10: cg init restores no extension when one it re-installs needs a
// newer cg, removes the trees it fetched, and restores them all once cg is
// new enough. An extension whose tree is present isn't checked.
func TestInitRefusesUnmetRequirement(t *testing.T) {
	requireGit(t)
	install := func(t *testing.T) (cfgHome, ws string) {
		t.Helper()
		setVersion(t, "2.1.0")
		cfgHome, ws = t.TempDir(), newWorkspace(t)
		installRepo(t, cfgHome, ws, withRequires(commandsManifest("alpha", "1.0.0", "alpha"), "2.1"))
		installRepo(t, cfgHome, ws, withRequires(commandsManifest("beta", "1.0.0", "beta"), "2.1"))
		installRepo(t, cfgHome, ws, commandsManifest("gamma", "1.0.0", "gamma"))
		return cfgHome, ws
	}
	t.Run("missing", func(t *testing.T) {
		cfgHome, ws := install(t)
		cfg, _ := workspace.Detect(ws)
		refs := append([]workspace.ExtensionRef(nil), cfg.Extensions...)
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"alpha", "beta", "gamma"} {
			if err := os.Remove(filepath.Join(ws, "notes", n+".md")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(extension.LedgerPath(ws, n)); err != nil {
				t.Fatal(err)
			}
		}
		Version = "2.0.0"
		out, err := runInitForce(t, cfgHome, ws)
		wantRequirementErr(t, err, out, "2.1", "2.0.0", "alpha", "beta")
		for i := range refs {
			n := refs[i].Name
			if _, serr := os.Lstat(extension.LedgerPath(ws, n)); !errors.Is(serr, fs.ErrNotExist) {
				t.Errorf("%s was applied despite the refusal (%v)", n, serr)
			}
			if _, serr := os.Lstat(extension.SourceRoot(&refs[i])); !errors.Is(serr, fs.ErrNotExist) {
				t.Errorf("%s's tree left behind by the refused init (%v)", n, serr)
			}
		}
		Version = "2.1.0"
		if out, err := runInitForce(t, cfgHome, ws); err != nil {
			t.Fatalf("init on a new enough cg: %v\n%s", err, out)
		}
		for _, n := range []string{"alpha", "beta", "gamma"} {
			ledgerOf(t, ws, n)
		}
	})
	t.Run("present", func(t *testing.T) {
		cfgHome, ws := install(t)
		Version = "2.0.0"
		if out, err := runInitForce(t, cfgHome, ws); err != nil {
			t.Fatalf("init with present trees: %v\n%s", err, out)
		}
	})
}

// unmetWorkspace installs alpha (needs cg 2.1, declares commands) into a
// fresh workspace on cg 2.1.0, makes it the current directory, and then makes
// the running cg 2.0.0, so alpha's requirement is unmet.
func unmetWorkspace(t *testing.T, cfgHome string, commands ...string) string {
	t.Helper()
	setVersion(t, "2.1.0")
	ws := newWorkspace(t)
	installRepo(t, cfgHome, ws, withRequires(commandsManifest("alpha", "1.0.0", commands...), "2.1"))
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Chdir(ws)
	Version = "2.0.0"
	return ws
}

// wantRequirementStop fails unless dispatching args stops with S13's message
// for alpha's command.
func wantRequirementStop(t *testing.T, args []string, command string) {
	t.Helper()
	run, err := externalDispatch(args)
	if err == nil {
		t.Fatalf("externalDispatch(%q) = %+v, want the requirement refused", args, run)
	}
	var coded *cgerr.CodedError
	if !errors.As(err, &coded) || coded.ExitCode() != cgerr.ExitUsage {
		t.Errorf("error %v is not a usage error", err)
	}
	prefix := "cg " + command + `: extension "alpha" (installed in `
	suffix := ") can't run it: it needs cg 2.1 or newer; this is cg 2.0.0. " + extension.UpdateHint
	if msg := err.Error(); !strings.HasPrefix(msg, prefix) || !strings.HasSuffix(msg, suffix) || strings.Contains(msg, "\n") {
		t.Errorf("error %q\nwant %q<root>%q", msg, prefix, suffix)
	}
}

// S13: an otherwise verified entry of an extension this cg is too old for
// stops dispatch with the requirement, in the deciding root, without falling
// back to PATH; a shorter entry of another extension doesn't stand in for it.
func TestDispatchStopsOnUnmetRequirement(t *testing.T) {
	requireGit(t)
	t.Run("own entry", func(t *testing.T) {
		unmetWorkspace(t, t.TempDir(), "shop", "shop list")
		pathProgram(t, "shop")
		wantRequirementStop(t, []string{"shop", "list", "x"}, "shop list")
		wantRequirementStop(t, []string{"shop"}, "shop")
		wantRequirementStop(t, []string{"shop", "add"}, "shop")
	})
	t.Run("shorter entry of another extension", func(t *testing.T) {
		// Nested commands of two extensions are refused at install, so the
		// state comes from a pulled .cg.json and ledger: beta declares shop.
		cfgHome := t.TempDir()
		ws := unmetWorkspace(t, cfgHome, "shop list")
		installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "other"))
		updateManifestInTree(t, ws, "beta", commandsManifest("beta", "1.0.0", "other", "shop"))
		ledgerAdd(t, ws, "beta", extension.CommandContribution{Command: "shop", Binary: "cg-beta"})
		pathProgram(t, "shop")
		wantRequirementStop(t, []string{"shop", "list", "x"}, "shop list")
		// beta's own command still runs.
		if run := mustDispatch(t, "shop", "add"); run.command != "shop" || run.path != binOf(t, ws, "beta") {
			t.Errorf("cg shop add ran %+v, want beta's shop", run)
		}
	})
	t.Run("a damaged tree's fix comes first", func(t *testing.T) {
		ws := unmetWorkspace(t, t.TempDir(), "shop")
		if err := os.Remove(binOf(t, ws, "alpha")); err != nil {
			t.Fatal(err)
		}
		dispatchErr(t, []string{"shop"}, `"alpha"`, "is missing", "cg init --force")
		if _, err := externalDispatch([]string{"shop"}); err != nil && strings.Contains(err.Error(), "needs cg") {
			t.Errorf("a missing binary reported as the requirement: %v", err)
		}
	})
	t.Run("development build", func(t *testing.T) {
		ws := unmetWorkspace(t, t.TempDir(), "shop")
		Version = "dev"
		if run := mustDispatch(t, "shop"); run.path != binOf(t, ws, "alpha") {
			t.Errorf("ran %+v, want alpha's binary", run)
		}
	})
}

// S12: an extension this cg is too old for still owns its commands for the
// conflict check, and doesn't stop installing an unrelated extension.
func TestUnmetRequirementCountsAsVerifiedForConflicts(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	ws := unmetWorkspace(t, cfgHome, "shop list")
	installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "other"))
	before := snapshotDir(t, ws)
	repo := makeExtRepo(t, commandsManifest("gamma", "1.0.0", "shop list"))
	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	wantConflict(t, err, out, `"shop list"`, `"alpha"`, `"gamma"`)
	assertUnchanged(t, ws, before)
}

// captureStderr returns what fn writes to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = old }()
	fn()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// S13b: the workspace's launcher, from an extension this cg is too old for,
// is skipped with a warning, and the agent starts directly.
func TestStartSkipsLauncherWithUnmetRequirement(t *testing.T) {
	ws, exe := launcherWorkspace(t)
	installLauncher(t, ws, "tmux", "cg-tmux", "cg-tmux", nil, true)
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(extension.SourceRoot(findRef(cfg.Extensions, "tmux")), extension.ManifestFile)
	data, err := os.ReadFile(p) //nolint:gosec // the test's own store
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(withRequires(string(data), "2.1")), 0o644); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "2.0.0")
	s := newStubStart(t, true)
	stderr := captureStderr(t, func() {
		if err := startCmd.RunE(startCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if s.path != exe || !reflect.DeepEqual(s.argv, []string{exe}) {
		t.Errorf("want the agent directly, got %+v", s)
	}
	want := "cg start: extension tmux: needs cg 2.1 or newer; this is cg 2.0.0; " +
		"Update cg (cg update upgrade, or brew upgrade --cask cg), then run this again; starting the agent without it\n"
	if stderr != want {
		t.Errorf("stderr = %q\nwant %q", stderr, want)
	}

	// On a new enough cg the launcher runs.
	Version = "2.1.0"
	s = newStubStart(t, true)
	if err := startCmd.RunE(startCmd, nil); err != nil {
		t.Fatal(err)
	}
	if s.path == exe {
		t.Error("the launcher didn't run on a new enough cg")
	}
}

// S13c: cg extension list shows an unmet requirement, in text and --json,
// with or without a ledger, and still lists the commands as verified.
func TestExtListShowsUnmetRequirement(t *testing.T) {
	requireGit(t)
	cfgHome := t.TempDir()
	ws := unmetWorkspace(t, cfgHome, "shop")
	installRepo(t, cfgHome, ws, commandsManifest("beta", "1.0.0", "other"))
	check := func(t *testing.T, withLedger bool) {
		t.Helper()
		out, err := runExtCfg(t, cfgHome, ws, "list")
		if err != nil {
			t.Fatalf("list: %v\n%s", err, out)
		}
		if strings.Count(out, "  needs cg 2.1 or newer; this is cg 2.0.0\n") != 1 {
			t.Errorf("list text lacks the requirement line once:\n%s", out)
		}
		if withLedger && (!strings.Contains(out, "  commands: shop\n") || strings.Contains(out, "not verified")) {
			t.Errorf("alpha's command not listed as verified:\n%s", out)
		}
		out, err = runExtCfg(t, cfgHome, ws, "list", "--json")
		if err != nil {
			t.Fatalf("list --json: %v", err)
		}
		var listed []struct {
			Name             string `json:"name"`
			RequirementUnmet string `json:"requirementUnmet"`
			Commands         []struct {
				Unverified string `json:"unverified"`
			} `json:"commands"`
		}
		if err := json.Unmarshal([]byte(out), &listed); err != nil {
			t.Fatalf("list --json: %v\n%s", err, out)
		}
		for _, l := range listed {
			want := ""
			if l.Name == "alpha" {
				want = extension.RequirementMessage("alpha", "2.1", "2.0.0")
			}
			if l.RequirementUnmet != want {
				t.Errorf("%s requirementUnmet = %q, want %q", l.Name, l.RequirementUnmet, want)
			}
			for _, c := range l.Commands {
				if c.Unverified != "" {
					t.Errorf("%s command unverified: %q", l.Name, c.Unverified)
				}
			}
		}
		if strings.Count(out, `"requirementUnmet"`) != 1 {
			t.Errorf("requirementUnmet not omitted when met:\n%s", out)
		}
	}
	t.Run("ledger", func(t *testing.T) { check(t, true) })
	t.Run("no ledger", func(t *testing.T) {
		if err := os.Remove(extension.LedgerPath(ws, "alpha")); err != nil {
			t.Fatal(err)
		}
		check(t, false)
	})
}

// U2: git-hook trust doesn't check the requirement, so an extension this cg
// is too old for keeps its git hooks approved.
func TestGitHookTrustIgnoresRequirement(t *testing.T) {
	requireGit(t)
	setVersion(t, "2.1.0")
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, `{"manifest": 1, "requires": {"cg": "2.1"}, "name": "hooks", "version": "1.0.0", "description": "d",
	  "contributes": {"git-hooks": [{"hook": "post-commit", "script": "hooks/a.sh"}]}}`)
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	Version = "2.0.0"
	if got := extension.TrustedGitHookScripts(context.Background(), ws, cfg.Extensions); len(got) != 1 {
		t.Errorf("trusted = %v, want the extension's one script", got)
	}
}
