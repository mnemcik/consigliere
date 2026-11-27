package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/gitx"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// skillsManifest is a v2 manifest for extension name at version shipping one
// skill per name in skills (from skills/<skill>/), and a note.
func skillsManifest(name, version string, skills ...string) string {
	entries := make([]string, len(skills))
	for i, s := range skills {
		entries[i] = fmt.Sprintf(`{ "src": "skills/%s" }`, s)
	}
	return fmt.Sprintf(`{
  "manifest": 2,
  "name": %[1]q,
  "version": %[2]q,
  "description": "ships skills",
  "contributes": {
    "notes": [{ "src": "notes/%[1]s.md", "dest": "notes/%[1]s.md" }],
    "skills": [%[3]s]
  }
}`, name, version, strings.Join(entries, ", "))
}

// releaseExtRepo rewrites the repo's manifest, adds the stub files it needs,
// and commits.
func releaseExtRepo(t *testing.T, repo, manifest string) {
	t.Helper()
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(repo, "cg-extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReferencedStubs(t, repo, manifest)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "release"}} {
		if _, err := gitx.Run(ctx, repo, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
}

func setTargets(t *testing.T, ws string, targets ...string) {
	t.Helper()
	cfg, err := workspace.Detect(ws)
	if err != nil {
		t.Fatal(err)
	}
	if targets == nil {
		targets = []string{}
	}
	cfg.Skills = &workspace.SkillsConfig{Targets: &targets}
	if err := cfg.Save(ws); err != nil {
		t.Fatal(err)
	}
}

// S13, S18: an installed skill lands under .claude/skills and is listed.
func TestInstallSkill(t *testing.T) {
	requireGit(t)
	home := t.TempDir() // no user-scope skill of the same name (S26)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	repo := makeExtRepo(t, skillsManifest("alpha", "1.0.0", "digest"))
	out, err := runExtCfg(t, cfgHome, ws, "install", repo)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	mustExist(t, filepath.Join(ws, ".claude", "skills", "digest", "SKILL.md"))
	if !strings.Contains(out, "1 skill(s)") || strings.Contains(out, "warning") {
		t.Errorf("summary: %s", out)
	}
	led := ledgerOf(t, ws, "alpha")
	if len(led.Skills) != 1 || led.Skills[0].Name != "digest" {
		t.Errorf("ledger: %+v", led.Skills)
	}
	out, err = runExtCfg(t, cfgHome, ws, "list")
	if err != nil || !strings.Contains(out, "skills: digest (claude)") {
		t.Errorf("list: %v\n%s", err, out)
	}
}

// S19, S21: the same skill name from two extensions conflicts for a shared
// target only.
func TestInstallRefusesASkillAnotherShips(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	before := snapshotDir(t, ws)
	out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, skillsManifest("beta", "1.0.0", "digest")))
	wantConflict(t, err, out, `"digest"`, `"alpha"`, `"beta"`, "claude")
	assertUnchanged(t, ws, before)

	// alpha installed for claude only; beta for agents only: no conflict.
	setTargets(t, ws, "agents")
	if out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, skillsManifest("beta", "1.0.0", "digest"))); err != nil {
		t.Fatalf("install for another target: %v\n%s", err, out)
	}
	mustExist(t, filepath.Join(ws, ".agents", "skills", "digest", "SKILL.md"))
}

// S16: an unknown target refuses install.
func TestInstallRefusesUnknownTarget(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	setTargets(t, ws, "nope")
	out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, skillsManifest("alpha", "1.0.0", "digest")))
	wantConflict(t, err, out, `"nope"`, "claude, agents")
	// Also for an extension that ships no skill.
	out, err = runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, testManifest))
	wantConflict(t, err, out, `"nope"`)
}

