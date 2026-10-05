package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mnemcik/consigliere/internal/autoupdate"
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
	Long: `Reconcile this workspace's framework-managed content (CLAUDE.md sections,
framework notes, and the skills, slash commands, hook wrappers and status line
under .claude/) against the content shipped by the current cg binary.

This is the content side of upgrades — distinct from 'cg update', which would
replace the binary itself.

Without --apply, 'cg sync' is a dry run: it classifies each managed artifact and
prints what would change, writing nothing. With --apply, it updates framework
sections/notes you have not edited and adds new ones, but never clobbers an
artifact you have edited (those are reported for you to resolve).`,
	RunE: runSync,
}

// runSync reports, and with --apply writes, the reconciliation of the
// workspace's framework content with what this binary ships.
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
	frameworkFileBytes, err := embeddedClaudeFileContents()
	if err != nil {
		return err
	}
	report, err := buildSyncReport(dir, mf, frameworkCLAUDE, frameworkNotes, hashFiles(frameworkFileBytes))
	if err != nil {
		return err
	}

	if !syncApply {
		// The manifest is what sync itself records, so it is the accurate
		// framework version even for a workspace .cg.json still disagrees with.
		workspaceVer := mf.FrameworkVersion
		if workspaceVer == "" {
			workspaceVer = cfg.Version
		}
		printSyncReport(report, workspaceVer, Version)
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
	applied, err := applySync(dir, mf, report, manifest.ParseSections(frameworkCLAUDE), frameworkNoteBytes, frameworkFileBytes)
	if err != nil {
		return err
	}
	printApplySummary(report, applied)

	// The manifest now records this framework version; .cg.json must agree, or
	// cg sync and cg status keep showing the old one (consigliere#122). Only the
	// one value is patched: re-saving the whole config would drop any key this
	// binary does not know. A dev build has no version worth recording.
	// The sync itself is already written by now, so a .cg.json the patch cannot
	// handle (a version that is null or missing) is a warning, not a failure
	// that would repeat on every run.
	if cfg.Version != Version && autoupdate.IsReleaseVersion(Version) {
		if serr := workspace.SetVersion(dir, Version); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not record version %s in %s (%v); set \"version\" there by hand\n",
				Version, workspace.ConfigFile, serr)
		}
	}

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

// resolveInside returns the real path of target, confirming it stays inside
// realRoot. A lexical filepath.Rel check is not enough on its own: a symlinked
// note -- or a symlinked directory anywhere above it -- would pass the lexical
// test while os.WriteFile followed the link and overwrote a file outside the
// workspace. EvalSymlinks is applied to the deepest existing ancestor, since a
// brand-new note does not exist yet.
func resolveInside(realRoot, target string) (string, error) {
	probe := target
	var trailing []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			full := filepath.Join(append([]string{resolved}, trailing...)...)
			rel, relErr := filepath.Rel(realRoot, full)
			if relErr != nil || rel == ".." ||
				strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("%q resolves outside the workspace", target)
			}
			return full, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("resolving %q: %w", target, err)
		}
		trailing = append([]string{filepath.Base(probe)}, trailing...)
		probe = parent
	}
}

// applyResult lists what applySync wrote, by artifact kind.
type applyResult struct {
	Sections, Notes, Files []string
}

