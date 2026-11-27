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
	var wroteFirst, deletedFirst bool
	skillWriteHook = func(dest string) error {
		if strings.HasSuffix(dest, "z/late.md") {
			// Earlier steps of the run must have changed the workspace, so
			// the check below sees a rollback, not an untouched tree.
			wroteFirst = readText(t, filepath.Join(root, ".claude", "skills", "s", "a.md")) == "a2" &&
				exists(filepath.Join(root, ".claude", "skills", "s", "new", "n.md"))
			deletedFirst = !exists(filepath.Join(root, ".claude", "skills", "s", "gone.md"))
			return errors.New("disk full")
		}
		return nil
	}
	t.Cleanup(func() { skillWriteHook = nil })
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected the injected failure, got %v", err)
	}
	if !wroteFirst || !deletedFirst {
		t.Fatalf("the failure came before the run changed anything (wrote %v, deleted %v)", wroteFirst, deletedFirst)
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

// S16b for ledger data: a committed ledger whose skill name or file climbs
// out of the skill's directory is refused before anything is written,
// deleted or pruned (update and remove alike).
func TestSkillLedgerPathsRefused(t *testing.T) {
	clone := skillClone(t, "s", nil)
	for name, bad := range map[string]LedgerSkill{
		"name climbs":       {Name: "x/../../..", Targets: []string{"claude"}, Files: []string{".claude/skills/x/../../../f"}},
		"file climbs":       {Name: "s", Targets: []string{"claude"}, Files: []string{".claude/skills/s/../../../f"}},
		"file elsewhere":    {Name: "s", Targets: []string{"claude"}, Files: []string{"notes/f.md"}},
		"unknown target":    {Name: "s", Targets: []string{"nope"}, Files: nil},
		"backslash in file": {Name: "s", Targets: []string{"claude"}, Files: []string{`.claude/skills/s/..\..\f`}},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "ws")
			writeTree(t, parent, map[string]string{"f": "x", "ws/.keep": "", "empty/.gitkeep": ""})
			if err := os.Remove(filepath.Join(parent, "empty", ".gitkeep")); err != nil {
				t.Fatal(err)
			}
			old := &Ledger{Name: "demo", Skills: []LedgerSkill{bad}, Files: map[string]string{}}
			for _, f := range bad.Files {
				old.Files[f] = ContentID([]byte("x"))
			}
			_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: old})
			if err == nil {
				t.Fatal("update accepted the ledger")
			}
			if !strings.Contains(err.Error(), ".cg/ext/demo.json") || !strings.Contains(err.Error(), "restore") || strings.Contains(err.Error(), "cg init") {
				t.Errorf("the message doesn't name the ledger and a working way out: %v", err)
			}
			if err := RemoveSkills(root, old, &ApplyOptions{}, &SkillReport{}); err == nil {
				t.Error("remove accepted the ledger")
			}
			if err := CheckSkillPaths(root, []string{"claude"}, old); err == nil {
				t.Error("cg init's check accepted the ledger")
			}
			if !exists(filepath.Join(parent, "f")) || !exists(filepath.Join(parent, "empty")) {
				t.Error("cg touched the workspace's parent directory")
			}
		})
	}
}

// A link above src (the extension's skills/ directory) that leaves the
// extension is refused.
func TestReadSkillRefusesLinkAboveSrc(t *testing.T) {
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"s/SKILL.md": skillMD("s")})
	clone := t.TempDir()
	symlink(t, outside, filepath.Join(clone, "skills"))
	_, _, err := runSkills(t, t.TempDir(), clone, oneSkill("s"), skillRunOpts{})
	wantErr(t, err, "outside the extension")
}

// S18: a ledger of an extension that ships only skills, saved with
// targets [] (no file, so no Files in the file), still counts as having
// content ids after it is read back.
func TestSkillsOnlyLedgerHasContentIDs(t *testing.T) {
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, skillClone(t, "s", nil), oneSkill("s"), skillRunOpts{targets: []string{}})
	if err := l.Save(root); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLedger(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !back.HasContentIDs() {
		t.Errorf("read back as a ledger from before content ids: %+v", back)
	}
}

