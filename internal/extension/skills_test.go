package extension

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/gitx"
)

func skillMD(name string) string {
	return "---\nname: " + name + "\ndescription: does things\n---\n\n" + v1Block + "\n# " + name + "\n"
}

// writeTree writes files (slash paths → content) under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// skillClone is an extension tree shipping files under skills/<name>/.
func skillClone(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{"skills/" + name + "/SKILL.md": skillMD(name)}
	for rel, body := range files {
		all["skills/"+name+"/"+rel] = body
	}
	writeTree(t, dir, all)
	return dir
}

func oneSkill(name string, exec ...string) *Manifest {
	return skillManifest(SkillContribution{Src: "skills/" + name, Executables: exec})
}

type skillRunOpts struct {
	old     *Ledger
	targets []string
	mode    SkillMode
	force   bool
}

func runSkills(t *testing.T, root, clone string, m *Manifest, o skillRunOpts) (*Ledger, *SkillReport, error) {
	t.Helper()
	r := &SkillReport{}
	l, err := ApplyWith(root, clone, m, ApplyOptions{Old: o.old, SkillTargets: o.targets, SkillMode: o.mode,
		Force: o.force, Context: context.Background(), Repo: "https://example.com/demo", SkillReport: r})
	return l, r, err
}

func mustRunSkills(t *testing.T, root, clone string, m *Manifest, o skillRunOpts) (*Ledger, *SkillReport) {
	t.Helper()
	l, r, err := runSkills(t, root, clone, m, o)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return l, r
}

// gitWorkspace is a workspace root that is a git repository with a commit.
func gitWorkspace(t *testing.T) string {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	for _, args := range [][]string{
		{"init", "--quiet"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"}, {"commit", "--quiet", "--allow-empty", "-m", "init"},
	} {
		git(t, root, args...)
	}
	return root
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func commitAll(t *testing.T, root string) {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "--quiet", "--allow-empty", "-m", "c")
}

func readText(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not contain %q", err, p)
		}
	}
}

func joined(r *SkillReport) string {
	return strings.Join(r.Notices, "\n") + "\n" + strings.Join(r.Warnings, "\n")
}

// S7–S11: a skill that can't be read refuses the install and writes nothing.
func TestReadSkillRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		m     *Manifest
		want  []string
	}{
		{"no directory", func(t *testing.T, dir string) {}, oneSkill("s"), []string{`"s"`, "not a directory"}},
		{"src is a file", func(t *testing.T, dir string) { writeTree(t, dir, map[string]string{"skills/s": "x"}) }, oneSkill("s"), []string{"not a directory"}},
		{"no SKILL.md", func(t *testing.T, dir string) { writeTree(t, dir, map[string]string{"skills/s/README.md": "x"}) }, oneSkill("s"), []string{"no SKILL.md"}},
		{"SKILL.md only nested", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/sub/SKILL.md": skillMD("s")})
		}, oneSkill("s"), []string{"no SKILL.md"}},
		{"no frontmatter", func(t *testing.T, dir string) { writeTree(t, dir, map[string]string{"skills/s/SKILL.md": "# s\n"}) }, oneSkill("s"), []string{"frontmatter"}},
		{"unclosed frontmatter", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": "---\nname: s\n"})
		}, oneSkill("s"), []string{"frontmatter"}},
		{"name differs", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": skillMD("other")})
		}, oneSkill("s"), []string{"name", `"other"`}},
		{"no description", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": "---\nname: s\n---\n"})
		}, oneSkill("s"), []string{"description"}},
		{"empty description", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": "---\nname: s\ndescription: \"  \"\n---\n"})
		}, oneSkill("s"), []string{"description"}},
		{"long description", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": "---\nname: s\ndescription: " + strings.Repeat("é", 1025) + "\n---\n"})
		}, oneSkill("s"), []string{"description", "1024"}},
		{"missing executable", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": skillMD("s")})
		}, oneSkill("s", "run.py"), []string{"run.py", "names no file"}},
		{"ships SKILL.local.md", func(t *testing.T, dir string) {
			writeTree(t, dir, map[string]string{"skills/s/SKILL.md": skillMD("s"), "skills/s/skill.LOCAL.md": "x"})
		}, oneSkill("s"), []string{"skill.LOCAL.md", "workspace"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clone, root := t.TempDir(), t.TempDir()
			c.setup(t, clone)
			_, _, err := runSkills(t, root, clone, c.m, skillRunOpts{})
			wantErr(t, err, c.want...)
			if exists(filepath.Join(root, ".claude")) {
				t.Error("the workspace changed")
			}
		})
	}
}

