---
name: cg-init
description: >-
  Bootstrap a new Consigliere workspace in the current directory. Creates the folder structure,
  templates, index files, .cg.json, CLAUDE.md, and PROFILE.md. Use when the user wants
  to set up a new personal workspace or knowledge base, or says "init cg", "create workspace",
  "set up consigliere", or "init consigliere".
user-invocable: true
allowed-tools: Read, Write, Bash, Glob, Grep, Edit
argument-hint: "[--force]"
---

# /cg-init — Workspace Bootstrapper

Set up a new Consigliere workspace in the current working directory.

## Step 1: Guard — Check for existing workspace

Read `.cg.json` in the current directory.

- If it exists and `$ARGUMENTS` does NOT contain `--force`: report "This directory is already a Consigliere workspace (version {version}). Use `/cg-init --force` to re-initialize." and stop.
- If it exists and `$ARGUMENTS` contains `--force`: proceed, but **do not overwrite CLAUDE.md or PROFILE.md** if they exist (the user may have customized them). Only overwrite `.cg.json` and templates.
- If it does not exist: proceed normally.

## Step 2: Locate plugin templates

The templates bundled with this plugin are located at `../../templates/` relative to this SKILL.md file. Use Glob to find the exact path:

```
**/consigliere/templates/idea.md
```

Once you find one template file, derive the base template directory from its path. All templates are under that directory:
- `{base}/idea.md`
- `{base}/note.md`
- `{base}/insight.md`
- `{base}/area.md`
- `{base}/subagent-briefing.md`
- `{base}/project/README.md`
- `{base}/project/decisions.md`
- `{base}/project/todo.md`
- `{base}/project/log.md`
- `{base}/project/references.md`
- `{base}/workspace/CLAUDE.md`
- `{base}/workspace/PROFILE.md`
- `{base}/workspace/.cg.json`
- `{base}/workspace/.gitignore`

## Step 3: Create directory structure

Create these directories if they don't already exist:

```
projects/
areas/
ideas/
notes/
insights/
templates/
templates/project/
```

Track which directories were created vs. already existed.

## Step 4: Copy content templates

Read each content template from the plugin's template directory and write it to the workspace's `templates/` directory:

| Source (plugin) | Destination (workspace) |
|---|---|
| `idea.md` | `templates/idea.md` |
| `note.md` | `templates/note.md` |
| `insight.md` | `templates/insight.md` |
| `area.md` | `templates/area.md` |
| `subagent-briefing.md` | `templates/subagent-briefing.md` |
| `project/README.md` | `templates/project/README.md` |
| `project/decisions.md` | `templates/project/decisions.md` |
| `project/todo.md` | `templates/project/todo.md` |
| `project/log.md` | `templates/project/log.md` |
| `project/references.md` | `templates/project/references.md` |

If the destination file already exists, **skip it** (do not overwrite user customizations).

## Step 5: Create index files

Create each index file only if it does not already exist.

### projects/TODO.md
```markdown
# Projects & Todo List

## Active Projects

| # | Project | Status | Areas | Folder |
|---|---------|--------|-------|--------|
```

### areas/INDEX.md
```markdown
# Areas Index

Areas are domains of knowledge and responsibility. They serve as reference hubs — the single source of truth for a domain's systems, contacts, constraints, and current state.

## Service/System Areas

| Area | Slug | Description |
|------|------|-------------|

## Practice/Platform Areas

| Area | Slug | Description |
|------|------|-------------|
```

### ideas/BACKLOG.md
```markdown
# Ideas Backlog

Ideas captured here. When an idea reaches **ready**, create a project in `projects/` using `templates/project/`.

## Index

| # | Idea | Status | Areas | Tags | One-liner |
|---|------|--------|-------|------|-----------|

<!-- Statuses: raw, exploring, ready, parked, rejected -->
```

### notes/INDEX.md
```markdown
# Notes Index

Session notes, findings, and reference material organized by category.

## Tool Gotchas

## Workflow

## Architecture

## Process

## Research

## Reference

## Troubleshooting
```

### insights/DRAFTS.md
```markdown
# Insights

Draft observations about user work style, prompting patterns, and collaboration preferences.
Drafts are **pending review** — Claude MUST NOT apply draft insights to its behavior.

**Workflow:** User reviews this table periodically and changes status to `promoted` or `rejected`.
Promoted insights get their suggested rule added to CLAUDE.md.

| Insight | Status | Date | File |
|---|---|---|---|
```

## Step 6: Create workspace metadata

### .cg.json

Read the `.cg.json` template from the plugin's `workspace/` directory and write it to the workspace root. This records the Consigliere version used during initialization.

### CLAUDE.md

If CLAUDE.md does NOT exist in the workspace root:
- Read the CLAUDE.md template from the plugin's `workspace/` directory
- Write it to the workspace root

If CLAUDE.md already exists:
- Do NOT overwrite it
- Instead, note in the summary that the user should manually merge framework sections if desired
- Write the template to `CLAUDE.cg-template.md` as a reference

### PROFILE.md

If PROFILE.md does NOT exist:
- Read the PROFILE.md template from the plugin's `workspace/` directory
- Write it to the workspace root

If PROFILE.md already exists:
- Skip it

### .gitignore

If .gitignore does NOT exist:
- Read the .gitignore template from the plugin's `workspace/` directory
- Write it to the workspace root

If .gitignore already exists:
- Skip it

## Step 7: Summary

Print a clear summary report:

```
## Consigliere workspace initialized

**Version:** 1.0.0

### Created
- [list of directories and files that were created]

### Skipped (already existed)
- [list of directories and files that were skipped]

### Next steps
1. Edit `PROFILE.md` with your role, responsibilities, and context
2. Edit the `Purpose` and `Area Categories` sections in `CLAUDE.md`
3. Define your first area in `areas/` using `templates/area.md`
4. Run `git init` if this is not yet a git repository
5. Commit the initial workspace structure
```

If CLAUDE.md was skipped, add:
```
> **Note:** CLAUDE.md already existed and was not overwritten. A reference copy of the Consigliere
> framework template has been saved to `CLAUDE.cg-template.md`. You may want to merge
> framework sections into your existing CLAUDE.md.
```