// A rename that only changes a file's case, on a file system that ignores
// case, keeps the file (it is the same file on disk).
func TestSkillCaseOnlyRename(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"Foo.md": "foo"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "s", "foo.md")); err != nil {
		t.Skip("case-sensitive file system: the two names are different files")
	}
	if err := os.Remove(filepath.Join(clone, "skills", "s", "Foo.md")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/foo.md": "foo"})
	var r *SkillReport
	l, r = mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if readText(t, filepath.Join(root, ".claude", "skills", "s", "foo.md")) != "foo" {
		t.Fatal("the renamed file was deleted")
	}
	if !strings.Contains(joined(r), "`git mv --force \".claude/skills/s/Foo.md\" \".claude/skills/s/foo.md\"` from the workspace root") {
		t.Errorf("no git mv hint: %v", r.Notices)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".claude", "skills", "s"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == "foo.md" }) {
		t.Errorf("the file keeps its old spelling: %v", entries)
	}
	// And with new content: updated, not refused as unowned.
	writeTree(t, clone, map[string]string{"skills/s/foo.md": "foo2"})
	if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l}); err != nil {
		t.Fatalf("update: %v", err)
	}
}

// A target directory that links to the other target (.claude/skills ->
// ../.agents/skills): changing the targets keeps the skill, since the old
// path and the new one are the same file.
func TestSkillLinkedTargetDirectories(t *testing.T) {
	for name, from := range map[string][]string{"claude to agents": {"claude"}, "both to agents": {"claude", "agents"}} {
		t.Run(name, func(t *testing.T) {
			clone := skillClone(t, "s", map[string]string{"a.md": "a"})
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("..", ".agents", "skills"), filepath.Join(root, ".claude", "skills")); err != nil {
				t.Skipf("no symlinks: %v", err)
			}
			l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{targets: from})
			l, r := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"agents"}})
			for _, f := range []string{"SKILL.md", "a.md"} {
				if !exists(filepath.Join(root, ".agents", "skills", "s", f)) {
					t.Errorf("%s deleted", f)
				}
			}
			if strings.Contains(joined(r), "left ") {
				t.Errorf("the skill's own files reported as left behind: %v", r.Notices)
			}
			// The next update finds nothing edited.
			if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"agents"}}); err != nil {
				t.Errorf("next update: %v", err)
			}
		})
	}
}

// A committed ledger can't get the workspace's SKILL.local.md deleted.
func TestSkillLedgerLocalFileRefused(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".claude/skills/s/skill.local.md": "mine"})
	old := &Ledger{Name: "demo", Skills: []LedgerSkill{{Name: "s", Targets: []string{"claude"}, Files: []string{".claude/skills/s/skill.local.md"}}},
		Files: map[string]string{".claude/skills/s/skill.local.md": ContentID([]byte("mine"))}}
	if err := RemoveSkills(root, old, &ApplyOptions{}, &SkillReport{}); err == nil {
		t.Error("remove accepted the ledger")
	}
	if !exists(filepath.Join(root, ".claude", "skills", "s", "skill.local.md")) {
		t.Error("the local file was deleted")
	}
}

// linkedWorkspace is a git workspace where .claude/skills links to
// ../.agents/skills.
func linkedWorkspace(t *testing.T) string {
	t.Helper()
	root := gitWorkspace(t)
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", ".agents", "skills"), filepath.Join(root, ".claude", "skills")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	return root
}

// From [agents] to [claude] through the link: git is asked about the real
// path, so an uncommitted edit stops the run and a committed one is replaced.
func TestSkillLinkedTargetsEditState(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1"})
	root := linkedWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{targets: []string{"agents"}})
	commitAll(t, root)
	edited := filepath.Join(root, ".agents", "skills", "s", "a.md")
	if err := os.WriteFile(edited, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"claude"}})
	var stop *SkillEditError
	if !errors.As(err, &stop) {
		t.Fatalf("uncommitted edit through the link didn't stop the run: %v", err)
	}
	if readText(t, edited) != "mine" {
		t.Fatal("the edit was overwritten")
	}
	commitAll(t, root)
	if _, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"claude"}}); err != nil {
		t.Fatalf("committed edit: %v", err)
	}
	if readText(t, edited) != "a1" {
		t.Error("the committed edit wasn't replaced")
	}
}

// A file the new version drops, in a directory the run keeps through the
// link: its emptied subdirectory is pruned.
func TestSkillLinkedTargetsPruneDropped(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"sub/x.md": "x"})
	root := linkedWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{targets: []string{"claude"}})
	if err := os.RemoveAll(filepath.Join(clone, "skills", "s", "sub")); err != nil {
		t.Fatal(err)
	}
	mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, targets: []string{"agents"}})
	if exists(filepath.Join(root, ".agents", "skills", "s", "sub")) {
		t.Error("the emptied subdirectory was left behind")
	}
	if !exists(filepath.Join(root, ".agents", "skills", "s", "SKILL.md")) {
		t.Error("SKILL.md deleted")
	}
}

