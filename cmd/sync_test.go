package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnemcik/consigliere/internal/manifest"
	syncpkg "github.com/mnemcik/consigliere/internal/sync"
)

// buildSyncReport must wire the three hash sources together correctly across
// sections and notes. We control the framework content directly, write a
// workspace, and hand-build a manifest so each artifact lands in a known status.
func TestBuildSyncReportClassifiesSectionsAndNotes(t *testing.T) {
	dir := t.TempDir()

	// On-disk CLAUDE.md: untouched (==recorded, old) + drifted (user-edited).
	claude := "# CLAUDE.md\n" +
		"<!-- cg:section:start=untouched -->\nold body\n<!-- cg:section:end=untouched -->\n" +
		"<!-- cg:section:start=drifted -->\nuser edited this\n<!-- cg:section:end=drifted -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}
	// On-disk notes: one untouched-but-stale, one drifted.
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "notes", "kept.md"), "note v1")
	mustWrite(t, filepath.Join(dir, "notes", "edited.md"), "note user edit")

	// Manifest records what cg last wrote: matches the on-disk *untouched* items,
	// and the original (pre-edit) hashes for the drifted ones.
	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections: map[string]manifest.Artifact{
			"untouched": {Hash: manifest.HashContent("old body")},
			"drifted":   {Hash: manifest.HashContent("original body")},
			"removed":   {Hash: manifest.HashContent("gone in new framework")},
		},
		Notes: map[string]manifest.Artifact{
			"notes/kept.md":   {Hash: manifest.HashContent("note v1")},
			"notes/edited.md": {Hash: manifest.HashContent("note original")},
		},
	}

	// Framework (this cg version) ships: changed 'untouched', a brand-new section,
	// updated note content, and a brand-new note; drops 'removed'.
	frameworkCLAUDE := "# CLAUDE.md\n" +
		"<!-- cg:section:start=untouched -->\nNEW framework body\n<!-- cg:section:end=untouched -->\n" +
		"<!-- cg:section:start=drifted -->\nNEW framework body for drifted\n<!-- cg:section:end=drifted -->\n" +
		"<!-- cg:section:start=brandnew -->\nbrand new section\n<!-- cg:section:end=brandnew -->\n"
	frameworkNotes := map[string]string{
		"notes/kept.md":   manifest.HashContent("note v2"),        // changed
		"notes/edited.md": manifest.HashContent("note v2"),        // changed
		"notes/fresh.md":  manifest.HashContent("brand new note"), // new
	}

	report, err := buildSyncReport(dir, mf, frameworkCLAUDE, frameworkNotes)
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}

	got := map[string]syncpkg.Status{}
	for _, it := range report.Items {
		got[string(it.Kind)+":"+it.ID] = it.Status
	}
	want := map[string]syncpkg.Status{
		"section:untouched":    syncpkg.StatusUpdatable, // on-disk==recorded, framework changed
		"section:drifted":      syncpkg.StatusDrifted,   // on-disk != recorded and != framework
		"section:brandnew":     syncpkg.StatusNew,       // framework only
		"section:removed":      syncpkg.StatusRemoved,   // manifest only
		"note:notes/kept.md":   syncpkg.StatusUpdatable,
		"note:notes/edited.md": syncpkg.StatusDrifted,
		"note:notes/fresh.md":  syncpkg.StatusNew,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	if !report.Actionable() {
		t.Error("report with changes must be actionable")
	}
}

