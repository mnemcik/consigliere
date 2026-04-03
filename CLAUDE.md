# CLAUDE.md

This file provides guidance to Claude Code when working on the Second Brain (2b) plugin itself.

## What This Repo Is

A Claude Code plugin that provides a personal workspace management framework. It is a content-only repository — markdown and JSON files, no build system or runtime.

## Repository Structure

```
.claude-plugin/plugin.json        # Plugin manifest
skills/                           # Slash command definitions
  match-project/SKILL.md          # /match-project — project matcher (forked subagent)
  init/SKILL.md                   # /2b-init — workspace bootstrapper
templates/                        # Bundled templates
  workspace/                      # Full workspace scaffolding (CLAUDE.md, PROFILE.md, etc.)
  project/                        # Project folder templates
  idea.md, note.md, etc.          # Content item templates
```

## Key Conventions

- Plugin follows Claude Code plugin structure (`.claude-plugin/plugin.json` at root)
- Skills use YAML frontmatter in SKILL.md files
- Templates in `templates/workspace/CLAUDE.md` use sentinel comments (`<!-- 2b:section:start=X -->`) to delimit framework vs user sections
- Version in `.claude-plugin/plugin.json` must match version in `templates/workspace/.2b.json`
- Content templates are generic — no user-specific content

## Adding a New Skill

1. Create `skills/{skill-name}/SKILL.md` with YAML frontmatter
2. Include a guard clause that checks for `.2b.json` at workspace root
3. Update `README.md` with the new skill's documentation
4. Bump version in `.claude-plugin/plugin.json` and `templates/workspace/.2b.json`

## Release Process

1. Bump version in `.claude-plugin/plugin.json` and `templates/workspace/.2b.json`
2. Commit with message: `release: vX.Y.Z — {summary}`
3. Tag: `git tag vX.Y.Z`
4. Push: `git push origin main --tags`
