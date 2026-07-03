package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/manifest"
	syncpkg "github.com/mnemcik/consigliere/internal/sync"
	"github.com/mnemcik/consigliere/internal/workspace"
	"github.com/spf13/cobra"
)

var syncApply bool

func init() {
	syncCmd.Flags().BoolVar(&syncApply, "apply", false,
		"apply the safe changes (update untouched framework sections/notes, add new ones); drifted artifacts are never touched")
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile the workspace's framework content with this cg version",
	Long: `Reconcile this workspace's framework-managed content (CLAUDE.md sections and
framework notes) against the content shipped by the current cg binary.

This is the content side of upgrades — distinct from 'cg update', which would
replace the binary itself.

Without --apply, 'cg sync' is a dry run: it classifies each managed artifact and
prints what would change, writing nothing. With --apply, it updates framework
sections/notes you have not edited and adds new ones, but never clobbers an
artifact you have edited (those are reported for you to resolve).`,
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

	if !syncApply {
		printSyncReport(report, cfg.Version, Version)
		pending, herr := extension.NormalizeHookCommands(dir, false)
		if herr != nil {
			return fmt.Errorf("checking hook command paths: %w", herr)
		}
		printHookNormalizeDryRun(pending)
		return nil
	}

	frameworkNoteBytes, err := embeddedNoteContents()
	if err != nil {
		return err
	}
	appliedSections, appliedNotes, err := applySync(dir, mf, report, manifest.ParseSections(frameworkCLAUDE), frameworkNoteBytes)
	if err != nil {
		return err
	}
	printApplySummary(report, appliedSections, appliedNotes)

	normalized, herr := extension.NormalizeHookCommands(dir, true)
	if herr != nil {
		return fmt.Errorf("normalizing hook command paths: %w", herr)
	}
	printHookNormalizeApplied(normalized)
	return nil
}

// printHookNormalizeDryRun reports settings.json hook/statusLine commands that
// `cg sync --apply` would pin to $CLAUDE_PROJECT_DIR (cwd-independent).
func printHookNormalizeDryRun(pending []string) {
	if len(pending) == 0 {
		return
	}
	fmt.Printf("\nHook command paths to pin to $CLAUDE_PROJECT_DIR (%d):\n", len(pending))
	for _, cmd := range pending {
		fmt.Printf("  - %s\n", cmd)
	}
	fmt.Println("(run `cg sync --apply` to rewrite them)")
}

// printHookNormalizeApplied reports the settings.json commands that were pinned.
func printHookNormalizeApplied(normalized []string) {
	if len(normalized) == 0 {
		return
	}
	fmt.Printf("\nPinned %d hook command path(s) to $CLAUDE_PROJECT_DIR:\n", len(normalized))
	for _, cmd := range normalized {
		fmt.Printf("  - %s → %s\n", cmd, extension.PinnedCommand(cmd))
	}
}