// S20: a framework skill or command name is refused for claude, not for
// agents; S20b: one an extension already ships, reserved by a later cg, warns.
func TestReservedSkillNames(t *testing.T) {
	requireGit(t)
	for _, name := range []string{"wrap", "cg-sync"} {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, skillsManifest("alpha", "1.0.0", name)))
		wantConflict(t, err, out, `"`+name+`"`, "framework")
	}
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	setTargets(t, ws, "agents")
	alpha := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "wrap"))
	// Adding claude to the targets of the installed reserved-name skill: refused.
	setTargets(t, ws, "agents", "claude")
	out, err := runExtCfg(t, cfgHome, ws, "update", "alpha")
	wantConflict(t, err, out, `"wrap"`, "framework")

	// A later cg reserves a name alpha already ships for claude: warning.
	setTargets(t, ws, "claude")
	releaseExtRepo(t, alpha, skillsManifest("alpha", "1.1.0", "wrap", "later"))
	setTargets(t, ws, "agents")
	if out, err := runExtCfg(t, cfgHome, ws, "update", "alpha"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	cfgHome2, ws2 := t.TempDir(), newWorkspace(t)
	beta := installRepo(t, cfgHome2, ws2, skillsManifest("beta", "1.0.0", "later"))
	frameworkClaudeFiles = append(frameworkClaudeFiles, frameworkFile{"skills/later/SKILL.md", ".claude/skills/later/SKILL.md", 0o644})
	t.Cleanup(func() { frameworkClaudeFiles = frameworkClaudeFiles[:len(frameworkClaudeFiles)-1] })
	releaseExtRepo(t, beta, skillsManifest("beta", "1.1.0", "later"))
	out, err = runExtCfg(t, cfgHome2, ws2, "update", "beta")
	if err != nil || !strings.Contains(out, `warning: skill "later" of extension "beta" hides`) {
		t.Errorf("update: %v\n%s", err, out)
	}
	out, _ = runExtCfg(t, cfgHome2, ws2, "list")
	if !strings.Contains(out, `hides the framework's "later"`) {
		t.Errorf("list doesn't flag it:\n%s", out)
	}
	// A reserved name added by an update is refused.
	releaseExtRepo(t, beta, skillsManifest("beta", "1.2.0", "later", "match-project"))
	out, err = runExtCfg(t, cfgHome2, ws2, "update", "beta")
	wantConflict(t, err, out, `"match-project"`)
}

// S22: an update conflicting with an extension not being updated hints at
// updating that one first.
func TestUpdateSkillConflictHint(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	beta := installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0", "other"))
	releaseExtRepo(t, beta, skillsManifest("beta", "1.1.0", "other", "digest"))
	out, err := runExtCfg(t, cfgHome, ws, "update", "beta")
	wantConflict(t, err, out, `"digest"`, `if a newer version of "alpha" drops the skill, update it first`)
}