// A managed directory the new version makes a file, and a managed file it
// makes a directory: the old paths go first, so the new ones can be written;
// a rollback puts the old layout back.
func TestSkillPathTypeChanges(t *testing.T) {
	for name, c := range map[string]struct{ before, after map[string]string }{
		"directory to file": {map[string]string{"assets/config.json": "c"}, map[string]string{"assets": "now a file"}},
		"file to directory": {map[string]string{"assets": "a file"}, map[string]string{"assets/config.json": "c"}},
		"file to nested":    {map[string]string{"assets": "a file"}, map[string]string{"assets/nested/config.json": "c"}},
	} {
		t.Run(name, func(t *testing.T) {
			clone := skillClone(t, "s", c.before)
			root := t.TempDir()
			l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
			before := snapshotTree(t, root)
			if err := os.RemoveAll(filepath.Join(clone, "skills", "s", "assets")); err != nil {
				t.Fatal(err)
			}
			writeTree(t, clone, map[string]string{"skills/s/" + firstKey(c.after): c.after[firstKey(c.after)], "skills/s/zz.md": "z"})
			// A failure after the new layout is written rolls it back.
			newLayout := false
			skillWriteHook = func(dest string) error {
				if strings.HasSuffix(dest, "zz.md") {
					got := filepath.Join(root, ".claude", "skills", "s", filepath.FromSlash(firstKey(c.after)))
					b, err := os.ReadFile(got) //nolint:gosec // test temp dir
					newLayout = err == nil && string(b) == c.after[firstKey(c.after)]
					return errors.New("disk full")
				}
				return nil
			}
			_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
			skillWriteHook = nil
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				t.Fatalf("expected the injected failure, got %v", err)
			}
			if !newLayout {
				t.Fatal("the failure came before the new layout was written")
			}
			if after := snapshotTree(t, root); !maps.Equal(before, after) {
				t.Errorf("not rolled back:\nbefore %v\nafter  %v", before, after)
			}
			mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
			got := filepath.Join(root, ".claude", "skills", "s", filepath.FromSlash(firstKey(c.after)))
			if readText(t, got) != c.after[firstKey(c.after)] {
				t.Errorf("%s not written", firstKey(c.after))
			}
		})
	}
}

func firstKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

// A directory in the way holding a file the extension doesn't manage, or a
// file in the way that isn't a managed one, refuses before anything changes.
func TestSkillPathBlocked(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"assets/config.json": "c"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/assets/mine.md": "mine"})
	if err := os.RemoveAll(filepath.Join(clone, "skills", "s", "assets")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/assets": "file"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, ".claude/skills/s/assets/mine.md")
	if !exists(filepath.Join(root, ".claude", "skills", "s", "assets", "config.json")) {
		t.Error("the refused run deleted a file")
	}

	clone2 := skillClone(t, "s", map[string]string{"x/y.md": "y"})
	root2 := t.TempDir()
	writeTree(t, root2, map[string]string{".claude/skills/s/x": "someone's file"})
	_, _, err = runSkills(t, root2, clone2, oneSkill("s"), skillRunOpts{})
	wantErr(t, err, ".claude/skills/s/x")
}

// withinDir decides by location: an alias of dir (through a link) counts,
// a different directory spelled alike doesn't.
func TestWithinDirByLocation(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "Repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, filepath.Join(base, "alias")); err == nil {
		if !withinDir(repo, filepath.Join(base, "alias", "sub")) {
			t.Error("an alias of dir isn't inside it")
		}
	}
	other := filepath.Join(base, "repo")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Skip("case-insensitive file system: Repo and repo are one directory")
	}
	if withinDir(repo, filepath.Join(other, "f")) {
		t.Error("a different directory differing only in case counts as inside")
	}
}