// applySync writes the safe changes (updatable + new sections and notes) to the
// workspace, updates the manifest hashes for what it wrote, and bumps the
// recorded framework version. Drifted/removed/missing artifacts are never
// modified — they are reported (by the caller) for the user or the /cg-sync
// skill to resolve. It is idempotent: a second run finds everything up to date.
// Framework content is passed in (not read from the embed) so apply is testable.
func applySync(dir string, mf *manifest.Manifest, report syncpkg.Report, frameworkSections map[string]string, frameworkNoteBytes map[string][]byte) (appliedSections, appliedNotes []string, err error) {
	// Sections: batch all edits to CLAUDE.md, write once.
	claudePath := filepath.Join(dir, "CLAUDE.md")
	content, err := readFileAllowMissing(claudePath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading CLAUDE.md: %w", err)
	}
	sectionsChanged := false
	for _, it := range report.Items {
		if it.Kind != syncpkg.KindSection {
			continue
		}
		inner, ok := frameworkSections[it.ID]
		if !ok {
			continue
		}
		switch it.Status {
		case syncpkg.StatusUpdatable:
			if updated, replaced := manifest.ReplaceSection(content, it.ID, inner); replaced {
				content = updated
				sectionsChanged = true
				mf.Sections[it.ID] = manifest.Artifact{Hash: manifest.HashContent(inner)}
				appliedSections = append(appliedSections, it.ID)
			}
		case syncpkg.StatusNew:
			content = manifest.AppendSection(content, it.ID, inner)
			sectionsChanged = true
			mf.Sections[it.ID] = manifest.Artifact{Hash: manifest.HashContent(inner)}
			appliedSections = append(appliedSections, it.ID)
		case syncpkg.StatusUpToDate, syncpkg.StatusDrifted, syncpkg.StatusRemoved, syncpkg.StatusMissing:
			// Never auto-applied: nothing to do (up-to-date) or needs the user/skill.
		}
	}
	if sectionsChanged {
		if werr := os.WriteFile(claudePath, []byte(content), 0o644); werr != nil {
			return nil, nil, fmt.Errorf("writing CLAUDE.md: %w", werr)
		}
	}

	// Notes: write each updated/new file (whole-file).
	for _, it := range report.Items {
		if it.Kind != syncpkg.KindNote {
			continue
		}
		if it.Status != syncpkg.StatusUpdatable && it.Status != syncpkg.StatusNew {
			continue
		}
		body, ok := frameworkNoteBytes[it.ID]
		if !ok {
			continue
		}
		notePath := filepath.Join(dir, filepath.FromSlash(it.ID))
		if mkErr := os.MkdirAll(filepath.Dir(notePath), 0o755); mkErr != nil {
			return nil, nil, fmt.Errorf("creating dir for %s: %w", it.ID, mkErr)
		}
		if werr := os.WriteFile(notePath, body, 0o644); werr != nil {
			return nil, nil, fmt.Errorf("writing %s: %w", it.ID, werr)
		}
		mf.Notes[it.ID] = manifest.Artifact{Hash: manifest.HashContent(string(body))}
		appliedNotes = append(appliedNotes, it.ID)
	}

	mf.FrameworkVersion = Version
	if serr := mf.Save(dir); serr != nil {
		return nil, nil, fmt.Errorf("saving manifest: %w", serr)
	}

	return appliedSections, appliedNotes, nil
}

// readFileAllowMissing returns the file content, or "" if the file is absent.
func readFileAllowMissing(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// embeddedNoteContents returns the raw bytes of every framework note shipped in
// the embed tree, keyed by workspace-relative path (matching manifest keys).
func embeddedNoteContents() (map[string][]byte, error) {
	out := map[string][]byte{}
	notesSub, err := fs.Sub(embeddedFS, notesEmbedRoot)
	if err != nil {
		return out, nil // no notes shipped
	}
	walkErr := fs.WalkDir(notesSub, ".", func(p string, d fs.DirEntry, we error) error {
		if we != nil {
			return we
		}
		if d.IsDir() || strings.HasPrefix(filepath.Base(p), ".") {
			return nil
		}
		body, rerr := fs.ReadFile(notesSub, p)
		if rerr != nil {
			return rerr
		}
		out[dirNotes+"/"+p] = body
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("reading embedded notes: %w", walkErr)
	}
	return out, nil
}

func printApplySummary(report syncpkg.Report, appliedSections, appliedNotes []string) {
	total := len(appliedSections) + len(appliedNotes)
	if total == 0 {
		fmt.Println("Nothing to apply — no untouched framework changes or new artifacts.")
	} else {
		fmt.Printf("Applied %d change(s): %d section(s), %d note(s).\n", total, len(appliedSections), len(appliedNotes))
		for _, id := range appliedSections {
			fmt.Printf("  updated section %s\n", id)
		}
		for _, id := range appliedNotes {
			fmt.Printf("  wrote note %s\n", id)
		}
	}

	// Surface what was deliberately left alone.
	byStatus := report.ByStatus()
	if left := byStatus[syncpkg.StatusDrifted]; len(left) > 0 {
		fmt.Printf("\n%d artifact(s) you edited were left untouched — resolve manually:\n", len(left))
		for _, it := range left {
			fmt.Printf("  - %s %s\n", it.Kind, it.ID)
		}
	}
	for _, st := range []struct {
		s     syncpkg.Status
		label string
	}{
		{syncpkg.StatusMissing, "managed but missing from disk"},
		{syncpkg.StatusRemoved, "no longer shipped by the framework"},
	} {
		if items := byStatus[st.s]; len(items) > 0 {
			fmt.Printf("\n%d artifact(s) %s:\n", len(items), st.label)
			for _, it := range items {
				fmt.Printf("  - %s %s\n", it.Kind, it.ID)
			}
		}
	}
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
