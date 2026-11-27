package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