// cg init leaves a dropped, edited file in place, so it can't clear the way
// for a wanted path: the run refuses with the path, before changing anything.
func TestSkillRestoreLeftFileBlocks(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"x/f.md": "f"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/x/f.md": "edited"})
	if err := os.RemoveAll(filepath.Join(clone, "skills", "s", "x")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/x": "now a file"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l, mode: SkillsRestore})
	wantErr(t, err, "blocked", ".claude/skills/s/x/f.md")
	if readText(t, filepath.Join(root, ".claude", "skills", "s", "x", "f.md")) != "edited" {
		t.Error("the edit was lost")
	}
}

// The exact-spelling fast path compares byte for byte on every platform: a
// path whose spelling differs only in case, with nothing on disk to show it
// is the same directory, isn't inside.
func TestWithinDirExactSpelling(t *testing.T) {
	base := filepath.Join(t.TempDir(), "none")
	if withinDir(filepath.Join(base, "Repo"), filepath.Join(base, "repo", "f")) {
		t.Error("a differently cased spelling counted as inside")
	}
	if !withinDir(filepath.Join(base, "Repo"), filepath.Join(base, "Repo", "f")) || !withinDir(filepath.Join(base, "Repo"), filepath.Join(base, "Repo")) {
		t.Error("the exact spelling isn't inside")
	}
	if withinDir(filepath.Join(base, "Repo"), filepath.Join(base, "Repository")) {
		t.Error("a sibling with the same prefix counted as inside")
	}
}

// A template destination spelled ./… that becomes a skill file with the same
// bytes: adopted, not reversed as an orphan.
func TestSkillTemplateDotSlashMove(t *testing.T) {
	clone := skillClone(t, "s", nil)
	writeTree(t, clone, map[string]string{"tpl/SKILL.md": skillMD("s")})
	asTemplate := &Manifest{Manifest: 2, Name: "demo", Version: "1", Description: "d", Contributes: Contributions{Templates: []CopyContribution{
		{Src: "tpl/SKILL.md", Dest: "./.claude/skills/s/SKILL.md"}}}}
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, asTemplate, skillRunOpts{})
	l2, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if orphan := OrphanLedger(l, l2); orphan != nil && len(orphan.Templates) > 0 {
		t.Errorf("the adopted file is an orphan: %v", orphan.Templates)
	}
}

// A managed file replaced by a symbolic link (even to a clean tracked file)
// refuses the update; the link stays.
func TestSkillManagedFileReplacedByLink(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1"})
	root := gitWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{"clean.md": "x"})
	commitAll(t, root)
	a := filepath.Join(root, ".claude", "skills", "s", "a.md")
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "..", "clean.md"), a); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	writeTree(t, clone, map[string]string{"skills/s/a.md": "a2"})
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, ".claude/skills/s/a.md is a symbolic link")
	if !isLink(a) {
		t.Error("the link was replaced")
	}
	// Dropped by the new version: refused too, not deleted.
	if err := os.Remove(filepath.Join(clone, "skills", "s", "a.md")); err != nil {
		t.Fatal(err)
	}
	_, _, err = runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, "symbolic link")
	if !isLink(a) {
		t.Error("the link was deleted")
	}
}

// A file the plan judged committed that changes before its replace: the run
// stops and rolls back instead of overwriting the new change.
func TestSkillReplaceRechecks(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"a.md": "a1", "b.md": "b1"})
	root := gitWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, root, map[string]string{".claude/skills/s/a.md": "mine-a", ".claude/skills/s/b.md": "mine-b"})
	commitAll(t, root)
	b := filepath.Join(root, ".claude", "skills", "s", "b.md")
	skillWriteHook = func(dest string) error {
		if strings.HasSuffix(dest, "a.md") {
			return os.WriteFile(b, []byte("saved meanwhile"), 0o644) //nolint:gosec // test temp dir
		}
		return nil
	}
	t.Cleanup(func() { skillWriteHook = nil })
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, "b.md changed while cg was running")
	if readText(t, b) != "saved meanwhile" {
		t.Error("the change made during the run was overwritten")
	}
}

// A source file whose name can't be recorded as given is refused.
func TestReadSkillRefusesUnrecordableName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows can't create a name ending in a space")
	}
	clone := skillClone(t, "s", map[string]string{"notes.md ": "x"})
	_, _, err := runSkills(t, t.TempDir(), clone, oneSkill("s"), skillRunOpts{})
	wantErr(t, err, "can't be recorded")
}

// The skill name comes from the cleaned src ("skills/demo/." is demo).
func TestSkillNameCleanSrc(t *testing.T) {
	for src, want := range map[string]string{"skills/demo/.": "demo", "skills/demo/": "demo", "skills/demo": "demo", `skills\demo`: "demo", "./demo": "demo"} {
		if got := SkillName(SkillContribution{Src: src}); got != want {
			t.Errorf("SkillName(%q) = %q, want %q", src, got, want)
		}
	}
	if err := skillManifest(SkillContribution{Src: "skills/demo/."}).Validate(); err != nil {
		t.Errorf("skills/demo/. refused: %v", err)
	}
}

// A directory standing at a captured path is never removed by a rollback.
func TestSnapshotKeepsExistingDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "settings.json")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	snap := newSnapshot()
	if err := snap.capture(dir); err != nil {
		t.Fatal(err)
	}
	snap.restore()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Error("the existing directory was removed")
	}
}