// S8: a description of exactly 1024 characters, and a folded multi-line one,
// pass.
func TestSkillFrontmatterAccepts(t *testing.T) {
	for _, md := range []string{
		"---\nname: s\ndescription: " + strings.Repeat("é", 1024) + "\n---\n",
		"\ufeff---\r\nname: s\r\ndescription: >\r\n  folded\r\n  text\r\n---\r\n",
	} {
		if err := checkSkillFrontmatter("s", []byte(md)); err != nil {
			t.Errorf("%q: %v", md[:20], err)
		}
	}
}

// S9: a symbolic link anywhere in the skill is refused.
func TestReadSkillRefusesSymlinks(t *testing.T) {
	for _, where := range []string{"skills/s/link.md", "skills/s/sub/link", "skills/s"} {
		t.Run(where, func(t *testing.T) {
			clone := t.TempDir()
			writeTree(t, clone, map[string]string{"real/SKILL.md": skillMD("s"), "target.md": "x"})
			if where != "skills/s" {
				writeTree(t, clone, map[string]string{"skills/s/SKILL.md": skillMD("s")})
			}
			link := filepath.Join(clone, filepath.FromSlash(where))
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(clone, "target.md")
			if where == "skills/s" {
				target = filepath.Join(clone, "real")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("can't create a symbolic link here: %v", err)
			}
			_, _, err := runSkills(t, t.TempDir(), clone, oneSkill("s"), skillRunOpts{})
			wantErr(t, err, "symbolic link")
		})
	}
}

// S12, S13, S17, S18: every file, dot-files and nested ones included, lands
// under .claude/skills/<name>/ with its mode; the ledger lists them.
func TestInstallSkillFiles(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{".hidden": "h", "scripts/run.py": "print()\n", "a/b/c.md": "c"})
	root := t.TempDir()
	l, r := mustRunSkills(t, root, clone, oneSkill("s", "scripts/run.py"), skillRunOpts{})
	dir := filepath.Join(root, ".claude", "skills", "s")
	for _, rel := range []string{"SKILL.md", ".hidden", "scripts/run.py", "a/b/c.md"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(rel))) {
			t.Errorf("%s not installed", rel)
		}
	}
	if exists(filepath.Join(root, ".agents")) {
		t.Error(".agents/ written with the default targets")
	}
	if runtime.GOOS != "windows" {
		for rel, mode := range map[string]os.FileMode{"scripts/run.py": 0o755, "SKILL.md": 0o644, "a/b/c.md": 0o644} {
			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Errorf("%s: mode %o, want %o", rel, info.Mode().Perm(), mode)
			}
		}
	}
	if len(l.Skills) != 1 || l.Skills[0].Name != "s" || !slices.Equal(l.Skills[0].Targets, []string{"claude"}) || len(l.Skills[0].Files) != 4 {
		t.Fatalf("ledger skills: %+v", l.Skills)
	}
	for _, f := range l.Skills[0].Files {
		if l.Files[f] == "" {
			t.Errorf("no base for %s", f)
		}
	}
	if len(r.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings)
	}
}

// S14, S15, S16: the targets setting.
func TestSkillTargets(t *testing.T) {
	clone := skillClone(t, "s", nil)
	root := t.TempDir()
	mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{targets: []string{"claude", "agents"}})
	for _, d := range []string{".claude/skills/s/SKILL.md", ".agents/skills/s/SKILL.md"} {
		if !exists(filepath.Join(root, filepath.FromSlash(d))) {
			t.Errorf("%s missing", d)
		}
	}
	root = t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{targets: []string{}})
	if exists(filepath.Join(root, ".claude")) {
		t.Error("files written with targets []")
	}
	if len(l.Skills) != 1 || len(l.Skills[0].Targets) != 0 {
		t.Errorf("ledger: %+v", l.Skills)
	}
	_, _, err := runSkills(t, t.TempDir(), clone, oneSkill("s"), skillRunOpts{targets: []string{"cursor"}})
	wantErr(t, err, `"cursor"`, "claude, agents")
}