// A freshly seeded manifest that matches both the workspace and the framework
// must classify as fully up-to-date (the wiring introduces no spurious drift).
func TestBuildSyncReportCleanWhenAligned(t *testing.T) {
	dir := t.TempDir()
	claude := "# CLAUDE.md\n<!-- cg:section:start=s -->\nbody\n<!-- cg:section:end=s -->\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}
	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections:         map[string]manifest.Artifact{"s": {Hash: manifest.HashContent("body")}},
		Notes:            map[string]manifest.Artifact{},
	}
	report, err := buildSyncReport(dir, mf, claude, map[string]string{})
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	if report.Actionable() {
		t.Errorf("aligned workspace must be up-to-date, got %v", report.Items)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func hashesOf(noteBytes map[string][]byte) map[string]string {
	out := map[string]string{}
	for k, v := range noteBytes {
		out[k] = manifest.HashContent(string(v))
	}
	return out
}

// The project's Done-when round trip: a workspace at vN with an untouched
// section/note and a user-edited (drifted) section/note; the framework at vN+1
// changes the untouched ones, adds a new section + new note, and edits the ones
// the user also edited. `cg sync --apply` must update only the safe ones, never
// clobber drift, insert the new ones, persist the manifest, bump the version,
// and converge (a re-classification leaves only the drift actionable).
func TestApplySyncRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// --- vN workspace on disk ---
	claudeV1 := "# CLAUDE.md\n" +
		"<!-- cg:section:start=untouched -->\nv1 untouched body\n<!-- cg:section:end=untouched -->\n" +
		"<!-- cg:section:start=drifted -->\nUSER EDITED the drifted section\n<!-- cg:section:end=drifted -->\n"
	claudePath := filepath.Join(dir, "CLAUDE.md")
	mustWrite(t, claudePath, claudeV1)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "notes", "kept.md"), "note v1")
	mustWrite(t, filepath.Join(dir, "notes", "edited.md"), "USER EDITED note")

	// Manifest as cg last wrote it: untouched/kept match disk; drifted/edited
	// record the original (pre-user-edit) content.
	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections: map[string]manifest.Artifact{
			"untouched": {Hash: manifest.HashContent("v1 untouched body")},
			"drifted":   {Hash: manifest.HashContent("v1 drifted body")},
		},
		Notes: map[string]manifest.Artifact{
			"notes/kept.md":   {Hash: manifest.HashContent("note v1")},
			"notes/edited.md": {Hash: manifest.HashContent("note v1 original")},
		},
	}
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}

	// --- vN+1 framework ---
	frameworkCLAUDE := "# CLAUDE.md\n" +
		"<!-- cg:section:start=untouched -->\nv2 untouched body\n<!-- cg:section:end=untouched -->\n" +
		"<!-- cg:section:start=drifted -->\nv2 drifted body\n<!-- cg:section:end=drifted -->\n" +
		"<!-- cg:section:start=brandnew -->\nbrand new section\n<!-- cg:section:end=brandnew -->\n"
	frameworkNoteBytes := map[string][]byte{
		"notes/kept.md":   []byte("note v2"),
		"notes/edited.md": []byte("note v2"),
		"notes/fresh.md":  []byte("fresh note"),
	}

	report, err := buildSyncReport(dir, mf, frameworkCLAUDE, hashesOf(frameworkNoteBytes))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	appliedS, appliedN, err := applySync(dir, mf, report, manifest.ParseSections(frameworkCLAUDE), frameworkNoteBytes)
	if err != nil {
		t.Fatalf("applySync: %v", err)
	}

	// --- assert what was applied ---
	if !contains(appliedS, "untouched") || !contains(appliedS, "brandnew") || len(appliedS) != 2 {
		t.Errorf("appliedSections = %v, want [untouched brandnew]", appliedS)
	}
	if !contains(appliedN, "notes/kept.md") || !contains(appliedN, "notes/fresh.md") || len(appliedN) != 2 {
		t.Errorf("appliedNotes = %v, want [notes/kept.md notes/fresh.md]", appliedN)
	}

	// --- assert disk state ---
	sections := manifest.ParseSections(readFile(t, claudePath))
	if sections["untouched"] != "v2 untouched body" {
		t.Errorf("untouched section not updated: %q", sections["untouched"])
	}
	if sections["drifted"] != "USER EDITED the drifted section" {
		t.Errorf("drifted section was clobbered: %q", sections["drifted"])
	}
	if sections["brandnew"] != "brand new section" {
		t.Errorf("new section not inserted: %q", sections["brandnew"])
	}
	if got := readFile(t, filepath.Join(dir, "notes", "kept.md")); got != "note v2" {
		t.Errorf("kept.md not updated: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "notes", "edited.md")); got != "USER EDITED note" {
		t.Errorf("edited.md (drifted) was clobbered: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "notes", "fresh.md")); got != "fresh note" {
		t.Errorf("fresh.md not created: %q", got)
	}

	// --- assert manifest persisted + version bumped ---
	reloaded, err := manifest.Load(dir)
	if err != nil || reloaded == nil {
		t.Fatalf("reloading manifest: %v", err)
	}
	if reloaded.FrameworkVersion != Version {
		t.Errorf("FrameworkVersion = %q, want %q", reloaded.FrameworkVersion, Version)
	}
	if reloaded.Sections["untouched"].Hash != manifest.HashContent("v2 untouched body") {
		t.Error("manifest hash for untouched not updated")
	}
	if reloaded.Sections["drifted"].Hash != manifest.HashContent("v1 drifted body") {
		t.Error("manifest hash for drifted must be left at the recorded value")
	}
	if _, ok := reloaded.Notes["notes/fresh.md"]; !ok {
		t.Error("new note not registered in manifest")
	}

	// --- convergence: only the user-edited drift remains actionable ---
	report2, err := buildSyncReport(dir, reloaded, frameworkCLAUDE, hashesOf(frameworkNoteBytes))
	if err != nil {
		t.Fatalf("buildSyncReport (2nd): %v", err)
	}
	for _, it := range report2.Items {
		userEditedDrift := it.Status == syncpkg.StatusDrifted && (it.ID == "drifted" || it.ID == "notes/edited.md")
		if it.Status != syncpkg.StatusUpToDate && !userEditedDrift {
			t.Errorf("after apply, %s %s = %q; expected up-to-date except the user-edited drift", it.Kind, it.ID, it.Status)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// A body update must not silently delete frontmatter the workspace added to a
// framework note -- applySync rewrites the whole file, so the block has to be
// carried across.
func TestApplySyncPreservesWorkspaceFrontmatter(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	const v1Body = "# Note\n\n## Meta\n\n- **Tags:** `a`\n"
	const frontmatter = "---\ntitle: \"Note\"\ntags: [a, ai-instructions]\n---\n\n"
	mustWrite(t, filepath.Join(dir, "notes", "fw.md"), frontmatter+v1Body)

	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections:         map[string]manifest.Artifact{},
		// Recorded as the bare body: the note is untouched as far as cg is
		// concerned, because frontmatter is not part of the tracked content.
		Notes: map[string]manifest.Artifact{"notes/fw.md": {Hash: manifest.HashBody(v1Body)}},
	}
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}

	const v2Body = "# Note\n\n## Meta\n\n- **Tags:** `a`, `ai-instructions`\n"
	fw := map[string][]byte{"notes/fw.md": []byte(v2Body)}

	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", hashesOf(fw))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	if _, appliedN, aerr := applySync(dir, mf, report, nil, fw); aerr != nil {
		t.Fatalf("applySync: %v", aerr)
	} else if !contains(appliedN, "notes/fw.md") {
		t.Fatalf("note was not updated; appliedNotes = %v -- frontmatter must not block a body update", appliedN)
	}

	got := readFile(t, filepath.Join(dir, "notes", "fw.md"))
	if !strings.HasPrefix(got, frontmatter) {
		t.Errorf("workspace frontmatter was lost on update:\n%q", got)
	}
	if !strings.Contains(got, "`a`, `ai-instructions`") {
		t.Errorf("framework body was not applied:\n%q", got)
	}
}

// Note ids come from the manifest and the framework listing; one that escapes
// the workspace must be refused rather than written.
func TestApplySyncRejectsEscapingNoteID(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")
	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections:         map[string]manifest.Artifact{},
		Notes:            map[string]manifest.Artifact{},
	}
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}

	escaping := "../escaped.md"
	fw := map[string][]byte{escaping: []byte("pwned")}
	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", hashesOf(fw))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	if _, _, aerr := applySync(dir, mf, report, nil, fw); aerr == nil {
		t.Error("expected an error for a note id resolving outside the workspace")
	}
	if _, serr := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.md")); serr == nil {
		t.Error("a file was written outside the workspace")
	}
}