// A note spelled ./… inside a skill's directory that the next version ships
// as a skill file (S40): the base is found, so an unedited note is updated,
// not refused as unowned.
func TestSkillNoteDotSlashToSkill(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"x.md": "x2"})
	writeTree(t, clone, map[string]string{"n/x.md": "x1", "n/SKILL.md": skillMD("s")})
	asNote := &Manifest{Manifest: 2, Name: "demo", Version: "1", Description: "d", Contributes: Contributions{Notes: []CopyContribution{
		{Src: "n/x.md", Dest: "./.claude/skills/s/x.md"}, {Src: "n/SKILL.md", Dest: "./.claude/skills/s/SKILL.md"}}}}
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, asNote, skillRunOpts{})
	l2, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	if err != nil {
		t.Fatalf("the hand-off was refused: %v", err)
	}
	if readText(t, filepath.Join(root, ".claude", "skills", "s", "x.md")) != "x2" {
		t.Error("the unedited note wasn't updated")
	}
	if orphan := OrphanLedger(l, l2); orphan != nil && len(orphan.Notes) > 0 {
		t.Errorf("orphans: %v", orphan.Notes)
	}
}

// Skill -> template with a ./ destination and changed template content: the
// file is the old skill file, so it is updated, not refused as unowned.
func TestSkillToTemplateDotSlash(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"x.md": "x1"})
	root := t.TempDir()
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	writeTree(t, clone, map[string]string{"tpl/x.md": "x2"})
	asTemplate := &Manifest{Manifest: 2, Name: "demo", Version: "2", Description: "d", Contributes: Contributions{Templates: []CopyContribution{
		{Src: "tpl/x.md", Dest: "./.claude/skills/s/x.md"}}}}
	l2, _, err := runSkills(t, root, clone, asTemplate, skillRunOpts{old: l})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if readText(t, filepath.Join(root, ".claude", "skills", "s", "x.md")) != "x2" || len(l2.Pending) != 0 {
		t.Errorf("not updated (pending %v)", l2.Pending)
	}
}

// A case-only rename whose file changes after the plan: the re-check stops
// the run before the rename, and the rollback leaves the change alone.
func TestSkillCaseRenameRecheck(t *testing.T) {
	clone := skillClone(t, "s", map[string]string{"0.md": "z1", "Foo.md": "f1"})
	root := gitWorkspace(t)
	l, _ := mustRunSkills(t, root, clone, oneSkill("s"), skillRunOpts{})
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "s", "foo.md")); err != nil {
		t.Skip("case-sensitive file system: the two names are different files")
	}
	foo := filepath.Join(root, ".claude", "skills", "s", "Foo.md")
	writeTree(t, root, map[string]string{".claude/skills/s/Foo.md": "mine"})
	commitAll(t, root)
	if err := os.Remove(filepath.Join(clone, "skills", "s", "Foo.md")); err != nil {
		t.Fatal(err)
	}
	writeTree(t, clone, map[string]string{"skills/s/foo.md": "f2", "skills/s/0.md": "z2"})
	skillWriteHook = func(dest string) error {
		if strings.HasSuffix(dest, "0.md") {
			return os.WriteFile(foo, []byte("saved meanwhile"), 0o644) //nolint:gosec // test temp dir
		}
		return nil
	}
	t.Cleanup(func() { skillWriteHook = nil })
	_, _, err := runSkills(t, root, clone, oneSkill("s"), skillRunOpts{old: l})
	wantErr(t, err, "changed while cg was running")
	if readText(t, foo) != "saved meanwhile" {
		t.Error("the change made during the run was overwritten")
	}
}

// Target directories compare by location, not by the spelling a link
// records: .claude -> .AGENTS on a case-insensitive file system is the same
// place as .agents, with or without skills/ in it.
func TestSameSkillTargetDirCaseLink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".AGENTS")); err != nil {
		t.Skip("case-sensitive file system")
	}
	symlink(t, ".AGENTS", filepath.Join(root, ".claude"))
	if !SameSkillTargetDir(root, "claude", "agents") {
		t.Error("before skills/ exists: not the same place")
	}
	if err := os.Mkdir(filepath.Join(root, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !SameSkillTargetDir(root, "claude", "agents") {
		t.Error("with skills/: not the same place")
	}
}

// Separate target directories are not the same place.
func TestSameSkillTargetDirSeparate(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".claude/skills/x": "", ".agents/skills/y": ""})
	if SameSkillTargetDir(root, "claude", "agents") {
		t.Error("separate directories are one place")
	}
	if SameSkillTargetDir(t.TempDir(), "claude", "agents") {
		t.Error("two missing directories are one place")
	}
}
