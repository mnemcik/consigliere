package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mnemcik/consigliere/internal/manifest"
	syncpkg "github.com/mnemcik/consigliere/internal/sync"
)

// The .claude/ files follow the same reconcile contract as notes: an untouched
// stale file is updated, a user-edited one is left alone, a missing new one is
// installed, and a file cg never recorded is only taken over when it already
// matches. Ids are real frameworkClaudeFiles entries so the install mode applies.
func TestApplySyncClaudeFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const (
		skill   = ".claude/skills/wrap/SKILL.md"      // recorded, untouched, stale → updated
		command = ".claude/commands/cg-sync.md"       // recorded, user-edited → drifted
		hook    = ".claude/hooks/session-end.sh"      // recorded, untouched, stale → updated, stays executable
		fresh   = ".claude/commands/cg-init.md"       // not recorded, absent → installed
		foreign = ".claude/commands/match-project.md" // not recorded, different file on disk → drifted
		adopted = ".claude/statusline.sh"             // not recorded, already identical → recorded
	)
	write := func(id, body string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(id))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(skill, "---\nname: wrap\n---\nskill v1\n", 0o644)
	write(command, "USER EDITED command\n", 0o644)
	write(hook, "#!/bin/sh\nhook v1\n", 0o755)
	write(foreign, "the user's own match-project\n", 0o644)
	write(adopted, "#!/bin/sh\nstatusline v2\n", 0o755)
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")

	mf := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Sections:      map[string]manifest.Artifact{},
		Notes:         map[string]manifest.Artifact{},
		Files: map[string]manifest.Artifact{
			skill:   {Hash: manifest.HashContent("---\nname: wrap\n---\nskill v1\n")},
			command: {Hash: manifest.HashContent("command v1\n")},
			hook:    {Hash: manifest.HashContent("#!/bin/sh\nhook v1\n")},
		},
	}

	fw := map[string][]byte{
		skill:   []byte("---\nname: wrap\n---\nskill v2\n"),
		command: []byte("command v2\n"),
		hook:    []byte("#!/bin/sh\nhook v2\n"),
		fresh:   []byte("cg-init v2\n"),
		foreign: []byte("match-project v2\n"),
		adopted: []byte("#!/bin/sh\nstatusline v2\n"),
	}

	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", nil, hashFiles(fw))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	status := map[string]syncpkg.Status{}
	for _, it := range report.Items {
		if it.Kind == syncpkg.KindFile {
			status[it.ID] = it.Status
		}
	}
	want := map[string]syncpkg.Status{
		skill:   syncpkg.StatusUpdatable,
		command: syncpkg.StatusDrifted,
		hook:    syncpkg.StatusUpdatable,
		fresh:   syncpkg.StatusNew,
		foreign: syncpkg.StatusDrifted,
		adopted: syncpkg.StatusNew,
	}
	for id, w := range want {
		if status[id] != w {
			t.Errorf("%s: status %q, want %q", id, status[id], w)
		}
	}

	applied, err := applySync(dir, mf, report, nil, nil, fw)
	if err != nil {
		t.Fatalf("applySync: %v", err)
	}
	if len(applied.Files) != 4 {
		t.Errorf("applied files = %v, want skill, hook, fresh, adopted", applied.Files)
	}

	read := func(id string) string { return readFile(t, filepath.Join(dir, filepath.FromSlash(id))) }
	if got := read(skill); got != string(fw[skill]) {
		t.Errorf("skill not updated (frontmatter included): %q", got)
	}
	if got := read(command); got != "USER EDITED command\n" {
		t.Errorf("user-edited command was clobbered: %q", got)
	}
	if got := read(foreign); got != "the user's own match-project\n" {
		t.Errorf("unrecorded file that differs was clobbered: %q", got)
	}
	if got := read(fresh); got != string(fw[fresh]) {
		t.Errorf("new file not installed: %q", got)
	}
	fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(hook)))
	if err != nil || fi.Mode()&0o111 == 0 {
		t.Errorf("hook must stay executable after update: %v %v", fi.Mode(), err)
	}

	reloaded, err := manifest.Load(dir)
	if err != nil || reloaded == nil {
		t.Fatalf("reloading manifest: %v", err)
	}
	if _, ok := reloaded.Files[adopted]; !ok {
		t.Error("identical unrecorded file was not recorded, so its next update would read as drift")
	}
	if _, ok := reloaded.Files[foreign]; ok {
		t.Error("an unrecorded file that differs must stay unrecorded")
	}

	// Convergence: only the two files cg must not touch remain actionable.
	report2, err := buildSyncReport(dir, reloaded, "# CLAUDE.md\n", nil, hashFiles(fw))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range report2.Items {
		leftAloneDrift := (it.ID == command || it.ID == foreign) && it.Status == syncpkg.StatusDrifted
		if it.Status != syncpkg.StatusUpToDate && !leftAloneDrift {
			t.Errorf("after apply, %s %s = %q", it.Kind, it.ID, it.Status)
		}
	}
}

