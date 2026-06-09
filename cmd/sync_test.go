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