// S37, S39: a skill moving between extensions in one update is refused with
// the order that works; following it succeeds.
func TestSkillMoveBetweenExtensions(t *testing.T) {
	requireGit(t)
	t.Run("one move", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		alpha := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
		beta := installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0"))
		releaseExtRepo(t, alpha, skillsManifest("alpha", "1.1.0"))
		releaseExtRepo(t, beta, skillsManifest("beta", "1.1.0", "digest"))
		before := snapshotDir(t, ws)
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantConflict(t, err, out, `skill "digest" moves from extension "alpha" to extension "beta"`,
			"cg extension update alpha\n  cg extension update beta\n  cg extension update")
		assertUnchanged(t, ws, before)
		for _, args := range [][]string{{"update", "alpha"}, {"update", "beta"}, {"update"}} {
			if out, err := runExtCfg(t, cfgHome, ws, args...); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
		mustExist(t, filepath.Join(ws, ".claude", "skills", "digest", "SKILL.md"))
		if a, b := ledgerOf(t, ws, "alpha"), ledgerOf(t, ws, "beta"); len(a.Skills) != 0 || len(b.Skills) != 1 {
			t.Errorf("ledgers: alpha %+v beta %+v", a.Skills, b.Skills)
		}
	})
	t.Run("chain", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		a := installRepo(t, cfgHome, ws, skillsManifest("aa", "1", "s1"))
		b := installRepo(t, cfgHome, ws, skillsManifest("bb", "1", "s2"))
		c := installRepo(t, cfgHome, ws, skillsManifest("cc", "1"))
		releaseExtRepo(t, a, skillsManifest("aa", "2"))
		releaseExtRepo(t, b, skillsManifest("bb", "2", "s1"))
		releaseExtRepo(t, c, skillsManifest("cc", "2", "s2"))
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantConflict(t, err, out, "cg extension update aa\n  cg extension update bb\n  cg extension update cc\n  cg extension update")
	})
	t.Run("swap", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		a := installRepo(t, cfgHome, ws, skillsManifest("aa", "1", "s1"))
		b := installRepo(t, cfgHome, ws, skillsManifest("bb", "1", "s2"))
		releaseExtRepo(t, a, skillsManifest("aa", "2", "s2"))
		releaseExtRepo(t, b, skillsManifest("bb", "2", "s1"))
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantConflict(t, err, out, "cycle", "aa, bb", "remove")
	})
	t.Run("ring of three", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		a := installRepo(t, cfgHome, ws, skillsManifest("aa", "1", "s1"))
		b := installRepo(t, cfgHome, ws, skillsManifest("bb", "1", "s2"))
		c := installRepo(t, cfgHome, ws, skillsManifest("cc", "1", "s3"))
		releaseExtRepo(t, a, skillsManifest("aa", "2", "s3"))
		releaseExtRepo(t, b, skillsManifest("bb", "2", "s1"))
		releaseExtRepo(t, c, skillsManifest("cc", "2", "s2"))
		out, err := runExtCfg(t, cfgHome, ws, "update")
		wantConflict(t, err, out, "cycle", "aa, bb, cc")
	})
}

// S39 with an uncommitted edit: the dropper's update stops, and succeeds once
// the edit is committed.
func TestSkillMoveWithUncommittedEdit(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	ctx := context.Background()
	for _, args := range [][]string{{"init", "--quiet"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		if _, err := gitx.Run(ctx, ws, args...); err != nil {
			t.Fatal(err)
		}
	}
	commit := func() {
		for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "c"}} {
			if _, err := gitx.Run(ctx, ws, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	alpha := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	beta := installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0"))
	commit()
	md := filepath.Join(ws, ".claude", "skills", "digest", "SKILL.md")
	if err := os.WriteFile(md, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseExtRepo(t, alpha, skillsManifest("alpha", "1.1.0"))
	releaseExtRepo(t, beta, skillsManifest("beta", "1.1.0", "digest"))
	out, err := runExtCfg(t, cfgHome, ws, "update", "alpha")
	wantConflict(t, err, out, "commit or discard your change to .claude/skills/digest/SKILL.md")
	commit()
	for _, args := range [][]string{{"update", "alpha"}, {"update", "beta"}} {
		if out, err := runExtCfg(t, cfgHome, ws, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	b, err := os.ReadFile(md) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("the new owner's SKILL.md is missing: %v", err)
	}
	if want := readRepoFile(t, beta, "skills/digest/SKILL.md"); string(b) != want {
		t.Errorf("SKILL.md isn't beta's version:\n%s", b)
	}
}

// S31: an update of several extensions names every uncommitted edit, across
// all of them, before applying any.
func TestUpdateStopsOnEditsAcrossExtensions(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	alpha := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "one"))
	beta := installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0", "two"))
	for _, s := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(ws, ".claude", "skills", s, "SKILL.md"), []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	releaseExtRepo(t, alpha, skillsManifest("alpha", "1.1.0", "one"))
	releaseExtRepo(t, beta, skillsManifest("beta", "1.1.0", "two"))
	before := snapshotDir(t, ws)
	out, err := runExtCfg(t, cfgHome, ws, "update")
	wantConflict(t, err, out, ".claude/skills/one/SKILL.md", ".claude/skills/two/SKILL.md")
	assertUnchanged(t, ws, before)
}

// S24: a note or template of one extension inside another's skill directory.
func TestTemplateInsideAnotherExtensionsSkill(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	m := `{"manifest": 2, "name": "beta", "version": "1", "description": "d",
	  "contributes": {"templates": [{"src": "t.md", "dest": ".claude/skills/digest/extra.md"}]}}`
	out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, m))
	wantConflict(t, err, out, ".claude/skills/digest/extra.md", `"alpha"`, `"beta"`)
}