// applySync writes the safe changes (updatable + new sections, notes and
// .claude/ files) to the workspace, updates the manifest hashes for what it
// wrote, and bumps the recorded framework version. Drifted/removed/missing artifacts are never
// modified — they are reported (by the caller) for the user or the /cg-sync
// skill to resolve. It is idempotent: a second run finds everything up to date.
// Framework content is passed in (not read from the embed) so apply is testable.
func applySync(dir string, mf *manifest.Manifest, report syncpkg.Report, frameworkSections map[string]string, frameworkNoteBytes, frameworkFileBytes map[string][]byte) (applyResult, error) {
	var res applyResult
	// Sections: batch all edits to CLAUDE.md, write once.
	// Resolve the workspace root once so note paths can be checked against a
	// symlink-free root (a macOS /tmp workspace is itself behind a symlink).
	realRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return res, fmt.Errorf("resolving workspace root: %w", err)
	}

	// Check every note and file target before writing anything, so an unsafe
	// path (a symlinked .claude/skills/, say) fails the apply up front instead
	// of after CLAUDE.md is rewritten and before the manifest records it.
	for _, it := range report.Items {
		if it.Kind == syncpkg.KindSection ||
			(it.Status != syncpkg.StatusUpdatable && it.Status != syncpkg.StatusNew) {
			continue
		}
		if _, perr := writablePath(realRoot, dir, it.ID); perr != nil {
			return res, fmt.Errorf("%s %w", it.Kind, perr)
		}
	}

	claudePath := filepath.Join(dir, "CLAUDE.md")
	content, err := readFileAllowMissing(claudePath)
	if err != nil {
		return res, fmt.Errorf("reading CLAUDE.md: %w", err)
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
				res.Sections = append(res.Sections, it.ID)
			}
		case syncpkg.StatusNew:
			// A new section can already be on disk, byte-identical, in a workspace
			// whose manifest never recorded it: replace in place, so it is
			// recorded rather than appended a second time.
			if updated, replaced := manifest.ReplaceSection(content, it.ID, inner); replaced {
				content = updated
			} else {
				content = manifest.AppendSection(content, it.ID, inner)
			}
			sectionsChanged = true
			mf.Sections[it.ID] = manifest.Artifact{Hash: manifest.HashContent(inner)}
			res.Sections = append(res.Sections, it.ID)
		case syncpkg.StatusUpToDate, syncpkg.StatusDrifted, syncpkg.StatusRemoved, syncpkg.StatusMissing:
			// Never auto-applied: nothing to do (up-to-date) or needs the user/skill.
		}
	}
	if sectionsChanged {
		if werr := os.WriteFile(claudePath, []byte(content), 0o644); werr != nil {
			return res, fmt.Errorf("writing CLAUDE.md: %w", werr)
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
		notePath, perr := writablePath(realRoot, dir, it.ID)
		if perr != nil {
			return res, fmt.Errorf("note %w", perr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(notePath), 0o755); mkErr != nil {
			return res, fmt.Errorf("creating dir for %s: %w", it.ID, mkErr)
		}
		// The framework owns the body; any frontmatter on the note belongs to
		// the workspace (a derived `title:`, tags for an editor's tag pane).
		// Carry it across, or a body update would silently delete it.
		out := body
		if existing, rerr := os.ReadFile(notePath); rerr == nil { //nolint:gosec // notePath is confirmed inside dir above
			if fm, _ := manifest.SplitFrontmatter(string(existing)); fm != "" {
				_, newBody := manifest.SplitFrontmatter(string(body))
				// Keep the blank line between the block and the body. fm ends
				// at the closing `---` newline, so without this every synced
				// note would slowly lose its separator.
				sep := "\n"
				if strings.HasPrefix(newBody, "\n") {
					sep = ""
				}
				out = []byte(fm + sep + newBody)
			}
		}
		if werr := os.WriteFile(notePath, out, 0o644); werr != nil { //nolint:gosec // notePath is confirmed inside dir above
			return res, fmt.Errorf("writing %s: %w", it.ID, werr)
		}
		mf.Notes[it.ID] = manifest.Artifact{Hash: manifest.HashBody(string(out))}
		res.Notes = append(res.Notes, it.ID)
	}

	// .claude/ files: written whole, frontmatter included, with the mode the
	// framework installs them with (hook wrappers must stay executable).
	for _, it := range report.Items {
		if it.Kind != syncpkg.KindFile {
			continue
		}
		if it.Status != syncpkg.StatusUpdatable && it.Status != syncpkg.StatusNew {
			continue
		}
		body, ok := frameworkFileBytes[it.ID]
		if !ok {
			continue
		}
		filePath, perr := writablePath(realRoot, dir, it.ID)
		if perr != nil {
			return res, fmt.Errorf("file %w", perr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(filePath), 0o755); mkErr != nil {
			return res, fmt.Errorf("creating dir for %s: %w", it.ID, mkErr)
		}
		mode := frameworkFileMode(it.ID)
		if werr := os.WriteFile(filePath, body, mode); werr != nil { //nolint:gosec // filePath is confirmed inside dir by writablePath
			return res, fmt.Errorf("writing %s: %w", it.ID, werr)
		}
		// WriteFile only applies mode on create; make the bit stick on update.
		if cerr := os.Chmod(filePath, mode); cerr != nil {
			return res, fmt.Errorf("setting mode on %s: %w", it.ID, cerr)
		}
		if mf.Files == nil {
			mf.Files = make(map[string]manifest.Artifact)
		}
		mf.Files[it.ID] = manifest.Artifact{Hash: manifest.HashContent(string(body))}
		res.Files = append(res.Files, it.ID)
	}

	recordUpToDate(mf, report, frameworkSections, frameworkNoteBytes, frameworkFileBytes)

	mf.FrameworkVersion = Version
	if serr := mf.Save(dir); serr != nil {
		return res, fmt.Errorf("saving manifest: %w", serr)
	}

	return res, nil
}

// writablePath resolves a manifest id (a workspace-relative path) to the file
// apply may write. Ids come from the manifest
// and the framework listing, so it refuses anything that lands outside the
// workspace -- an id like "../../etc/x.md", or a path reached through a
// symlink -- and a symlink at the target itself, which os.WriteFile would
// follow, overwriting the link target instead.
func writablePath(realRoot, dir, id string) (string, error) {
	target := filepath.Join(dir, filepath.FromSlash(id))
	if _, err := resolveInside(realRoot, filepath.Dir(target)); err != nil {
		return "", fmt.Errorf("id %q: %w", id, err)
	}
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%q is a symlink; refusing to write through it", id)
	}
	return target, nil
}

// recordUpToDate points the manifest record of every up-to-date artifact at the
// framework's content. An artifact can be up to date with a stale record or
// none: the user resolved a drift by taking the framework copy, or a file the
// manifest never tracked already matched. Leaving the old record would make
// the next framework change read as a user edit.
func recordUpToDate(mf *manifest.Manifest, report syncpkg.Report, sections map[string]string, notes, files map[string][]byte) {
	for _, it := range report.Items {
		if it.Status != syncpkg.StatusUpToDate {
			continue
		}
		switch it.Kind {
		case syncpkg.KindSection:
			if inner, ok := sections[it.ID]; ok {
				if mf.Sections == nil {
					mf.Sections = make(map[string]manifest.Artifact)
				}
				mf.Sections[it.ID] = manifest.Artifact{Hash: manifest.HashContent(inner)}
			}
		case syncpkg.KindNote:
			if body, ok := notes[it.ID]; ok {
				if mf.Notes == nil {
					mf.Notes = make(map[string]manifest.Artifact)
				}
				mf.Notes[it.ID] = manifest.Artifact{Hash: manifest.HashBody(string(body))}
			}
		case syncpkg.KindFile:
			if body, ok := files[it.ID]; ok {
				if mf.Files == nil {
					mf.Files = make(map[string]manifest.Artifact)
				}
				mf.Files[it.ID] = manifest.Artifact{Hash: manifest.HashContent(string(body))}
			}
		}
	}
}

// frameworkFileMode is the mode cg installs a framework .claude/ file with.
func frameworkFileMode(id string) os.FileMode {
	for _, f := range frameworkClaudeFiles {
		if f.dst == id {
			return f.mode
		}
	}
	return 0o644
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

// embeddedClaudeFileContents returns the bytes of every framework .claude/ file
// this binary ships, keyed by workspace-relative path (matching manifest keys).
func embeddedClaudeFileContents() (map[string][]byte, error) {
	out := make(map[string][]byte, len(frameworkClaudeFiles))
	for _, f := range frameworkClaudeFiles {
		data, err := embeddedFS.ReadFile(f.src)
		if err != nil {
			return nil, fmt.Errorf("reading embedded %s: %w", f.src, err)
		}
		out[f.dst] = data
	}
	return out, nil
}

// hashFiles hashes whole-file content, keyed as given.
func hashFiles(files map[string][]byte) map[string]string {
	out := make(map[string]string, len(files))
	for id, data := range files {
		out[id] = manifest.HashContent(string(data))
	}
	return out
}

func printApplySummary(report syncpkg.Report, applied applyResult) {
	total := len(applied.Sections) + len(applied.Notes) + len(applied.Files)
	if total == 0 {
		fmt.Println("Nothing to apply — no untouched framework changes or new artifacts.")
	} else {
		fmt.Printf("Applied %d change(s): %d section(s), %d note(s), %d file(s).\n",
			total, len(applied.Sections), len(applied.Notes), len(applied.Files))
		for _, id := range applied.Sections {
			fmt.Printf("  updated section %s\n", id)
		}
		for _, id := range applied.Notes {
			fmt.Printf("  wrote note %s\n", id)
		}
		for _, id := range applied.Files {
			fmt.Printf("  wrote file %s\n", id)
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
// content (CLAUDE.md template, note hashes, .claude/ file hashes). Framework
// content is a parameter rather than read here so the classification is
// testable in isolation.
func buildSyncReport(dir string, mf *manifest.Manifest, frameworkCLAUDE string, frameworkNotes, frameworkFiles map[string]string) (syncpkg.Report, error) {
	onDiskSections, err := sectionHashesFromFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		return syncpkg.Report{}, fmt.Errorf("reading workspace CLAUDE.md: %w", err)
	}
	frameworkSections := hashSections(frameworkCLAUDE)

	recordedNotes := artifactHashes(mf.Notes)
	onDiskNotes := onDiskHashes(dir, recordedNotes, frameworkNotes, manifest.HashBody)

	recordedFiles := artifactHashes(mf.Files)
	onDiskFiles := onDiskHashes(dir, recordedFiles, frameworkFiles, manifest.HashContent)
	if !claudeFilesInstalled(recordedFiles, onDiskFiles, frameworkFiles) {
		// The workspace declined Claude Code integration at `cg init` (the
		// wizard's slash-command question). That answer is not stored, so infer
		// it, and do not install the files behind the user's back.
		frameworkFiles = nil
		onDiskFiles = onDiskHashes(dir, recordedFiles, nil, manifest.HashContent)
	}
	// A file with no record (the workspace predates cg tracking .claude/ files,
	// or an older binary rewrote the manifest without them) whose bytes match a
	// version cg once shipped is unedited: treat that version as the record, so
	// it classifies as updatable instead of drifted.
	for id, onDisk := range onDiskFiles {
		if _, ok := recordedFiles[id]; ok {
			continue
		}
		if fw, ok := frameworkFiles[id]; ok && onDisk != fw && knownClaudeFileHash(id, onDisk) {
			recordedFiles[id] = onDisk
		}
	}

	return syncpkg.Classify(
		onDiskSections, artifactHashes(mf.Sections), frameworkSections,
		onDiskNotes, recordedNotes, frameworkNotes,
		onDiskFiles, recordedFiles, frameworkFiles,
	), nil
}

// claudeFilesInstalled reports whether the workspace uses cg's .claude/ files at
// all: any of them recorded in the manifest, or any of them on disk with bytes
// cg shipped. A file of the user's own at one of these paths does not count,
// so a statusline.sh someone wrote does not opt them in to the rest.
func claudeFilesInstalled(recorded, onDisk, framework map[string]string) bool {
	if len(recorded) > 0 {
		return true
	}
	for id, h := range onDisk {
		if h == framework[id] || knownClaudeFileHash(id, h) {
			return true
		}
	}
	return false
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

// onDiskHashes hashes each workspace file referenced by either the manifest or
// the framework with hash; a path absent on disk is omitted (→ missing/new).
// Notes hash their body (workspace frontmatter is not framework content);
// .claude/ files hash everything.
func onDiskHashes(dir string, recorded, framework map[string]string, hash func(string) string) map[string]string {
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
			out[relPath] = hash(string(data))
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
	{syncpkg.StatusNew, "New (framework adds these, or they already match and get recorded)"},
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

	fmt.Println("(dry run — no changes written. Run `cg sync --apply` to apply the safe changes.)")
}
