package cmd

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/extension"
	"github.com/mnemcik/consigliere/internal/manifest"
	"github.com/mnemcik/consigliere/internal/wizard"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// validSlug matches a canonical area slug: lowercase letters/digits separated
// by single dashes. Used as a defense-in-depth check before writing files
// whose path is derived from the slug.
var validSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

//go:embed all:embed_templates
var embeddedFS embed.FS

// gateTemplateRel is the workspace-relative path of the editable session-gate
// template; the generated .cg.json points session.gateTemplate at it.
const gateTemplateRel = ".claude/cg/session-gate.md"

var (
	forceInit  bool
	wizardInit bool
)

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Re-initialize (preserves CLAUDE.md and PROFILE.md)")
	initCmd.Flags().BoolVarP(&wizardInit, "wizard", "i", false, "Run the interactive setup wizard (TTY required)")
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Bootstrap a new Consigliere workspace",
	Long:  "Create the folder structure, templates, index files, .cg.json, CLAUDE.md, and PROFILE.md in the current directory.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine working directory: %w", err)
	}

	// Guard: check for existing workspace
	cfg, err := workspace.Detect(dir)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", workspace.ConfigFile, err)
	}
	if cfg != nil && !forceInit {
		fmt.Printf("This directory is already a Consigliere workspace (version %s).\n", cfg.Version)
		fmt.Println("Use `cg init --force` to re-initialize.")
		return nil
	}

	var answers wizard.Answers
	answers.InstallSlash = true // default for non-wizard path
	if wizardInit {
		a, werr := wizard.Run()
		if werr != nil {
			if errors.Is(werr, wizard.ErrNotATTY) {
				return fmt.Errorf("--wizard requires a TTY; re-run without the flag for non-interactive init")
			}
			return werr
		}
		answers = a
	}

	var created, skipped []string

	// Create directories
	dirs := []string{
		dirProjects,
		dirAreas,
		dirIdeas,
		dirNotes,
		dirInsights,
		"templates",
		filepath.Join("templates", "project"),
	}
	for _, d := range dirs {
		path := filepath.Join(dir, d)
		if dirExists(path) {
			skipped = append(skipped, d+"/")
		} else {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", d, err)
			}
			created = append(created, d+"/")
		}
	}

	// Copy content templates
	contentTemplates := map[string]string{
		"embed_templates/idea.md":               filepath.Join("templates", "idea.md"),
		"embed_templates/note.md":               filepath.Join("templates", "note.md"),
		"embed_templates/insight.md":            filepath.Join("templates", "insight.md"),
		"embed_templates/area.md":               filepath.Join("templates", "area.md"),
		"embed_templates/subagent-briefing.md":  filepath.Join("templates", "subagent-briefing.md"),
		"embed_templates/project/README.md":     filepath.Join("templates", "project", "README.md"),
		"embed_templates/project/decisions.md":  filepath.Join("templates", "project", "decisions.md"),
		"embed_templates/project/todo.md":       filepath.Join("templates", "project", "todo.md"),
		"embed_templates/project/log.md":        filepath.Join("templates", "project", "log.md"),
		"embed_templates/project/references.md": filepath.Join("templates", "project", "references.md"),
		"embed_templates/project/resume.md":     filepath.Join("templates", "project", "resume.md"),
	}
	for src, dst := range contentTemplates {
		c, s := copyEmbeddedFile(dir, src, dst, false)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// Create index files
	indexFiles := map[string]string{
		filepath.Join(dirProjects, "TODO.md"):   indexProjectsTODO,
		filepath.Join(dirAreas, "INDEX.md"):     indexAreas,
		filepath.Join(dirIdeas, "BACKLOG.md"):   indexIdeas,
		filepath.Join(dirNotes, "INDEX.md"):     indexNotes,
		filepath.Join(dirInsights, "DRAFTS.md"): indexInsights,
	}
	for dst, content := range indexFiles {
		c, s := writeFileIfNotExists(dir, dst, content)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// Create .cg.json. On re-init (--force) preserve the installed extensions
	// list so the rewrite doesn't drop it; re-cloned workspaces re-install them
	// below.
	var priorExtensions []workspace.ExtensionRef
	var priorSession *workspace.SessionConfig
	if cfg != nil {
		priorExtensions = cfg.Extensions
		priorSession = cfg.Session
	}
	homeDir, _ := os.UserHomeDir()
	// Seed the public registry under the built-in "cg" alias so `cg extension
	// install cg/<name>` works out of the box. On re-init preserve any
	// user-configured registries, only adding "cg" if it's missing.
	registries := map[string]string{}
	if cfg != nil {
		for alias, src := range cfg.Registries {
			registries[alias] = src
		}
	}
	if _, ok := registries[extension.BuiltinRegistryAlias]; !ok {
		registries[extension.BuiltinRegistryAlias] = extension.DefaultRegistryURL
	}
	cgJSON := workspace.Config{
		Type:    workspace.TypeConsigliere,
		Version: Version,
		Indexes: map[string]string{
			dirProjects: indexProjectsPath,
			dirAreas:    "areas/INDEX.md",
			dirIdeas:    "ideas/BACKLOG.md",
			dirNotes:    "notes/INDEX.md",
			dirInsights: "insights/DRAFTS.md",
		},
		// Point the session-start gate at the editable template shipped under
		// .claude/cg/ so customizing the wording needs no binary rebuild.
		// statuslineUpstream is seeded from the user's prior statusLine command
		// so the badge layers on top of their existing status line rather than
		// replacing it with a bare badge.
		Session: &workspace.SessionConfig{
			GateTemplate:       gateTemplateRel,
			StatuslineUpstream: detectStatuslineUpstream(priorSession, homeDir),
		},
		Extensions: priorExtensions,
		Registries: registries,
	}
	data, _ := json.MarshalIndent(cgJSON, "", "  ")
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, workspace.ConfigFile), data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", workspace.ConfigFile, err)
	}
	if cfg != nil {
		// Was re-init (--force), overwritten
		created = append(created, workspace.ConfigFile+" (overwritten)")
	} else {
		created = append(created, workspace.ConfigFile)
	}

	// CLAUDE.md
	claudeSrc := claudeEmbedPath
	claudeDst := "CLAUDE.md"
	if forceInit && fileExists(filepath.Join(dir, claudeDst)) {
		// Don't overwrite, save as template reference
		c, _ := copyEmbeddedFile(dir, claudeSrc, "CLAUDE.cg-template.md", true)
		created = append(created, c...)
		skipped = append(skipped, "CLAUDE.md (preserved, template saved to CLAUDE.cg-template.md)")
	} else {
		c, s := copyEmbeddedFile(dir, claudeSrc, claudeDst, false)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// Framework notes live under notesEmbedRoot in the embed tree; the same
	// sub-FS drives both the on-disk copy and the manifest registration so they
	// can never disagree. Empty today — the directory carries only a .gitkeep
	// until the load-on-demand work ships framework notes.
	notesSub, notesErr := fs.Sub(embeddedFS, notesEmbedRoot)
	notesCopyOK := false
	if notesErr != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot read embedded notes: %v\n", notesErr)
	} else {
		// Copy notes into the workspace (skip-if-exists, like CLAUDE.md) so every
		// manifest note record has a backing file.
		nc, ns, cerr := copyFrameworkNotes(notesSub, dir, dirNotes)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot copy framework notes: %v\n", cerr)
		} else {
			notesCopyOK = true
		}
		created = append(created, nc...)
		skipped = append(skipped, ns...)
	}

	// Seed the workspace-sync manifest: record the framework-managed CLAUDE.md
	// sections and the framework notes, each with a content hash at this
	// framework version, so a future `cg sync` can distinguish framework-shipped
	// content from the user's edits. The manifest is derived from the embedded
	// templates (the canonical content for this version), not the on-disk copies,
	// which may be preserved user copies under --force. See docs/workspace-sync.md.
	if claudeMD, rerr := embeddedFS.ReadFile(claudeSrc); rerr != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot read embedded CLAUDE.md for manifest: %v\n", rerr)
	} else {
		mf := manifest.FromCLAUDE(string(claudeMD), Version)
		// Only register notes in the manifest if the copy succeeded, so a manifest
		// entry never points at a file that was not written.
		if notesErr == nil && notesCopyOK {
			if notes, nerr := manifest.NotesFromFS(notesSub, dirNotes); nerr != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot hash embedded notes for manifest: %v\n", nerr)
			} else {
				mf.Notes = notes
			}
		}
		manifestRel := filepath.Join(manifest.Dir, manifest.File)
		if werr := mf.Save(dir); werr != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot write %s: %v\n", manifestRel, werr)
		} else {
			created = append(created, manifestRel)
		}
	}

	// PROFILE.md — wizard answers, if provided, override the default template.
	profilePath := filepath.Join(dir, "PROFILE.md")
	if wizardInit {
		if fileExists(profilePath) && !forceInit {
			skipped = append(skipped, "PROFILE.md")
		} else {
			if err := os.WriteFile(profilePath, []byte(wizard.RenderProfile(&answers)), 0o644); err != nil {
				return fmt.Errorf("writing PROFILE.md: %w", err)
			}
			created = append(created, "PROFILE.md (from wizard)")
		}
	} else {
		c, s := copyEmbeddedFile(dir, "embed_templates/workspace/PROFILE.md", "PROFILE.md", false)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// .gitignore
	c, s := copyEmbeddedFile(dir, "embed_templates/workspace/.gitignore", ".gitignore", false)
	created = append(created, c...)
	skipped = append(skipped, s...)

	// Claude Code slash commands (.claude/commands/)
	if answers.InstallSlash {
		commands := map[string]string{
			"embed_templates/commands/match-project.md": filepath.Join(".claude", "commands", "match-project.md"),
			"embed_templates/commands/cg-init.md":       filepath.Join(".claude", "commands", "cg-init.md"),
			"embed_templates/commands/cg-sync.md":       filepath.Join(".claude", "commands", "cg-sync.md"),
		}
		for src, dst := range commands {
			c, s := copyEmbeddedFile(dir, src, dst, forceInit)
			created = append(created, c...)
			skipped = append(skipped, s...)
		}

		// Claude Code skills (.claude/skills/<name>/SKILL.md). Skills ship and
		// version with the binary (no per-skill version); the wrap skill moved
		// here from the standalone marketplace plugin.
		skills := map[string]string{
			"embed_templates/skills/wrap/SKILL.md": filepath.Join(".claude", "skills", "wrap", "SKILL.md"),
		}
		for src, dst := range skills {
			c, s := copyEmbeddedFile(dir, src, dst, forceInit)
			created = append(created, c...)
			skipped = append(skipped, s...)
		}

		// Claude Code hook wrappers + status line: framework-owned, so --force
		// rewrites them (and they carry the executable bit). They delegate to the
		// cg binary (DEC-004); a missing cg degrades to a no-op, not a hook error.
		wrappers := map[string]string{
			"embed_templates/workspace/.claude/hooks/session-start-gate.sh":        filepath.Join(".claude", "hooks", "session-start-gate.sh"),
			"embed_templates/workspace/.claude/hooks/mark-session-dirty.sh":        filepath.Join(".claude", "hooks", "mark-session-dirty.sh"),
			"embed_templates/workspace/.claude/hooks/pull-latest-main.sh":          filepath.Join(".claude", "hooks", "pull-latest-main.sh"),
			"embed_templates/workspace/.claude/hooks/external-repo-push-policy.sh": filepath.Join(".claude", "hooks", "external-repo-push-policy.sh"),
			"embed_templates/workspace/.claude/statusline.sh":                      filepath.Join(".claude", "statusline.sh"),
		}
		for src, dst := range wrappers {
			c, s := copyEmbeddedExecutable(dir, src, dst, forceInit)
			created = append(created, c...)
			skipped = append(skipped, s...)
		}

		// settings.json (hook/statusLine wiring) and the gate template are
		// user-customizable, so they are never clobbered — even on --force.
		userOwned := map[string]string{
			"embed_templates/workspace/.claude/settings.json":      filepath.Join(".claude", "settings.json"),
			"embed_templates/workspace/.claude/cg/session-gate.md": filepath.Join(".claude", "cg", "session-gate.md"),
		}
		for src, dst := range userOwned {
			c, s := copyEmbeddedFile(dir, src, dst, false)
			created = append(created, c...)
			skipped = append(skipped, s...)
		}
	}

	// Wizard-only post-bootstrap steps: first area + optional git init.
	if wizardInit && answers.HasFirstArea() {
		if !validSlug.MatchString(answers.AreaSlug) {
			return fmt.Errorf("invalid area slug %q: expected lowercase letters, digits, and single dashes", answers.AreaSlug)
		}
		today := time.Now().Format("2006-01-02")
		areaRel := filepath.Join("areas", answers.AreaSlug+".md")
		areaPath := filepath.Join(dir, areaRel)
		if fileExists(areaPath) {
			skipped = append(skipped, areaRel)
		} else {
			if err := os.WriteFile(areaPath, []byte(wizard.RenderArea(&answers, today)), 0o644); err != nil {
				return fmt.Errorf("writing area file: %w", err)
			}
			created = append(created, areaRel)
		}

		indexPath := filepath.Join(dir, "areas", "INDEX.md")
		existing, err := os.ReadFile(indexPath) //nolint:gosec // indexPath is a fixed name under dir
		if err != nil {
			return fmt.Errorf("reading areas/INDEX.md: %w", err)
		}
		updated := wizard.InsertAreaIndexRow(string(existing), &answers)
		switch {
		case updated == string(existing):
			// Idempotent re-run (row already present) or Areas section
			// missing from the template. Surface as skipped, not created.
			skipped = append(skipped, "areas/INDEX.md (row already present or section missing)")
		default:
			if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil { //nolint:gosec // indexPath is a fixed name under dir
				return fmt.Errorf("updating areas/INDEX.md: %w", err)
			}
			created = append(created, "areas/INDEX.md (row added)")
		}
	}

	if wizardInit && answers.RunGitInit {
		gitDir := filepath.Join(dir, ".git")
		_, statErr := os.Stat(gitDir)
		switch {
		case statErr == nil:
			skipped = append(skipped, ".git/ (already a git repo)")
		case errors.Is(statErr, os.ErrNotExist):
			gitCmd := exec.CommandContext(cmd.Context(), "git", "init")
			gitCmd.Dir = dir
			if out, err := gitCmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: git init failed: %v\n%s\n", err, out)
			} else {
				created = append(created, ".git/ (git init)")
			}
		default:
			fmt.Fprintf(os.Stderr, "warning: cannot stat .git: %v\n", statErr)
		}
	}

	// Re-install any extensions recorded in .cg.json whose machine-shared clone
	// is missing (the re-cloned-workspace scenario). Present clones are left as-is.
	reinstalled, rerr := reinstallMissingExtensions(cmd, dir, priorExtensions)
	if rerr != nil {
		return rerr
	}
	created = append(created, reinstalled...)

	// Summary
	fmt.Println()
	fmt.Println("## Consigliere workspace initialized")
	fmt.Println()
	fmt.Printf("**Version:** %s\n", Version)
	fmt.Println()

	if len(created) > 0 {
		fmt.Println("### Created")
		for _, f := range created {
			fmt.Printf("- %s\n", f)
		}
		fmt.Println()
	}

	if len(skipped) > 0 {
		fmt.Println("### Skipped (already existed)")
		for _, f := range skipped {
			fmt.Printf("- %s\n", f)
		}
		fmt.Println()
	}

	fmt.Println("### Next steps")
	if wizardInit {
		fmt.Println("1. Review `PROFILE.md` and fill any remaining placeholders")
		fmt.Println("2. Edit the `Purpose` and `Area Tags` sections in `CLAUDE.md`")
		if answers.HasFirstArea() {
			fmt.Printf("3. Flesh out `areas/%s.md` with key systems, contacts, and constraints\n", answers.AreaSlug)
		} else {
			fmt.Println("3. Define your first area in `areas/` using `templates/area.md`")
		}
		if !answers.RunGitInit {
			fmt.Println("4. Run `git init` if this is not yet a git repository")
		}
		fmt.Println("5. Commit the initial workspace structure")
	} else {
		fmt.Println("1. Edit `PROFILE.md` with your role, responsibilities, and context")
		fmt.Println("2. Edit the `Purpose` and `Area Tags` sections in `CLAUDE.md`")
		fmt.Println("3. Define your first area in `areas/` using `templates/area.md`")
		fmt.Println("4. Run `git init` if this is not yet a git repository")
		fmt.Println("5. Commit the initial workspace structure")
		fmt.Println()
		fmt.Println("Tip: re-run with `cg init --wizard` for an interactive walkthrough.")
	}

	return nil
}