// S16b: a directory on the way that resolves outside the workspace is refused.
func TestSkillPathsOutsideWorkspace(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"sub/f.md": "f"})
	cases := map[string]func(t *testing.T, root, outside string){
		"target dir": func(t *testing.T, root, outside string) {
			symlink(t, outside, filepath.Join(root, ".claude", "skills"))
		},
		"skill dir": func(t *testing.T, root, outside string) {
			symlink(t, outside, filepath.Join(root, ".claude", "skills", "s"))
		},
		"dangling": func(t *testing.T, root, outside string) {
			symlink(t, filepath.Join(outside, "nope"), filepath.Join(root, ".claude", "skills", "s"))
		},
		"subdirectory": func(t *testing.T, root, outside string) {
			symlink(t, outside, filepath.Join(root, ".claude", "skills", "s", "sub"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			setup(t, root, outside)
			_, _, err := runSkills(t, root, clone, oneSkill("s", "sub/f.md"), skillRunOpts{})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Errorf("wrote outside the workspace: %v", entries)
			}
		})
	}
	// A link that stays inside the workspace is fine.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "real"), filepath.Join(root, ".claude", "skills"))
	mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	if !exists(filepath.Join(root, "real", "s", "SKILL.md")) {
		t.Error("not written through an inside link")
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't create a symbolic link here: %v", err)
	}
}

// S12b: the local-file block warning, only when SKILL.md is new or changed.
func TestSkillLocalBlockWarning(t *testing.T) {
	clone := t.TempDir()
	writeTree(t, clone, map[string]string{"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\nno block\n"})
	root := t.TempDir()
	l, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], `"s"`) || !strings.Contains(r.Warnings[0], `"demo"`) ||
		!strings.Contains(r.Warnings[0], "https://example.com/demo") || !strings.Contains(r.Warnings[0], "maintainer") {
		t.Fatalf("warnings: %v", r.Warnings)
	}
	// Same SKILL.md again, another file changed, or a target added: silent.
	writeTree(t, clone, map[string]string{"skills/s/other.md": "x"})
	l, r = mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if len(r.Warnings) != 0 {
		t.Errorf("warned on an unchanged SKILL.md: %v", r.Warnings)
	}
	l, r = mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"claude", "agents"}})
	if len(r.Warnings) != 0 {
		t.Errorf("warned on adding a target: %v", r.Warnings)
	}
	// cg init never warns.
	_, r = mustRunSkills(t, t.TempDir(), clone, oneSkill("s"), skillRunOpts{mode: SkillsRestore})
	if len(r.Warnings) != 0 {
		t.Errorf("warned in cg init: %v", r.Warnings)
	}
	// A changed SKILL.md warns again.
	writeTree(t, clone, map[string]string{"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\nstill no block\n"})
	_, r = mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"claude", "agents"}})
	if len(r.Warnings) != 1 {
		t.Errorf("no warning for a changed SKILL.md: %v", r.Warnings)
	}
}

// S25, S25b: an unowned identical file is adopted; a different one refused
// unless forced.
func TestSkillUnownedFiles(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"run.sh": "echo\n"})
	root := t.TempDir()
	writeTree(t, root, map[string]string{".claude/skills/s/SKILL.md": skillMD("s"), ".claude/skills/s/run.sh": "mine\n"})
	_, _, err := runSkills(t, root, clone, oneSkill("s", "run.sh"), skillRunOpts{})
	wantErr(t, err, ".claude/skills/s/run.sh", "Move them aside")
	if readFile(t, filepath.Join(root, ".claude", "skills", "s", "run.sh")) != "mine\n" {
		t.Error("refused file overwritten")
	}
	l, _ := mustRunSkills(t, root, clone, oneSkill("s", "run.sh"), skillRunOpts{force: true})
	if readFile(t, filepath.Join(root, ".claude", "skills", "s", "run.sh")) != "echo\n" || l.Files[".claude/skills/s/SKILL.md"] == "" {
		t.Error("force didn't write, or the identical file wasn't adopted")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(filepath.Join(root, ".claude", "skills", "s", "run.sh")); info.Mode().Perm() != 0o755 {
			t.Errorf("mode %o", info.Mode().Perm())
		}
	}
}

