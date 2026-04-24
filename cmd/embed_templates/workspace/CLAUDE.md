# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
This is a **Consigliere workspace**. Sections marked with `cg:section` comments are maintained by the [Consigliere](https://github.com/mnemcik/consigliere) framework and can be updated automatically. Sections marked with `user:section` are yours to customize freely.

<!-- cg:version=1.0.0 -->

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

<!-- user:section:start=area-categories -->
### Area Categories

Define your area categories here. Areas typically fall into two types:

**Service/System Areas** — specific services, APIs, infrastructure components, or products:
- `example-slug` — Example Service Name

**Practice/Platform Areas** — processes, tools, and organizational practices:
- `example-slug` — Example Practice Name
<!-- user:section:end=area-categories -->

<!-- cg:section:start=area-rules -->
### Area Rules

1. **Every project, idea, and note MUST have an `Areas:` field** linking to one or more areas. Use the area slug(s).
2. **Areas are reference hubs, not duplicators.** When a project needs context about a system (contacts, constraints, architecture), link to the area file instead of writing it again. If the context doesn't exist in the area yet, add it there first, then reference it.
3. **Items can belong to multiple areas.** Use the primary area first, then secondary areas.
4. **When creating a new area,** use `templates/area.md`, add it to `areas/INDEX.md` under the correct category, and update the Area Categories section in CLAUDE.md.
5. **When reading an area for a project,** check the `Last reviewed` date. If it's older than 2 weeks, verify the content is still accurate before relying on it.

### Linking to Areas from Projects

Every project MUST link to at least one area. When starting work on a project, **read its associated area file(s) first** to understand the current context, constraints, and contacts. This is enforced by the Session-Start Rule above.

### External Repo → Area Lookup

When the user asks to work on a repository outside this knowledge base (e.g., a tool or service), **check `areas/INDEX.md` for a matching area before starting work.** If an area exists, read it — it contains repo conventions (branch naming, PR title format, CI rules), architecture constraints, and context that must be followed. If no area exists, create one before proceeding. This applies whether the user names the repo explicitly or describes the tool/service by function.
<!-- cg:section:end=area-rules -->

<!-- cg:section:start=project-structure -->
## Project Structure

Each project lives in its own folder under `projects/`. The folder name is the project slug (e.g., `projects/my-project/`).

### Standard Files

Every project folder contains these files:

| File | Purpose | Template |
|------|---------|----------|
| `README.md` | **Main project file.** Current state, goals, scope, stakeholders, dependencies. This is the authoritative source of truth for the project. Must stay concise — no historical baggage. | `templates/project/README.md` |
| `decisions.md` | **Decisions log.** Append-only. Each decision has a status (`active`, `superseded`, `reversed`) to prevent AI tools from misinterpreting old decisions. | `templates/project/decisions.md` |
| `todo.md` | **Action items.** Checkbox list of what needs doing. "What's next" when picking up a project. | `templates/project/todo.md` |
| `log.md` | **Activity log.** Chronological record of what happened — session summaries, findings, trial results, meeting notes. Newest first. | `templates/project/log.md` |

### Optional Files

| File | When to use | Template |
|------|-------------|----------|
| `references.md` | When a project accumulates external links (Slack threads, Confluence pages, repos, tickets). | `templates/project/references.md` |
| Any other file/folder | Project-specific content (e.g., `trials/`, `candidates.md`, `adr-draft.md`). Freeform — no template needed. | — |

### Rules for Working with Project Files

1. **Always read `README.md` first** when starting work on a project. It has the current state.
2. **Keep `README.md` concise.** It should answer "what is this project and where does it stand" — nothing more. No decision history, no session logs, no link collections.
3. **Record every decision in `decisions.md`**, not in README.md. Use the structured format with status. When a decision is superseded, update the old entry's status to `superseded` and add a `Superseded by: DEC-XXX` line — do not delete old decisions.
4. **Update `todo.md`** whenever new actions are identified or completed. This is the first place to look for "what's next" on a project.
5. **Log session activity in `log.md`** at the end of any session that produced meaningful progress, findings, or outcomes for the project. Keep entries brief — bullet points, not essays.
6. **Move external links to `references.md`** rather than accumulating them in README.md. Create the file on first use.
7. **Project-specific files** can be added freely. Reference them from README.md with relative links.

### Creating a New Project

1. Create the folder: `projects/{slug}/`
2. Copy the 4 standard templates (`README.md`, `decisions.md`, `todo.md`, `log.md`) from `templates/project/` into the folder
3. Fill in README.md with project details
4. Add the project to `projects/TODO.md`
5. Create an initial entry in `log.md` recording when and why the project was started

### Keeping Project Files Up to Date

Claude MUST keep project files current during every session that touches a project:

- **README.md**: Update status, scope, stakeholders, or dependencies if any changed during the session.
- **decisions.md**: Append any decisions made. Mark superseded decisions.
- **todo.md**: Check off completed items. Add new items discovered during the session.
- **log.md**: Add a dated entry summarizing what was done or learned.
- **references.md**: Add any new external links encountered.

**This is not optional.** Stale project files are worse than no files — they mislead future sessions.
<!-- cg:section:end=project-structure -->

<!-- cg:section:start=information-propagation -->
## Information Propagation Rule

After any session where new information is discussed or identified, Claude MUST check whether existing items need updating. This applies to areas, projects, ideas, and notes.

### When to trigger

- A decision is made or a constraint is discovered
- A contact's role or availability changes
- A system's status changes (e.g., deployed, deprecated, blocked)
- New architecture or technical details surface
- A project's status or scope changes
- A meeting or sync produces information relevant to tracked items

### How to propagate

1. **Identify affected areas** — which area(s) does the new information touch?
2. **Update the area file** — add the new information to the appropriate section (Architecture & Constraints, Current State, Key Contacts, etc.). Update the `Last reviewed` date.
3. **Check associated items** — read the area's Associated Items section. For each linked project/idea/note, check if the new information changes its status, scope, dependencies, or open questions. Update if needed.
4. **Check cross-area impact** — read the area's Related Areas section. If the new information affects a related area, update that too.
5. **Update indexes** — if a project status changed, update `projects/TODO.md`. If an idea status changed, update `ideas/BACKLOG.md`.

### What to look for

| New information type | Check and update |
|---------------------|-----------------|
| Decision made | Area decisions/constraints, project `decisions.md` |
| New contact or role change | Area Key Contacts, project Stakeholders |
| System status change | Area Current State, project Dependencies |
| Scope change | Project Scope, area Overview |
| Blocker or risk | Project `README.md` Dependencies, project `todo.md`, area Architecture & Constraints |
| Meeting notes | All areas mentioned, project `log.md` for session record |

**This is not optional.** Stale information in areas and projects leads to duplicated or contradictory context across sessions.
<!-- cg:section:end=information-propagation -->

<!-- cg:section:start=idea-workflow -->
## Workflow: Idea → Project

1. Capture an idea in `ideas/` using `templates/idea.md`. Add it to `ideas/BACKLOG.md` with status `raw`.
2. When exploring, update status to `exploring` and flesh out the idea file.
3. When the idea is mature enough, mark it `ready` and create a project folder in `projects/` using `templates/project/`. Link the idea file as the origin in README.md.
4. Ideas that won't be pursued get marked `parked` (maybe later) or `rejected` (won't do).
<!-- cg:section:end=idea-workflow -->

