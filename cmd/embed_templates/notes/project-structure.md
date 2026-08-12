# Project Structure — File Conventions & Workflow

## Meta

- **Category:** process
- **Tags:** `project-structure`, `knowledge-base-structure`, `workflow`
- **Framework note:** shipped by Consigliere and loaded on demand when creating a project or working with project files. Safe to edit for your workspace.

## Summary

Each project lives in its own folder under `projects/`, named for the project slug, and contains four standard files. This note holds the full mechanics: the file-purpose tables, the rules for working with project files, the new-project workflow, and the per-session update checklist. The one-line overview and the two load-bearing rules (read `README.md` first; keep files current) stay inline in `CLAUDE.md`.

## Standard Files

Every project folder contains these files:

| File | Purpose | Template |
|------|---------|----------|
| `README.md` | **Main project file.** Current state, goals, scope, stakeholders, dependencies. This is the authoritative source of truth for the project. Must stay concise — no historical baggage. | `templates/project/README.md` |
| `decisions.md` | **Decisions log.** Append-only. Each decision has a status (`active`, `superseded`, `reversed`) to prevent AI tools from misinterpreting old decisions. | `templates/project/decisions.md` |
| `todo.md` | **Action items.** Checkbox list of what needs doing. "What's next" when picking up a project. | `templates/project/todo.md` |
| `log.md` | **Activity log.** Chronological record of what happened — session summaries, findings, trial results, meeting notes. Newest first. | `templates/project/log.md` |

## Optional Files

| File | When to use | Template |
|------|-------------|----------|
| `references.md` | When a project accumulates external links (Slack threads, Confluence pages, repos, tickets). | `templates/project/references.md` |
| `resume.md` | **Transient — written by `/wrap pause`, not authored by hand.** Captures the in-flight cursor (active todo item, mental context, dirty state, next concrete action) so a mid-work session can be picked up later. The Session-Start Rule reads it, surfaces it, and **deletes it** once the user confirms work has resumed; a paused project should therefore have at most one, and a project that is not paused should have none. Durable content belongs in `log.md` or `decisions.md`, not here. | `templates/project/resume.md` |
| Any other file/folder | Project-specific content (e.g., `trials/`, `candidates.md`, `adr-draft.md`). Freeform — no template needed. | — |

## Rules for Working with Project Files

1. **Always read `README.md` first** when starting work on a project. It has the current state.
2. **Keep `README.md` concise.** It should answer "what is this project and where does it stand" — nothing more. No decision history, no session logs, no link collections.
3. **Record every decision in `decisions.md`**, not in README.md. Use the structured format with status. When a decision is superseded, update the old entry's status to `superseded` and add a `Superseded by: DEC-XXX` line — do not delete old decisions.
4. **Update `todo.md`** whenever new actions are identified or completed. This is the first place to look for "what's next" on a project.
5. **Log session activity in `log.md`** at the end of any session that produced meaningful progress, findings, or outcomes for the project. Keep entries brief — bullet points, not essays.
6. **Move external links to `references.md`** rather than accumulating them in README.md. Create the file on first use.
7. **Project-specific files** can be added freely. Reference them from README.md with relative links.

## Creating a New Project

1. Create the folder: `projects/{slug}/`
2. Copy the 4 standard templates (`README.md`, `decisions.md`, `todo.md`, `log.md`) from `templates/project/` into the folder
3. Fill in README.md with project details
4. Add the project to `projects/TODO.md`
5. Create an initial entry in `log.md` recording when and why the project was started

## Keeping Project Files Up to Date

Claude MUST keep project files current during every session that touches a project:

- **README.md**: Update status, scope, stakeholders, or dependencies if any changed during the session.
- **decisions.md**: Append any decisions made. Mark superseded decisions.
- **todo.md**: Check off completed items. Add new items discovered during the session.
- **log.md**: Add a dated entry summarizing what was done or learned.
- **references.md**: Add any new external links encountered.

**This is not optional.** Stale project files are worse than no files — they mislead future sessions.