// S35: remove deletes the skill and names a local file left behind.
func TestRemoveSkill(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	local := filepath.Join(ws, ".claude", "skills", "digest", "SKILL.local.md")
	if err := os.WriteFile(local, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runExtCfg(t, cfgHome, ws, "remove", "alpha")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	mustExist(t, local)
	if _, err := os.Stat(filepath.Join(ws, ".claude", "skills", "digest", "SKILL.md")); err == nil {
		t.Error("SKILL.md left behind")
	}
	if !strings.Contains(out, "left .claude/skills/digest/SKILL.local.md") {
		t.Errorf("output: %s", out)
	}
}

// S15, S26: list shows skills without targets, and a user-scope skill.
func TestListSkills(t *testing.T) {
	requireGit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	setTargets(t, ws)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	out, _ := runExtCfg(t, cfgHome, ws, "list")
	if !strings.Contains(out, "digest (not installed: no targets)") {
		t.Errorf("list:\n%s", out)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "digest"), 0o755); err != nil {
		t.Fatal(err)
	}
	setTargets(t, ws, "claude")
	out, err := runExtCfg(t, cfgHome, ws, "update", "alpha")
	if err != nil || !strings.Contains(out, "warning: skill \"digest\": "+filepath.Join(home, ".claude", "skills", "digest")) {
		t.Errorf("update: %v\n%s", err, out)
	}
	extListJSON = true
	t.Cleanup(func() { extListJSON = false })
	out, err = runExtCfg(t, cfgHome, ws, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var listed []listedExtension
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(listed) != 1 || len(listed[0].Skills) != 1 || len(listed[0].Skills[0].UserScope) != 1 {
		t.Errorf("json: %+v", listed)
	}
}

// S36, S36b, S23, S20c: cg init on a fresh clone.
func TestInitRestoresSkills(t *testing.T) {
	requireGit(t)
	t.Run("keeps an edit, restores a missing file", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
		md := filepath.Join(ws, ".claude", "skills", "digest", "SKILL.md")
		if err := os.WriteFile(md, []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}
		removeTree(t, ws, "alpha")
		out, err := runInitForce(t, cfgHome, ws)
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		if b, _ := os.ReadFile(md); string(b) != "edited" || !strings.Contains(out, ".claude/skills/digest/SKILL.md differs from the extension's version") {
			t.Errorf("init replaced the edit or said nothing:\n%s", out)
		}
	})
	t.Run("stops before fetching on a symlinked target dir", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		repo := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
		// No tree, no mirror and no source: a fetch would fail with its own
		// error, so the containment error proves the check ran first.
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		away := repo + "-away"
		if err := os.Rename(repo, away); err != nil {
			t.Fatal(err)
		}
		skills := filepath.Join(ws, ".claude", "skills")
		if err := os.RemoveAll(skills); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, skills); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
		out, err := runInitForce(t, cfgHome, ws)
		if err == nil || !strings.Contains(err.Error(), "outside the workspace") {
			t.Fatalf("init: %v\n%s", err, out)
		}
		cfg, _ := workspace.Detect(ws)
		if _, serr := os.Stat(extension.SourceRoot(findRef(cfg.Extensions, "alpha"))); serr == nil {
			t.Error("fetched before the check")
		}
		if err := os.Remove(skills); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(away, repo); err != nil {
			t.Fatal(err)
		}
		if out, err := runInitForce(t, cfgHome, ws); err != nil {
			t.Fatalf("init after the fix: %v\n%s", err, out)
		}
		mustExist(t, filepath.Join(skills, "digest", "SKILL.md"))
	})
	t.Run("whole set conflict applies none", func(t *testing.T) {
		cfgHome := t.TempDir()
		refs := make([]workspace.ExtensionRef, 0, 2)
		for _, n := range []string{"alpha", "beta"} {
			src := newWorkspace(t)
			installRepo(t, cfgHome, src, skillsManifest(n, "1.0.0", "digest"))
			cfg, _ := workspace.Detect(src)
			refs = append(refs, *findRef(cfg.Extensions, n))
		}
		ws := newWorkspace(t)
		cfg, _ := workspace.Detect(ws)
		for i := range refs {
			cfg.UpsertExtension(&refs[i])
		}
		if err := cfg.Save(ws); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		out, err := runInitForce(t, cfgHome, ws)
		wantConflict(t, err, out, `"digest"`, `"alpha"`, `"beta"`)
		if _, serr := os.Stat(filepath.Join(ws, ".claude", "skills", "digest")); serr == nil {
			t.Error("a skill was applied despite the conflict")
		}
	})
	t.Run("reserved name only warns", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		setTargets(t, ws, "agents")
		// A command name: cg init writes the framework's own skill files, so
		// a skill named after one would also meet that file.
		installRepo(t, cfgHome, ws, skillsManifest("gamma", "1.0.0", "match-project"))
		setTargets(t, ws, "claude")
		removeTree(t, ws, "gamma")
		out, err := runInitForce(t, cfgHome, ws)
		if err != nil || !strings.Contains(out, `skill "match-project" of extension "gamma" hides`) {
			t.Errorf("init: %v\n%s", err, out)
		}
	})
}