<!-- cg:section:start=session-end -->
## Session-End Rule: Capture Notes and Insights

Before ending any session, Claude MUST review the session and capture two distinct types of output. Skip entirely if the session was purely mechanical (e.g., a single file rename) with nothing worth noting.

### A. Notes — Topic/Resource Findings

Findings from working on a specific topic, tool, or resource (gotchas, patterns, technical learnings).

1. Create or update a note in `notes/` using `templates/note.md`
2. Assign a category: `tool-gotchas`, `workflow`, `architecture`, `process`, `research`, `reference`, or `troubleshooting`
3. Add relevant tags for discoverability
4. Update `notes/INDEX.md` under the appropriate category heading
5. Consolidate with existing notes — extend rather than duplicate
6. If a finding is broadly relevant as a convention/constraint, also add it to CLAUDE.md

### B. Insights — User Work Style Observations (Drafts Only)

Observations about how the user prefers to work with Claude — prompting patterns, communication preferences, decision-making style, collaboration expectations.

1. **Check for duplicates first** — Read `insights/DRAFTS.md` and check if the observation is already captured (as `draft`, `promoted`, or `rejected`). If an existing insight covers the same theme, skip or add new evidence to that file instead of creating a new one.
2. Create an insight file in `insights/YYYY-MM-DD/` (today's date subfolder) using `templates/insight.md` with status `draft`
3. Include concrete evidence (paraphrased examples from the session)
4. Propose a suggested rule that could be added to CLAUDE.md if promoted
5. Add a row to the table in `insights/DRAFTS.md` with the insight name, status `draft`, date, and file link

**CRITICAL: Insights are always created as drafts. Claude MUST NOT apply, follow, or reference draft insights in its behavior. They only become active rules when the user reviews them and promotes them to CLAUDE.md.**
<!-- cg:section:end=session-end -->

<!-- user:section:start=git-workflow -->
## Git Workflow

[Define your git workflow here. For example:]

**This repo:** commit directly to `main`. This is a personal knowledge base — no branches needed.
<!-- user:section:end=git-workflow -->

<!-- cg:section:start=pr-review-loop -->
### Pull Request Review Loop

**Every time Claude opens a PR (in any repo), Claude MUST enter a review-resolution loop before considering the work done.** The PR is not "shipped" at `gh pr create`; it is shipped when CI is green and every open review comment has been either fixed or answered with reasoning. Applies to any repo where `gh` is authenticated — this workspace or external.

#### Procedure

Run the loop immediately after `gh pr create`, and again after each push of follow-up commits, until the exit condition is met.

1. **Fetch state** — three probes plus CI:
   - `gh pr checks <N> --repo <org>/<repo>`
   - Inline comments: `gh api repos/<org>/<repo>/pulls/<N>/comments`
   - Issue comments: `gh api repos/<org>/<repo>/issues/<N>/comments`
   - Review decisions: `gh api repos/<org>/<repo>/pulls/<N>/reviews`

2. **For each open comment, decide one of three outcomes:**
   - **Fix** — comment is valid and the fix is mechanical, local, and low-risk (gofmt, typo, missing null-check, misnamed variable, missing CHANGELOG entry, lint violations, obvious bug). Implement in a follow-up commit on the same branch. Never amend — each review round is its own commit so the PR timeline stays readable.
   - **Reply with reasoning** — comment is valid *as an observation* but Claude judges the fix unnecessary, incorrect, or out-of-scope for this PR. Post a reply (`gh api repos/<org>/<repo>/pulls/<N>/comments/<comment-id>/replies` or the web UI equivalent) explaining *why* — cite the relevant constraint, test, decision, or scope rule. Do not silently dismiss.
   - **Escalate to user** — reserved for genuinely ambiguous calls: comment touches project direction, requires domain knowledge Claude lacks, or the fix has significant blast radius (API rename, architecture shift, deleting substantial code). Surface it with the comment, Claude's read, and the tradeoff — then wait.

3. **For each failing CI check** — read the log (`gh run view <run-id> --log`), fix, push, re-check. Treat CI failures the same as valid review comments: fix or escalate, never ignore.

4. **Re-run the fetch step after every push** — new fixes often trigger new comments (CodeRabbit re-reviews on each commit). Loop until:
   - All CI checks are green or explicitly accepted skips, AND
   - Every inline and issue comment is either marked resolved, has a follow-up commit addressing it, or has a reasoning reply from Claude.

5. **If CI or review is still pending at session end** — offer the user a `/schedule` agent to resume the loop in an appropriate window (15–30 min for typical Go CI, longer for slow pipelines, ~60 min if waiting for CodeRabbit on a large diff).

#### Guardrails

- **Never silently resolve a comment.** Either a commit addresses it or a reply explains the reasoning. A PR timeline without replies is a PR where decisions went unrecorded.
- **Never force-push or amend on a review branch.** Each round of review earns its own commit; the timeline is the review trail.
- **Bot comments (CodeRabbit, Copilot, dependabot) follow the same rule as human comments.** Bots are noisy but not dismissible by default — assess validity, fix or reply.
- **If a comment disagrees with an existing decision** (recorded in `decisions.md` or an area file), the reply should cite the decision. Do not reopen settled ground without user direction.
<!-- cg:section:end=pr-review-loop -->

<!-- cg:section:start=conventions -->
## Conventions

- Idea statuses: `raw` → `exploring` → `ready` → `parked` | `rejected`
- Project statuses: `defining` → `in-progress` → `done` | `on-hold`
- Project priorities: `high` | `medium` | `low` — stored in each project's README.md Meta section and in `projects/TODO.md` (sorted by priority)
- Area categories: `Service/System` | `Practice/Platform`
- Tags on ideas are free-form. Use them to group and filter.
- Areas on all items are mandatory. Use area slugs from `areas/INDEX.md`.
- Not everything becomes a ticket — projects may produce tools, docs, automation, or just notes.
- **Pull request URLs:** Always provide full URLs (e.g., `https://github.com/org/repo/pull/123`) when referencing pull requests, not shorthand like `org/repo#123`. The user needs clickable links to navigate directly.
<!-- cg:section:end=conventions -->

<!-- user:section:start=custom-conventions -->
## Custom Conventions

[Add your own workspace-specific conventions here.]
<!-- user:section:end=custom-conventions -->