// S27, S27b, S28, S29, S32, S33, S33b: updates of unedited files.
func TestSkillUpdateUnedited(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1", "sub/b.md": "b1"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	dir := filepath.Join(root, ".claude", "skills", "s")
	writeTree(t, root, map[string]string{".claude/skills/s/SKILL.local.md": "mine"})
	// New content, a new file, a dropped file (its directory goes too).
	if err := os.RemoveAll(filepath.Join(clone, "skills", "s", "sub")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/a.md": "a2", "skills/s/c.md": "c"})
	l, r := mustRunSkills(t, root, clone, oneSkill("s", "a.md"), skillRunOpts{old: l})
	if readFile(t, filepath.Join(dir, "a.md")) != "a2" || !exists(filepath.Join(dir, "c.md")) || exists(filepath.Join(dir, "sub")) {
		t.Errorf("update not applied: %v", r.Notices)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(filepath.Join(dir, "a.md")); info.Mode().Perm() != 0o755 {
			t.Errorf("executables change not applied: %o", info.Mode().Perm())
		}
		// S32: only the mode differs: set silently.
		if err := os.Chmod(filepath.Join(dir, "a.md"), 0o644); err != nil { //nolint:gosec // the mode cg installs non-executables with
			t.Fatal(err)
		}
		var r2 *SkillReport
		l, r2 = mustRunSkills(t, root, clone, oneSkill("s", "a.md"), skillRunOpts{old: l})
		if info, _ := os.Stat(filepath.Join(dir, "a.md")); info.Mode().Perm() != 0o755 || len(r2.Notices) != 0 {
			t.Errorf("mode not restored silently: %o %v", info.Mode().Perm(), r2.Notices)
		}
	}
	// S27b: a file already holding the new version, whatever its base.
	writeTree(t, clone, map[string]string{"skills/s/c.md": "c2"})
	writeTree(t, root, map[string]string{".claude/skills/s/c.md": "c2"})
	l, r = mustRunSkills(t, root, clone, oneSkill("s", "a.md"), skillRunOpts{old: l})
	if len(r.Notices) != 0 || l.Files[".claude/skills/s/c.md"] != ContentID([]byte("c2")) {
		t.Errorf("not adopted silently: %v", r.Notices)
	}
	// S29, S33, S33b: the whole skill dropped: managed files and empty
	// directories go, the local file stays and is named.
	l, r = mustRunSkills(t, root, clone, skillManifest(), skillRunOpts{old: l})
	if exists(filepath.Join(dir, "SKILL.md")) || !exists(filepath.Join(dir, "SKILL.local.md")) {
		t.Error("drop didn't delete managed files, or deleted the local file")
	}
	if !strings.Contains(joined(r), ".claude/skills/s/SKILL.local.md") || len(l.Skills) != 0 {
		t.Errorf("local file not named: %v", r.Notices)
	}
	if err := os.Remove(filepath.Join(dir, "SKILL.local.md")); err != nil {
		t.Fatal(err)
	}
}

// S29: a dropped skill without local files leaves no directory behind.
func TestSkillDropRemovesDirectory(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"deep/er/f.md": "f"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	mustRunSkills(t, root, clone, skillManifest(), skillRunOpts{old: l})
	if exists(filepath.Join(root, ".claude", "skills", "s")) {
		t.Error("skill directory left behind")
	}
	if !exists(filepath.Join(root, ".claude", "skills")) {
		t.Error("the target directory itself was removed")
	}
}

// S30, S31: an edited, committed file is replaced with a message; an
// uncommitted one stops the run before anything is written.
func TestSkillEditedFiles(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1", "b.md": "b1"})
	root := gitWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	commitAll(t, root)
	dir := filepath.Join(root, ".claude", "skills", "s")
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine"})
	writeTree(t, clone, map[string]string{"skills/s/b.md": "b2"})

	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	var stop *SkillEditError
	if !errors.As(err, &stop) || !strings.Contains(err.Error(), "commit or discard your change to .claude/skills/s/a.md") {
		t.Fatalf("expected a stop naming a.md, got %v", err)
	}
	if readFile(t, filepath.Join(dir, "b.md")) != "b1" {
		t.Error("the stopped run wrote b.md")
	}
	// Staged only still stops.
	git(t, root, "add", "-A")
	if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l}); !errors.As(err, &stop) {
		t.Fatalf("staged change didn't stop: %v", err)
	}
	commitAll(t, root)
	l, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if readFile(t, filepath.Join(dir, "a.md")) != "a1" || readFile(t, filepath.Join(dir, "b.md")) != "b2" {
		t.Error("committed edit not replaced")
	}
	msg := joined(r)
	if !strings.Contains(msg, "your change to .claude/skills/s/a.md was replaced") || !strings.Contains(msg, "git history") ||
		!strings.Contains(msg, ".claude/skills/s/SKILL.local.md") || strings.Count(msg, "a.md was replaced") != 1 {
		t.Errorf("message: %s", msg)
	}
	// A deletion committed: restored, without the history claim.
	commitAll(t, root)
	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root)
	_, r = mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if !exists(filepath.Join(dir, "a.md")) || !strings.Contains(joined(r), "a.md was missing, so cg restored") {
		t.Errorf("not restored: %v", r.Notices)
	}
}