// detectStatuslineUpstream returns the statuslineUpstream command to record in a
// freshly written .cg.json. On re-init it preserves any prior value (the user
// may have customized it). Otherwise it reads the user's global
// ~/.claude/settings.json statusLine command so the area/project badge layers on
// top of the user's existing status line instead of replacing it with a bare
// badge. Returns "" when there is nothing to preserve or the prior command would
// recurse into the badge renderer.
func detectStatuslineUpstream(prior *workspace.SessionConfig, homeDir string) string {
	if prior != nil && prior.StatuslineUpstream != "" {
		return prior.StatuslineUpstream
	}
	if homeDir == "" {
		return ""
	}
	cmd := readStatusLineCommand(filepath.Join(homeDir, ".claude", "settings.json"))
	if isRecursiveStatusline(cmd) {
		return ""
	}
	return cmd
}

// readStatusLineCommand returns the statusLine.command string from a Claude Code
// settings.json, or "" on any error (missing file, malformed JSON, no field).
func readStatusLineCommand(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // path derived from the user's home dir
	if err != nil {
		return ""
	}
	var s struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return ""
	}
	return s.StatusLine.Command
}

// isRecursiveStatusline reports whether a candidate upstream command would call
// the cg badge renderer, which must never be its own upstream. We match the
// renderer invocation ("cg session statusline") rather than the wrapper's
// basename: the user's real status line typically lives at ~/.claude/statusline.sh
// (e.g. "bash ~/.claude/statusline.sh"), which shares the cg wrapper's basename
// but is exactly what we want to preserve. An empty command is treated as
// recursive (nothing to preserve).
func isRecursiveStatusline(cmd string) bool {
	if cmd == "" {
		return true
	}
	return strings.Contains(cmd, "cg session statusline")
}