// S36c (as amended 2026-11-27): a cg init that stops after fetching removes
// the trees it fetched for extensions it didn't apply, so cg init run again
// after the fix applies them.
func TestInitStopAfterFetchRetries(t *testing.T) {
	requireGit(t)
	t.Run("one of two refused", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "one"))
		installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0", "two"))
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		// beta's managed file now differs and no ledger records it.
		if err := os.Remove(extension.LedgerPath(ws, "beta")); err != nil {
			t.Fatal(err)
		}
		md := filepath.Join(ws, ".claude", "skills", "two", "SKILL.md")
		if err := os.WriteFile(md, []byte("someone else's"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := runInitForce(t, cfgHome, ws)
		if err == nil || !strings.Contains(err.Error(), ".claude/skills/two/SKILL.md") {
			t.Fatalf("init: %v\n%s", err, out)
		}
		cfg, _ := workspace.Detect(ws)
		for _, n := range []string{"alpha", "beta"} {
			if _, serr := os.Stat(extension.SourceRoot(findRef(cfg.Extensions, n))); serr == nil {
				t.Errorf("%s's tree left behind", n)
			}
		}
		if err := os.Remove(md); err != nil {
			t.Fatal(err)
		}
		if out, err := runInitForce(t, cfgHome, ws); err != nil {
			t.Fatalf("init after the fix: %v\n%s", err, out)
		}
		for _, n := range []string{"alpha", "beta"} {
			if led := ledgerOf(t, ws, n); len(led.Skills) != 1 {
				t.Errorf("%s not applied: %+v", n, led.Skills)
			}
		}
		mustExist(t, md)
	})
	t.Run("a later fetch fails", func(t *testing.T) {
		cfgHome, ws := t.TempDir(), newWorkspace(t)
		installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "one"))
		beta := installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0", "two"))
		if err := os.RemoveAll(filepath.Join(cfgHome, "consigliere", "extensions")); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(ws, ".claude", "skills")); err != nil {
			t.Fatal(err)
		}
		hidden := beta + "-away"
		if err := os.Rename(beta, hidden); err != nil {
			t.Fatal(err)
		}
		out, err := runInitForce(t, cfgHome, ws)
		if err == nil || !strings.Contains(err.Error(), `"beta"`) {
			t.Fatalf("init: %v\n%s", err, out)
		}
		cfg, _ := workspace.Detect(ws)
		if _, serr := os.Stat(extension.SourceRoot(findRef(cfg.Extensions, "alpha"))); serr == nil {
			t.Error("alpha's tree left behind")
		}
		if err := os.Rename(hidden, beta); err != nil {
			t.Fatal(err)
		}
		if out, err := runInitForce(t, cfgHome, ws); err != nil {
			t.Fatalf("init after the fix: %v\n%s", err, out)
		}
		mustExist(t, filepath.Join(ws, ".claude", "skills", "one", "SKILL.md"))
		mustExist(t, filepath.Join(ws, ".claude", "skills", "two", "SKILL.md"))
	})
}