// S31: a path git ignores can't be committed: the stop says so.
func TestSkillEditedIgnored(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1"})
	root := gitWorkspace(t)
	writeTree(t, root, map[string]string{".gitignore": ".claude/skills/\n"})
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	commitAll(t, root)
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, "git ignores it", "SKILL.local.md")
}

// S31 in a workspace that isn't a git repository: nothing counts as
// committed.
func TestSkillEditedNoRepo(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, "commit or discard")
}

// S34, S34b, S33b: changing the targets moves the skill.
func TestSkillTargetChange(t *testing.T) {
	clone := skillClone(t, "s", nil)
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/SKILL.local.md": "mine"})
	l, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"agents"}})
	if exists(filepath.Join(root, ".claude", "skills", "s", "SKILL.md")) || !exists(filepath.Join(root, ".agents", "skills", "s", "SKILL.md")) {
		t.Error("not moved")
	}
	if !strings.Contains(joined(r), "now lives in .agents/skills/s/") {
		t.Errorf("message: %v", r.Notices)
	}
	writeTree(t, root, map[string]string{".claude/skills/s/SKILL.md": "unowned"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"agents", "claude"}})
	wantErr(t, err, ".claude/skills/s/SKILL.md")
}

// S35: remove deletes managed files, keeps local ones, stops on an
// uncommitted edit.
func TestRemoveSkills(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a"})
	root := gitWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine"})
	if err := RemoveSkills(root, l, &ApplyOptions{}, &SkillReport{}); err == nil {
		t.Fatal("uncommitted edit didn't stop the remove")
	}
	if !exists(filepath.Join(root, ".claude", "skills", "s", "SKILL.md")) {
		t.Error("the stopped remove deleted files")
	}
	writeTree(t, root, map[string]string{".claude/skills/s/SKILL.local.md": "local"})
	commitAll(t, root)
	r := &SkillReport{}
	if err := RemoveSkills(root, l, &ApplyOptions{}, r); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, ".claude", "skills", "s", "a.md")) || !exists(filepath.Join(root, ".claude", "skills", "s", "SKILL.local.md")) {
		t.Error("remove result wrong")
	}
	msg := joined(r)
	if !strings.Contains(msg, "a.md was deleted") || !strings.Contains(msg, "SKILL.local.md") {
		t.Errorf("message: %s", msg)
	}
}

// S36: cg init leaves an edited file and says so; its base stays the
// extension's.
func TestSkillRestoreKeepsEdits(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine"})
	if err := os.Remove(filepath.Join(root, ".claude", "skills", "s", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	l2, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, mode: SkillsRestore})
	if readFile(t, filepath.Join(root, ".claude", "skills", "s", "a.md")) != "mine" || !exists(filepath.Join(root, ".claude", "skills", "s", "SKILL.md")) {
		t.Error("init replaced the edit, or didn't restore the missing file")
	}
	if !strings.Contains(joined(r), ".claude/skills/s/a.md differs from the extension's version; the next `cg extension update` replaces it") {
		t.Errorf("message: %v", r.Notices)
	}
	if l2.Files[".claude/skills/s/a.md"] != ContentID([]byte("a")) {
		t.Error("kept file adopted")
	}
	// The next update stops on it (uncommitted, not a repository).
	if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l2}); err == nil {
		t.Error("update after init didn't see the edit")
	}
}

