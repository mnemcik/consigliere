package cmd

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/mnemcik/consigliere/internal/wizard"
	"github.com/mnemcik/consigliere/internal/workspace"
)

// validSlug matches a canonical area slug: lowercase letters/digits separated
// by single dashes. Used as a defense-in-depth check before writing files
// whose path is derived from the slug.
var validSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

//go:embed all:embed_templates
var embeddedFS embed.FS

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
		"projects",
		"areas",
		"ideas",
		"notes",
		"insights",
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
	}
	for src, dst := range contentTemplates {
		c, s := copyEmbeddedFile(dir, src, dst, false)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// Create index files
	indexFiles := map[string]string{
		filepath.Join("projects", "TODO.md"):   indexProjectsTODO,
		filepath.Join("areas", "INDEX.md"):     indexAreas,
		filepath.Join("ideas", "BACKLOG.md"):   indexIdeas,
		filepath.Join("notes", "INDEX.md"):     indexNotes,
		filepath.Join("insights", "DRAFTS.md"): indexInsights,
	}
	for dst, content := range indexFiles {
		c, s := writeFileIfNotExists(dir, dst, content)
		created = append(created, c...)
		skipped = append(skipped, s...)
	}

	// Create .cg.json
	cgJSON := workspace.Config{
		Type:    "consigliere",
		Version: Version,
		Indexes: map[string]string{
			"projects": "projects/TODO.md",
			"areas":    "areas/INDEX.md",
			"ideas":    "ideas/BACKLOG.md",
			"notes":    "notes/INDEX.md",
			"insights": "insights/DRAFTS.md",
		},
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
	claudeSrc := "embed_templates/workspace/CLAUDE.md"
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
		}
		for src, dst := range commands {
			c, s := copyEmbeddedFile(dir, src, dst, forceInit)
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
		if existing, err := os.ReadFile(indexPath); err == nil { //nolint:gosec // indexPath is a fixed name under dir
			updated := wizard.InsertAreaIndexRow(string(existing), &answers)
			if updated != string(existing) {
				if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil { //nolint:gosec // indexPath is a fixed name under dir
					return fmt.Errorf("updating areas/INDEX.md: %w", err)
				}
				created = append(created, "areas/INDEX.md (row added)")
			}
		}
	}

	if wizardInit && answers.RunGitInit {
		if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
			gitCmd := exec.CommandContext(cmd.Context(), "git", "init")
			gitCmd.Dir = dir
			if out, err := gitCmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: git init failed: %v\n%s\n", err, out)
			} else {
				created = append(created, ".git/ (git init)")
			}
		} else {
			skipped = append(skipped, ".git/ (already a git repo)")
		}
	}

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
		fmt.Println("2. Edit the `Purpose` and `Area Categories` sections in `CLAUDE.md`")
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
		fmt.Println("2. Edit the `Purpose` and `Area Categories` sections in `CLAUDE.md`")
		fmt.Println("3. Define your first area in `areas/` using `templates/area.md`")
		fmt.Println("4. Run `git init` if this is not yet a git repository")
		fmt.Println("5. Commit the initial workspace structure")
		fmt.Println()
		fmt.Println("Tip: re-run with `cg init --wizard` for an interactive walkthrough.")
	}

	return nil
}

func copyEmbeddedFile(dir, src, dst string, overwrite bool) (created, skipped []string) {
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

	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot write %s: %v\n", dst, err)
		return nil, nil
	}

	return []string{dst}, nil
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

## Service/System Areas

| Area | Slug | Description |
|------|------|-------------|

## Practice/Platform Areas

| Area | Slug | Description |
|------|------|-------------|
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

## Architecture

## Process

## Research

## Reference

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