func copyEmbeddedFile(dir, src, dst string, overwrite bool) (created, skipped []string) {
	return copyEmbeddedFileMode(dir, src, dst, overwrite, 0o644)
}

// copyEmbeddedExecutable is copyEmbeddedFile with the executable bit set — used
// for the hook/statusline wrappers Claude Code invokes.
func copyEmbeddedExecutable(dir, src, dst string, overwrite bool) (created, skipped []string) {
	return copyEmbeddedFileMode(dir, src, dst, overwrite, 0o755)
}

func copyEmbeddedFileMode(dir, src, dst string, overwrite bool, mode os.FileMode) (created, skipped []string) {
	destPath := filepath.Join(dir, dst)
	if !overwrite && fileExists(destPath) {
		return nil, []string{dst}
	}

	data, err := embeddedFS.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot read embedded %s: %v\n", src, err)
		return nil, nil
	}

	// Ensure parent directory exists
	if parent := filepath.Dir(destPath); parent != dir {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cannot create directory %s: %v\n", parent, err)
			return nil, nil
		}
	}

	if err := os.WriteFile(destPath, data, mode); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot write %s: %v\n", dst, err)
		return nil, nil
	}
	// WriteFile only applies mode on create; ensure the bit sticks on overwrite.
	if mode&0o111 != 0 {
		_ = os.Chmod(destPath, mode)
	}

	return []string{dst}, nil
}