// S40, S41: files moving between templates and a skill at the same paths.
func TestSkillTemplateMoves(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a"})
	writeTree(t, clone, map[string]string{"tpl/SKILL.md": skillMD("s"), "tpl/a.md": "a"})
	asTemplates := &Manifest{Manifest: 2, Name: "demo", Version: "1", Description: "d", Contributes: Contributions{Templates: []CopyContribution{
		{Src: "tpl/SKILL.md", Dest: ".claude/skills/s/SKILL.md"}, {Src: "tpl/a.md", Dest: ".claude/skills/s/a.md"}}}}
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, asTemplates, skillRunOpts{})
	writeTree(t, clone, map[string]string{"skills/s/a.md": "a2"})
	l2, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if orphan := OrphanLedger(l, l2); orphan != nil {
		t.Errorf("moved files reported as orphans: %+v", orphan)
	}
	if readFile(t, filepath.Join(root, ".claude", "skills", "s", "a.md")) != "a2" || len(r.Notices) != 0 {
		t.Errorf("templates→skill: %v", r.Notices)
	}
	// And back: an unedited file whose content changes is updated, not pending.
	writeTree(t, clone, map[string]string{"tpl/a.md": "a3"})
	l3, _ := mustRunSkills(t, root, clone, asTemplates, skillRunOpts{old: l2})
	if readFile(t, filepath.Join(root, ".claude", "skills", "s", "a.md")) != "a3" || len(l3.Pending) != 0 || len(l3.Skills) != 0 {
		t.Errorf("skill→templates: pending %v", l3.Pending)
	}
	if orphan := OrphanLedger(l2, l3); orphan != nil {
		t.Errorf("orphans: %+v", orphan)
	}
}

// S24: a note or template of the same extension inside its skill's
// directory is refused.
func TestSkillOwnTemplateInside(t *testing.T) {
	clone := skillClone(t, "s", nil)
	writeTree(t, clone, map[string]string{"t.md": "t"})
	m := oneSkill("s")
	m.Contributes.Templates = []CopyContribution{{Src: "t.md", Dest: ".claude/skills/s/extra.md"}}
	_, _, err := runSkills(t, t.TempDir(), clone, m, skillRunOpts{})
	wantErr(t, err, ".claude/skills/s/extra.md", `"s"`)
}

// S42: a failure part-way restores every file written or deleted and removes
// created directories; S42b: the next run adopts what a cut-short run wrote.
func TestSkillRollback(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1", "gone.md": "g", "z/late.md": "z"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	before := snapshotTree(t, root)
	if err := os.Remove(filepath.Join(clone, "skills", "s", "gone.md")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/a.md": "a2", "skills/s/new/n.md": "n", "skills/s/z/late.md": "z2"})
	skillWriteHook = func(dest string) error {
		if strings.HasSuffix(dest, "z/late.md") {
			return errors.New("disk full")
		}
		return nil
	}
	t.Cleanup(func() { skillWriteHook = nil })
	if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l}); err == nil {
		t.Fatal("expected the injected failure")
	}
	after := snapshotTree(t, root)
	if !maps.Equal(before, after) {
		t.Errorf("workspace not restored:\nbefore %v\nafter  %v", before, after)
	}
	if exists(filepath.Join(root, ".claude", "skills", "s", "new")) {
		t.Error("created directory left behind")
	}
	skillWriteHook = nil
	// S42b: files written by a run whose ledger wasn't saved are adopted.
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "a2", ".claude/skills/s/new/n.md": "n"})
	if _, r, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l}); err != nil || len(r.Notices) != 0 {
		t.Errorf("rerun: %v %v", err, r)
	}
}

func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = readText(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// S26: a user-scope skill of the same name warns.
func TestSkillUserScopeWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeTree(t, home, map[string]string{".claude/skills/u/SKILL.md": "x"})
	_, r := mustRunSkills(t, t.TempDir(), skillClone(t, "u", nil), oneSkill("u"), skillRunOpts{})
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], filepath.Join(home, ".claude", "skills", "u")) {
		t.Errorf("warnings: %v", r.Warnings)
	}
}
