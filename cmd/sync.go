package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mnemcik/consigliere/internal/manifest"
	syncpkg "github.com/mnemcik/consigliere/internal/sync"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Report how the workspace's framework content compares to this cg version",
	Long: `Reconcile this workspace's framework-managed content (CLAUDE.md sections and
framework notes) against the content shipped by the current cg binary.

This is the content side of upgrades — distinct from 'cg update', which would
replace the binary itself. 'cg sync' is currently report-only (a dry run): it
classifies each managed artifact and prints what would change. It never writes.`,
	RunE: runSync,
}

func runSync(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, err := workspace.Detect(dir)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	if cfg == nil {
		fmt.Println("Not a Consigliere workspace.")
		fmt.Println("Run `cg init` to set one up.")
		return nil
	}

	mf, err := manifest.Load(dir)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	if mf == nil {
		fmt.Println("This workspace has no sync manifest (.cg/manifest.json).")
		fmt.Println("It predates `cg sync`; run `cg init --force` to seed one, then re-run `cg sync`.")
		return nil
	}

	frameworkCLAUDE, frameworkNotes, err := embeddedFramework()
	if err != nil {
		return err
	}
	report, err := buildSyncReport(dir, mf, frameworkCLAUDE, frameworkNotes)
	if err != nil {
		return err
	}
	printSyncReport(report, cfg.Version, Version)
	return nil
}

// embeddedFramework returns the framework content this cg binary ships: the
// workspace CLAUDE.md template and the note hashes from the embed tree.
func embeddedFramework() (claudeMD string, noteHashes map[string]string, err error) {
	data, err := embeddedFS.ReadFile(claudeEmbedPath)
	if err != nil {
		return "", nil, fmt.Errorf("reading embedded CLAUDE.md: %w", err)
	}
	notes := map[string]string{}
	if notesSub, serr := fs.Sub(embeddedFS, notesEmbedRoot); serr == nil {
		artifacts, nerr := manifest.NotesFromFS(notesSub, dirNotes)
		if nerr != nil {
			return "", nil, fmt.Errorf("hashing embedded notes: %w", nerr)
		}
		notes = artifactHashes(artifacts)
	}
	return string(data), notes, nil
}

// buildSyncReport classifies every managed artifact by comparing the workspace's
// on-disk content, the manifest's recorded hashes, and the supplied framework
// content (CLAUDE.md template + note hashes). Framework content is a parameter
// rather than read here so the classification is testable in isolation.
func buildSyncReport(dir string, mf *manifest.Manifest, frameworkCLAUDE string, frameworkNotes map[string]string) (syncpkg.Report, error) {
	onDiskSections, err := sectionHashesFromFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		return syncpkg.Report{}, fmt.Errorf("reading workspace CLAUDE.md: %w", err)
	}
	frameworkSections := hashSections(frameworkCLAUDE)

	recordedNotes := artifactHashes(mf.Notes)
	onDiskNotes := onDiskNoteHashes(dir, recordedNotes, frameworkNotes)

	return syncpkg.Classify(
		onDiskSections, artifactHashes(mf.Sections), frameworkSections,
		onDiskNotes, recordedNotes, frameworkNotes,
	), nil
}

// sectionHashesFromFile parses the CLAUDE.md at path and returns id→hash. A
// missing file yields an empty map (the workspace lost its CLAUDE.md).
func sectionHashesFromFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return hashSections(string(data)), nil
}

// hashSections parses cg:section blocks from content and hashes each inner body.
func hashSections(content string) map[string]string {
	out := map[string]string{}
	for id, inner := range manifest.ParseSections(content) {
		out[id] = manifest.HashContent(inner)
	}
	return out
}

// onDiskNoteHashes hashes each workspace note file referenced by either the
// manifest or the framework; a path absent on disk is omitted (→ missing/new).
func onDiskNoteHashes(dir string, recorded, framework map[string]string) map[string]string {
	out := map[string]string{}
	seen := map[string]struct{}{}
	for _, set := range []map[string]string{recorded, framework} {
		for relPath := range set {
			if _, done := seen[relPath]; done {
				continue
			}
			seen[relPath] = struct{}{}
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(relPath)))
			if err != nil {
				continue // missing on disk
			}
			out[relPath] = manifest.HashContent(string(data))
		}
	}
	return out
}

func artifactHashes(m map[string]manifest.Artifact) map[string]string {
	out := make(map[string]string, len(m))
	for k, a := range m {
		out[k] = a.Hash
	}
	return out
}

// reportSections is the human-readable ordering and labelling of statuses.
var reportSections = []struct {
	status syncpkg.Status
	label  string
}{
	{syncpkg.StatusUpdatable, "Will update (framework changed, you didn't edit these)"},
	{syncpkg.StatusNew, "New (framework adds these)"},
	{syncpkg.StatusDrifted, "Drifted (you edited these — left untouched, resolve manually)"},
	{syncpkg.StatusMissing, "Missing (managed but gone from disk)"},
	{syncpkg.StatusRemoved, "Removed (framework no longer ships these)"},
}

func printSyncReport(report syncpkg.Report, workspaceVer, binaryVer string) {
	fmt.Printf("Workspace framework version: %s\n", workspaceVer)
	fmt.Printf("This cg binary ships:        %s\n\n", binaryVer)

	if !report.Actionable() {
		fmt.Println("Everything is up to date — nothing to sync.")
		return
	}

	byStatus := report.ByStatus()
	for _, sec := range reportSections {
		items := byStatus[sec.status]
		if len(items) == 0 {
			continue
		}
		fmt.Printf("%s:\n", sec.label)
		for _, it := range items {
			fmt.Printf("  - %s %s\n", it.Kind, it.ID)
		}
		fmt.Println()
	}

	fmt.Println("(dry run — no changes written. Apply is coming in a later release.)")
}