// A workspace that declined Claude Code integration at init has none of the
// files and no records; sync must not install them.
func TestBuildSyncReportSkipsClaudeFilesWhenNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")
	mf := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion}

	fw, err := embeddedClaudeFileContents()
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", nil, hashFiles(fw))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range report.Items {
		if it.Kind == syncpkg.KindFile {
			t.Errorf("opted-out workspace got file item %s (%s)", it.ID, it.Status)
		}
	}
}

// The case that started this: a workspace initialised before a skill shipped
// has the other .claude/ files but not the skill, and no file records at all.
// `cg sync --apply` must install the skill, and record the files it can prove
// are unedited, without touching the ones it can't.
func TestSyncInstallsSkillMissingFromOlderWorkspace(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer chdir(t, origDir)
	chdir(t, dir)
	forceInit = false
	if err := runInit(nil, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Rewind to a pre-tracking workspace: no file records, no wrap skill, and
	// one hook the user customised.
	mf, err := manifest.Load(dir)
	if err != nil || mf == nil {
		t.Fatalf("loading manifest: %v", err)
	}
	mf.Files = nil
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(dir, ".claude", "skills", "wrap", "SKILL.md")
	if err := os.Remove(skillPath); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(dir, ".claude", "hooks", "pull-latest-main.sh")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n# mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	syncApply = true
	defer func() { syncApply = false }()
	if err := runSync(nil, nil); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	want, err := embeddedFS.ReadFile("skills/wrap/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, skillPath); got != string(want) {
		t.Error("wrap skill was not installed by cg sync --apply")
	}
	if got := readFile(t, hookPath); got != "#!/bin/sh\n# mine\n" {
		t.Errorf("customised hook was clobbered: %q", got)
	}
	reloaded, err := manifest.Load(dir)
	if err != nil || reloaded == nil {
		t.Fatalf("reloading manifest: %v", err)
	}
	if _, ok := reloaded.Files[".claude/skills/wrap/SKILL.md"]; !ok {
		t.Error("installed skill not recorded")
	}
	if _, ok := reloaded.Files[".claude/hooks/pull-latest-main.sh"]; ok {
		t.Error("customised hook must not be recorded as cg's own")
	}
	if _, ok := reloaded.Files[".claude/commands/cg-sync.md"]; !ok {
		t.Error("unedited command not recorded")
	}
}

// A section that is on disk, byte-identical, but missing from the manifest is
// "new"; applying it must record it, not append a second copy.
func TestApplySyncNewSectionAlreadyOnDiskIsNotDuplicated(t *testing.T) {
	dir := t.TempDir()
	claude := "# CLAUDE.md\n<!-- cg:section:start=x -->\nbody\n<!-- cg:section:end=x -->\n"
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), claude)
	mf := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Sections:      map[string]manifest.Artifact{},
		Notes:         map[string]manifest.Artifact{},
	}
	report, err := buildSyncReport(dir, mf, claude, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applySync(dir, mf, report, manifest.ParseSections(claude), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "CLAUDE.md")); got != claude {
		t.Errorf("CLAUDE.md changed; want the section recorded in place:\n%s", got)
	}
	if _, ok := mf.Sections["x"]; !ok {
		t.Error("section not recorded")
	}
}