// copyFrameworkNotes copies every framework note from srcFS into <dir>/<destPrefix>/,
// preserving the tree shape, skipping dotfiles (e.g. .gitkeep), and never
// overwriting an existing file (skip-if-exists, like CLAUDE.md). The set of
// files it copies mirrors exactly what manifest.NotesFromFS(srcFS, destPrefix)
// registers, so every manifest note record has a backing file on disk. Returned
// paths are workspace-relative (forward-slash). It is a no-op while the
// framework ships no notes.
func copyFrameworkNotes(srcFS fs.FS, dir, destPrefix string) (created, skipped []string, err error) {
	walkErr := fs.WalkDir(srcFS, ".", func(p string, d fs.DirEntry, we error) error {
		if we != nil {
			return we
		}
		if d.IsDir() {
			// Prune hidden directories so nested files aren't copied — mirrors
			// manifest.NotesFromFS so copy and registration stay consistent.
			if p != "." && strings.HasPrefix(filepath.Base(p), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(filepath.Base(p), ".") {
			return nil // dotfile (e.g. .gitkeep)
		}
		// p is a forward-slash fs path relative to srcFS root; keep the manifest
		// key portable (forward-slash) to match manifest.NotesFromFS exactly.
		rel := destPrefix + "/" + p // "notes/<rel>"
		destPath := filepath.Join(dir, filepath.FromSlash(rel))
		if fileExists(destPath) {
			skipped = append(skipped, rel)
			return nil
		}
		data, rerr := fs.ReadFile(srcFS, p)
		if rerr != nil {
			return rerr
		}
		if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o755); mkErr != nil {
			return mkErr
		}
		if wErr := os.WriteFile(destPath, data, 0o644); wErr != nil {
			return wErr
		}
		created = append(created, rel)
		return nil
	})
	return created, skipped, walkErr
}

func writeFileIfNotExists(dir, dst, content string) (created, skipped []string) {
	destPath := filepath.Join(dir, dst)
	if fileExists(destPath) {
		return nil, []string{dst}
	}

	if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot write %s: %v\n", dst, err)
		return nil, nil
	}

	return []string{dst}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Index file contents
const indexProjectsTODO = `# Projects & Todo List

## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
`

const indexAreas = `# Areas Index

Areas are domains of knowledge and responsibility. They serve as reference hubs — the single source of truth for a domain's systems, contacts, constraints, and current state.

Each area carries free-form **tags** describing what kind of domain it covers (e.g. ` + "`microservice`" + `, ` + "`practice`" + `, ` + "`compliance`" + `). Tags are multi-valued; taxonomy emerges from use.

## Areas

| Area | Slug | Tags | Description |
|------|------|------|-------------|
`

const indexIdeas = `# Ideas Backlog

Ideas captured here. When an idea reaches **ready**, create a project in ` + "`projects/`" + ` using ` + "`templates/project/`" + `.

## Index

| # | Idea | Status | Areas | Tags | One-liner |
|---|------|--------|-------|------|-----------|

<!-- Statuses: raw, exploring, ready, parked, rejected -->
`

const indexNotes = `# Notes Index

Session notes, findings, and reference material organized by category.

## Tool Gotchas

## Workflow

- [CLAUDE.md Hygiene — Extract vs Inline](claude-md-hygiene.md) — before editing CLAUDE.md, decide whether a rule belongs inline (every session) or extracted to a note (trigger-only).
- [Idea → Project — Lifecycle & Status](idea-to-project-workflow.md) — the lifecycle from capturing an idea to turning it into a project (or parking/rejecting it), plus the status vocabulary.

## Architecture

## Process

- [Project Structure — File Conventions & Workflow](project-structure.md) — the standard project files, the rules for working with them, the new-project workflow, and the per-session update checklist.
- [Information Propagation — Procedure & Lookup](information-propagation.md) — when new information surfaces, the step-by-step procedure for updating affected areas/projects/ideas/notes, plus the what-to-update lookup table.
- [Session-End Capture — Notes & Insights Procedures](session-end-capture.md) — the step-by-step procedures for capturing notes (category + tags + INDEX) and insight drafts at session end, plus the duplicate-check.
- [After Creating a PR — Review-Resolution Loop](after-pr-checks.md) — after opening a PR, the fetch probes, the per-comment fix/reply/escalate taxonomy, CI-failure handling, and the guardrails for driving review to green.
- [Apply Uncontroversial Review Findings Without Asking](apply-uncontroversial-review-findings.md) — the per-finding validation checklist, when to apply silently vs. push back, and the rule that a target repo's own review config overrides a principled "out of scope" rejection.
- [Debugging — Evidence Contract Before Fixing](debugging-evidence-contract.md) — for a non-trivial bug, state reproduction-or-trace + hypothesis-with-evidence + one-alternative-ruled-out before proposing a fix; the bug-diagnosis subset of Evidence Over Inference.
- [Bulk / Destructive Ops — Pre-flight](bulk-ops-preflight.md) — before a command that touches many entities, state target set + exclusion set + single-sample dry-run + quoting/arity check and wait for approval.
- [Continuous Improvement — What to Look For, How to Suggest, Where to Capture](continuous-improvement.md) — the what-to-look-for catalogue for improving the workspace's own rules/structure/workflows, plus the in-session vs. session-end sequencing.
- [Capturing Improvement Proposals](capturing-improvement-proposals.md) — how to persist a harness improvement proposal with a lifecycle + triage surface (meta-framework idea file + BACKLOG row).
- [Reviewing Insights Workflow](reviewing-insights-workflow.md) — the promote / reject / defer mechanics for draft insights; draft insights are never active rules until promoted.
- [Selecting the Next Project — Procedure + Active-Work Detection](selecting-next-project.md) — the ranked selection procedure (TODO order, skip Done/On-hold/active) and the cg active detector flags/windows.
- [Promoting a Project to an Area](promoting-a-project-to-an-area.md) — when and how to promote a project that outgrew single-project tracking into a persistent area, re-bounding remaining work.
- [Area Rules — Linking, Reference Hubs & External-Repo Lookup](area-rules.md) — the full area rules (multi-area items, creating an area, the freshness check), the project-linking rule, and the external-repo → area lookup.

## Research

## Reference

- [Tooling Preferences — MCPs and Custom Code](tooling-preferences-mcps.md) — the preference order for external-system integration (official MCP → custom code you control → third-party MCP), with the gap-handling rule and reasoning.

## Troubleshooting
`

const indexInsights = `# Insights

Draft observations about user work style, prompting patterns, and collaboration preferences.
Drafts are **pending review** — Claude MUST NOT apply draft insights to its behavior.

**Workflow:** User reviews this table periodically and changes status to ` + "`promoted`" + ` or ` + "`rejected`" + `.
Promoted insights get their suggested rule added to CLAUDE.md.

| Insight | Status | Date | File |
|---|---|---|---|
`