func readRepoFile(t *testing.T, repo, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel))) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A skill moving between extensions for one target (alpha had it for
// claude, beta for agents; targets become [claude]) is caught as a move.
func TestSkillMovePerTarget(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	setTargets(t, ws, "claude")
	alpha := installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	setTargets(t, ws, "agents")
	installRepo(t, cfgHome, ws, skillsManifest("beta", "1.0.0", "digest"))
	releaseExtRepo(t, alpha, skillsManifest("alpha", "1.1.0"))
	setTargets(t, ws, "claude")
	before := snapshotDir(t, ws)
	out, err := runExtCfg(t, cfgHome, ws, "update")
	wantConflict(t, err, out, `skill "digest" moves from extension "alpha" to extension "beta"`)
	assertUnchanged(t, ws, before)
}

// S38: only the extensions in the cycle are named as ways out; one that
// only waits on the cycle is named apart.
func TestSkillMoveCycleMembers(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	a := installRepo(t, cfgHome, ws, skillsManifest("aa", "1", "s1", "s3"))
	b := installRepo(t, cfgHome, ws, skillsManifest("bb", "1", "s2"))
	c := installRepo(t, cfgHome, ws, skillsManifest("cc", "1"))
	releaseExtRepo(t, a, skillsManifest("aa", "2", "s2"))
	releaseExtRepo(t, b, skillsManifest("bb", "2", "s1"))
	releaseExtRepo(t, c, skillsManifest("cc", "2", "s3"))
	out, err := runExtCfg(t, cfgHome, ws, "update")
	wantConflict(t, err, out, "in a cycle: aa, bb)", "Extensions cc only wait on the cycle")
}

// S24 with a destination spelled ./…: still inside another extension's skill.
func TestTemplateInsideSkillDotSlash(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, skillsManifest("alpha", "1.0.0", "digest"))
	m := `{"manifest": 2, "name": "beta", "version": "1", "description": "d",
	  "contributes": {"templates": [{"src": "t.md", "dest": "./.claude/skills/digest/extra.md"}]}}`
	out, err := runExtCfg(t, cfgHome, ws, "install", makeExtRepo(t, m))
	wantConflict(t, err, out, ".claude/skills/digest/extra.md", `"alpha"`)
}

// list --json keeps its shape for an extension without skills.
func TestListJSONWithoutSkills(t *testing.T) {
	requireGit(t)
	cfgHome, ws := t.TempDir(), newWorkspace(t)
	installRepo(t, cfgHome, ws, testManifest)
	extListJSON = true
	t.Cleanup(func() { extListJSON = false })
	out, err := runExtCfg(t, cfgHome, ws, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"skills"`) {
		t.Errorf("skills key for an extension without skills:\n%s", out)
	}
}
