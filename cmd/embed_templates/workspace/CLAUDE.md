# CLAUDE.md

<!-- cg:section:start=intro -->
This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
This is a **Consigliere workspace**. Sections marked with `cg:section` comments are maintained by the [Consigliere](https://github.com/mnemcik/consigliere) framework and can be updated automatically. Sections marked with `user:section` are yours to customize freely.
<!-- cg:section:end=intro -->

<!-- user:section:start=purpose -->
## Purpose

[Describe what this workspace is for. What domains does it cover? What kind of work does it track?]
<!-- user:section:end=purpose -->

<!-- user:section:start=owner -->
## Owner

See [PROFILE.md](PROFILE.md) for role, responsibilities, and context that should inform how ideas and work are interpreted.
<!-- user:section:end=owner -->

<!-- cg:section:start=memory-policy -->
## Memory Policy

**Do NOT use Claude Code's auto-memory system** (`~/.claude/projects/.../memory/`). It is not transparent (hidden in dotfiles) and not portable across AI tools.

Instead, persist all learnings, preferences, references, and feedback **in this repository**:
- Findings and gotchas → `notes/` (see Session-End Rule below)
- User profile and context → `PROFILE.md`
- Conventions and rules → this file (`CLAUDE.md`)

If something is worth remembering, it should be committed to the repo where any tool can read it.
<!-- cg:section:end=memory-policy -->

<!-- cg:section:start=claude-md-hygiene -->
## Editing CLAUDE.md — Hygiene Trigger

**Before adding or changing anything in this file, load [`notes/claude-md-hygiene.md`](notes/claude-md-hygiene.md) and apply the extract-vs-inline decision.** CLAUDE.md is loaded into context for *every* session, so every line costs tokens on sessions that never need the rule. The test: **does this apply to every session, or only when a specific trigger appears?** Every-session rules stay inline; trigger-only rules extract to a `notes/<topic>.md` file with a ≤3-sentence pointer here (trigger + one-line headline + note path). Treat additions as a last resort, not a first instinct.
<!-- cg:section:end=claude-md-hygiene -->

<!-- cg:section:start=session-start -->
## Session-Start Rule: Identify Project and Area

**This is a mandatory gate. No work may begin until this rule is satisfied.**

Every session that involves meaningful work (code changes, research, analysis, document creation) MUST be associated with a **project** and at least one **area**. The only exceptions are when the user explicitly says otherwise (e.g., "just a quick question", "no project needed", "skip the project").

### Procedure

1. **Identify the area.** From the user's request, determine which area(s) from `areas/INDEX.md` the work relates to. If the request mentions a repository, tool, service, or domain, match it to an area.
2. **Read the area file.** Before doing anything else, read the matched area file(s) to load context, constraints, and repo conventions.
3. **Identify the project.** Check `projects/TODO.md` for an existing project that covers this work. If no project exists, create one using the standard project creation workflow (see "Creating a New Project" below).
4. **Read the project files.** Read the project's `README.md` and `todo.md` to understand current state and pending work.
5. **Proceed with work.** Only after steps 1–4 are complete.

### When identification fails

If the area or project cannot be determined from the user's request:
- **Do NOT guess or proceed without them.**
- **Stop and ask the user** which area and/or project this work belongs to, or whether they want a new project created.
- Suggest the closest matching area(s) and project(s) if possible, to make it easy for the user to confirm.

### What counts as "explicitly skipped"

The user must clearly indicate that project tracking is not needed. Examples:
- "no project for this"
- "just a quick question"
- "skip the project stuff"
- Idea capture (`idea: ...`) — these follow the Idea Capture workflow instead

Ambiguous requests (e.g., "fix this bug", "update the docs") are NOT exempt — they need a project.
<!-- cg:section:end=session-start -->

<!-- cg:section:start=idea-capture -->
## Idea Capture

When the user submits a new idea (e.g., "idea: ..."), interpret what it refers to, classify it with appropriate tags, and store it immediately using the workflow below. Use the owner's profile and domain context to fill in the What/Why/Notes sections. Only ask for clarification if the idea is genuinely ambiguous.
<!-- cg:section:end=idea-capture -->

<!-- cg:section:start=structure -->
## Structure

- `PROFILE.md` — Owner's role, responsibilities, and context for interpreting work
- `areas/` — Domains of knowledge and responsibility. Reference hubs for systems, services, practices, and platforms. Index: `areas/INDEX.md`
- `ideas/` — Idea backlog. Lightweight captures of ideas before they become projects. Index: `ideas/BACKLOG.md`
- `projects/` — Active and completed projects. Each project is a folder (see Project Structure below). Index: `projects/TODO.md`
- `templates/` — Templates for ideas, projects, notes, insights, and areas (`idea.md`, `project/`, `note.md`, `insight.md`, `area.md`)
- `notes/` — Session notes, findings, and reference material. Index: `notes/INDEX.md`
- `insights/` — Draft observations about user work style and preferences. Index: `insights/DRAFTS.md`. **Drafts are NOT active rules — do not apply them until promoted.**
<!-- cg:section:end=structure -->

<!-- cg:section:start=areas -->
## Areas

Areas are domains of knowledge and responsibility. They serve as **reference hubs** — the single source of truth for a domain's systems, contacts, constraints, and current state. Projects, ideas, and notes link to areas instead of duplicating context.
<!-- cg:section:end=areas -->

<!-- user:section:start=area-tags -->
### Area Tags

Areas use **free-form tags** to describe what kind of domain they cover. Tags are multi-valued, no fixed enum — the taxonomy emerges from use. List the tags currently in play here so new areas can reuse existing vocabulary instead of inventing near-duplicates.

**Currently in use:**

- `example-tag` — short description of what this tag means

Add new tags freely. Run `scripts/area-tags.sh` (if present) to list all tags currently used across `areas/*.md` with counts.
<!-- user:section:end=area-tags -->

<!-- cg:section:start=area-rules -->
### Area Rules

1. **Every project, idea, and note MUST have an `Areas:` field** linking to one or more areas. Use the area slug(s).
2. **Areas are reference hubs, not duplicators.** When a project needs context about a system (contacts, constraints, architecture), link to the area file instead of writing it again. If the context doesn't exist in the area yet, add it there first, then reference it.
3. **Items can belong to multiple areas.** Use the primary area first, then secondary areas.
4. **When creating a new area,** use `templates/area.md`, add it to `areas/INDEX.md`, and add any new tags (with short descriptions) to the Area Tags section in CLAUDE.md.
5. **When reading an area for a project,** check the `Last reviewed` date. If it's older than 2 weeks, verify the content is still accurate before relying on it.

### Linking to Areas from Projects

Every project MUST link to at least one area. When starting work on a project, **read its associated area file(s) first** to understand the current context, constraints, and contacts. This is enforced by the Session-Start Rule above.

### External Repo → Area Lookup

When the user asks to work on a repository outside this knowledge base (e.g., a tool or service), **check `areas/INDEX.md` for a matching area before starting work.** If an area exists, read it — it contains repo conventions (branch naming, PR title format, CI rules), architecture constraints, and context that must be followed. If no area exists, create one before proceeding. This applies whether the user names the repo explicitly or describes the tool/service by function.
<!-- cg:section:end=area-rules -->

<!-- cg:section:start=project-structure -->
## Project Structure

Each project lives in its own folder under `projects/`, named for the project slug (e.g., `projects/my-project/`). Every folder has four standard files — `README.md` (current state; authoritative), `decisions.md` (append-only decisions log), `todo.md` (next actions), `log.md` (chronological activity, newest first). Optional: `references.md` (external links) and freeform project-specific files.

**Always read `README.md` first** when starting work on a project — it holds the current state. **Keep every project file current** during any session that touches the project; stale project files mislead future sessions.

For the file-purpose tables, the rules for working with project files, the Creating-a-New-Project workflow, and the per-session update checklist, load [`notes/project-structure.md`](notes/project-structure.md).
<!-- cg:section:end=project-structure -->

<!-- cg:section:start=information-propagation -->
## Information Propagation Rule

After any session where new information surfaces (a decision made, a contact's role change, a system status change, new architecture details, a project scope change, meeting notes), Claude MUST check whether existing tracked items — areas, projects, ideas, notes — need updating. **This is not optional**; stale information across tracked items leads to duplicated or contradictory context across sessions.

For the trigger list, the step-by-step propagation procedure, and the new-information-type → what-to-update lookup table, load [`notes/information-propagation.md`](notes/information-propagation.md).
<!-- cg:section:end=information-propagation -->

<!-- cg:section:start=idea-workflow -->
## Workflow: Idea → Project

When an idea is captured, explored, turned into a project, parked, or rejected, load [`notes/idea-to-project-workflow.md`](notes/idea-to-project-workflow.md) for the lifecycle steps and status vocabulary (`raw → exploring → ready → parked | rejected`).
<!-- cg:section:end=idea-workflow -->

<!-- cg:section:start=session-end -->
## Session-End Rule: Capture Notes and Insights

Before ending any session, Claude MUST capture two distinct outputs: **notes** (topic/resource findings — gotchas, patterns, technical learnings) in `notes/`, and **insights** (user-work-style observations — prompting patterns, preferences) as drafts in `insights/YYYY-MM-DD/`. Skip entirely if the session was purely mechanical.

**CRITICAL: Insights are always created as drafts. Claude MUST NOT apply, follow, or reference draft insights in its behavior.** They only become active rules when the user reviews them and promotes them to CLAUDE.md.

For the notes-capture procedure (category + tags + INDEX), the insights-draft procedure, and the duplicate-check on `insights/DRAFTS.md`, load [`notes/session-end-capture.md`](notes/session-end-capture.md).
<!-- cg:section:end=session-end -->

<!-- user:section:start=git-workflow -->
## Git Workflow

[Define your git workflow here. For example:]

**This repo:** commit directly to `main`. This is a personal knowledge base — no branches needed.
<!-- user:section:end=git-workflow -->

<!-- cg:section:start=pr-review-loop -->
### Pull Request Review Loop

**Every time Claude opens a PR (in any repo), Claude MUST enter a review-resolution loop before considering the work done.** The PR is not "shipped" at `gh pr create`; it is shipped when CI is green and every open review comment has been either fixed in a follow-up commit or answered with a reasoning reply — never silently dismissed. Bot reviews (CodeRabbit, Copilot, dependabot) count the same as human reviews.

For the fetch probes, the per-comment outcome taxonomy (fix / reply / escalate), the CI-failure handling, the guardrails (no amend/force-push on a review branch), and the session-end `/schedule` handoff, load [`notes/after-pr-checks.md`](notes/after-pr-checks.md).
<!-- cg:section:end=pr-review-loop -->

<!-- cg:section:start=conventions -->
## Conventions

- Idea statuses: `raw` → `exploring` → `ready` → `parked` | `rejected`
- Project statuses: `defining` → `in-progress` → `done` | `on-hold`
- Project priorities: `high` | `medium` | `low` — stored in each project's README.md Meta section and in `projects/TODO.md` (sorted by priority)
- Area tags: free-form, multi-valued. The current tag vocabulary is listed in the **Area Tags** section above.
- Tags on ideas are free-form. Use them to group and filter.
- Areas on all items are mandatory. Use area slugs from `areas/INDEX.md`.
- Not everything becomes a ticket — projects may produce tools, docs, automation, or just notes.
- **Pull request URLs:** Always provide full URLs (e.g., `https://github.com/org/repo/pull/123`) when referencing pull requests, not shorthand like `org/repo#123`. The user needs clickable links to navigate directly.
<!-- cg:section:end=conventions -->

<!-- user:section:start=custom-conventions -->
## Custom Conventions

[Add your own workspace-specific conventions here.]
<!-- user:section:end=custom-conventions -->
