package cmd

import (
	"os"
	"path/filepath"
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