// A symlinked note must not be written through. filepath.Rel only validates
// the lexical path, so a link inside the workspace pointing at an external file
// would otherwise pass the check and os.WriteFile would overwrite the target.
func TestApplySyncRejectsSymlinkedNote(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside.md")

	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")
	const sentinel = "DO NOT OVERWRITE\n"
	mustWrite(t, outside, sentinel)

	notePath := filepath.Join(dir, "notes", "fw.md")
	if err := os.Symlink(outside, notePath); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections:         map[string]manifest.Artifact{},
		Notes:            map[string]manifest.Artifact{"notes/fw.md": {Hash: manifest.HashBody(sentinel)}},
	}
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}

	fw := map[string][]byte{"notes/fw.md": []byte("framework v2\n")}
	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", hashesOf(fw))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	if _, _, aerr := applySync(dir, mf, report, nil, fw); aerr == nil {
		t.Error("expected an error for a symlinked note")
	}
	if got := readFile(t, outside); got != sentinel {
		t.Errorf("wrote through the symlink; outside file is now %q", got)
	}
}

// A symlinked *directory* above the note is the same hazard one level up.
func TestApplySyncRejectsSymlinkedParentDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "ws")
	outsideDir := filepath.Join(base, "elsewhere")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "CLAUDE.md"), "# CLAUDE.md\n")
	if err := os.Symlink(outsideDir, filepath.Join(dir, "notes")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	mf := &manifest.Manifest{
		SchemaVersion:    manifest.SchemaVersion,
		FrameworkVersion: "1.0.0",
		Sections:         map[string]manifest.Artifact{},
		Notes:            map[string]manifest.Artifact{},
	}
	if err := mf.Save(dir); err != nil {
		t.Fatal(err)
	}

	fw := map[string][]byte{"notes/fw.md": []byte("framework v2\n")}
	report, err := buildSyncReport(dir, mf, "# CLAUDE.md\n", hashesOf(fw))
	if err != nil {
		t.Fatalf("buildSyncReport: %v", err)
	}
	if _, _, aerr := applySync(dir, mf, report, nil, fw); aerr == nil {
		t.Error("expected an error for a note under a symlinked directory")
	}
	if _, serr := os.Stat(filepath.Join(outsideDir, "fw.md")); serr == nil {
		t.Error("wrote into the symlinked directory's real target")
	}
}
