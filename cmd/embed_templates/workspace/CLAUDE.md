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
5. **Check for paused state.** If `projects/<slug>/resume.md` exists, the project was paused mid-work by `/wrap pause`. Read it as the authoritative pickup context, surface it to the user, and delete it once they confirm work has resumed.
6. **Proceed with work.** Only after steps 1–5 are complete.

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

Every project, idea, and note MUST carry an `Areas:` field linking to one or more areas; areas are reference hubs (link to them, don't duplicate their content). When starting work on a project — or on any external repo — read the matching area file first for its context, constraints, and conventions.

For the full area rules (multi-area items, creating a new area, the `Last reviewed` freshness check), the project-linking rule, and the external-repo → area lookup, load [`notes/area-rules.md`](notes/area-rules.md).
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

<!-- cg:section:start=confidence-gate -->
## Confidence Gate — Non-Trivial Tasks

Before non-trivial work (multi-file changes, new mechanisms, design decisions, ambiguous asks), gauge confidence in **intent** and **approach**. If ≥95% sure on both, proceed. If not, name the ambiguity and either ask one clarifying question or present 2–3 options before implementing. Trivial edits and exploratory questions are exempt — proceed directly.
<!-- cg:section:end=confidence-gate -->

<!-- cg:section:start=exploratory-options -->
## Exploratory Questions — Option-List Shape

For exploratory questions with **2+ reasonable solution shapes** (scope, location, mechanism, timing), structure the response as: one-line recommendation → numbered alternatives with main tradeoff each → short pick-one closer. For questions with an **obviously dominant answer**, a single recommendation is still enough — don't pad follow-up asks with options that have dominant defaults. The option-list shape is only worth the extra lines when the answer genuinely has multiple valid directions.
<!-- cg:section:end=exploratory-options -->

<!-- cg:section:start=post-completion-coverage -->
## Post-Completion Coverage Questions

When the user asks a coverage/adequacy question right after Claude reports work complete ("is it properly covered?", "is this enough?", "are the tests good?", "did you handle X?"), treat it as a request for **gap analysis, not reassurance**. Respond with: (1) what *is* covered (tight summary), (2) what is *not* covered (named, concrete gaps), (3) honest judgment on whether each gap is worth closing. Then wait for direction. Don't manufacture gaps to look thorough — if the work genuinely covers what matters, say so explicitly.
<!-- cg:section:end=post-completion-coverage -->

<!-- cg:section:start=problem-capture -->
## Problem Capture — Describe, Don't Solution

When asked to capture a problem, open question, feature request, or requirement, describe **what** the problem is — the observable behavior, the gap, the thing that needs resolving. Do not prescribe candidate solutions, option matrices, or recommended approaches. If rationale helps, explain the *consequences* or *impact* of the problem, not the shape of a solution. Solutioning is a downstream step owned by whoever resolves the question. Override only when the user explicitly asks for options or recommendations.
<!-- cg:section:end=problem-capture -->

<!-- cg:section:start=reframing -->
## Reframing Questions — Zoom Out from Tactical

When the user responds to your tactical options with a reframing question (*"should we rather …"*, *"is the real issue …"*, *"why not Y instead?"*), treat it as a root-cause challenge, not a preference among your options. Evaluate honestly whether the tactical fix is solving a symptom — if yes, say so and let the tactical work go. When the reframe reveals a larger concern that deserves dedicated tracking, promote it to its own project/idea instead of burying it in another project's todo. Don't defend in-progress tactical work for its own sake; wasted effort on a mooted path is cheaper than committing to it.
<!-- cg:section:end=reframing -->

<!-- cg:section:start=parallel-by-default -->
## Parallel by Default for Independent Operations

When multiple operations are independent — no data dependency between them — run them in parallel, not sequentially. Multiple `Read`/`Bash`/`Grep` calls go in a single response; multiple long-running jobs go in parallel background `Bash` runs (`run_in_background: true`) or fan out to subagents; independent subagent work goes in a single response with multiple `Agent` tool calls. Sequential is the right choice only when a later step needs an earlier step's output.
<!-- cg:section:end=parallel-by-default -->

<!-- cg:section:start=evidence -->
## Evidence Over Inference

When a recommendation, instruction, or trade-off depends on what users, teams, or environments already have in place (tooling installed, permissions granted, naming conventions, account/vault configuration, team practices), do **not** assert the precondition. Either verify it (ask the user, read repo/docs/config, run a command) or caveat it plainly — *"assuming X; flag if that's not the case"*. Inference from a single signal (it's a Node repo → "everyone has Node"; one folder follows a naming convention → "every folder does") is not evidence. Cost of verification is seconds; cost of an unfounded anchor is doc rewrites and lost trust on the next recommendation.

When diagnosing a performance or behaviour problem, separate **state** (allocation snapshots, accumulated counts, lifetime totals) from **rate** (paging/sec, I/O during the slow window, sampled deltas). Causal claims require rates or timings, not snapshots. "47G memory used" is a state; "0 swapins, 0 swapouts in the slow window" is a rate. If the user challenges an attribution rooted in a static number, don't defend — re-measure with a time-derivative tool (`vm_stat 1 3`, `iostat`, `top -l 2 -i 1`, `time <cmd>`) and revise openly.

For analyses that involve **external vendors, products, pricing, governance, acquisitions, or other time-sensitive state**, verify load-bearing claims via web search / fetch / official MCPs *before* presenting — not only when prompted. Training-data assertions about acquisition status, licence model, project affiliation, current pricing, or product archive state go stale fast and break the credibility of the whole artifact when one is wrong. A claim is load-bearing if it justifies a recommendation, would flip the recommendation if false, or will be quoted in an external artifact (ADR, review, proposal). Cite sources inline so freshness is auditable; flag any claim that couldn't be verified.
<!-- cg:section:end=evidence -->

<!-- cg:section:start=dont-guess-api -->
## Don't Guess API Contracts

When calling an API, tool, or service with a non-trivial contract (endpoint paths, HTTP verbs, request/response body shapes, parameter names), consult the official API reference *before* calling. Do not probe by trying guessed endpoints or body formats: a 404 from a wrong path misreads as "unsupported", and a guessed body can mutate state unexpectedly. If the docs are silent, find an authoritative source (OpenAPI spec, SDK source, vendor support) rather than brute-forcing variants.
<!-- cg:section:end=dont-guess-api -->

<!-- cg:section:start=bug-diagnosis -->
## Bug Diagnosis — Evidence Before Fixing

For bug diagnosis specifically (user reports broken behaviour, asks "why doesn't X work", or requests a fix for a non-trivial symptom — especially UI / layout / scroll / resize / component-framework / framework-internal), state **reproduction-or-trace + hypothesis-with-evidence + one-alternative-ruled-out** before proposing the change. Trivial bugs (typo, obviously-wrong constant) are exempt.

For the full contract, why single-hypothesis debugging is the trap, and the exceptions, load [`notes/debugging-evidence-contract.md`](notes/debugging-evidence-contract.md).
<!-- cg:section:end=bug-diagnosis -->

<!-- cg:section:start=remote-sync -->
## Remote-Sync Check

**Run `git fetch origin` before reading repo state to review, assess, or summarize, and `git pull --ff-only` if the current branch is behind.** Other concurrent sessions may land commits since your worktree was created; stale local state produces incorrect reviews. Fresh worktrees from `cg worktree create` start synced and `cg worktree land` handles push-time drift automatically — this rule is specifically about **read-time** freshness when you're about to inspect, review, or assess files.
<!-- cg:section:end=remote-sync -->

<!-- cg:section:start=shared-state-auth -->
## Shared-State Actions Require Per-Action Authorisation

**Never run `gh pr merge`, a release-firing `git push origin <tag>`, a force-push to a shared branch, or equivalent shared-state-firing actions without explicit per-action authorisation from the user** — even under auto mode, even with green CI + approved reviews + resolved threads. Summarise the state and pause for explicit direction (*"pr merged"*, *"merge it"*, *"can you execute? i'll approve"*). A "wait for CI and review" instruction is a prerequisite, not an authorisation. Narrow per-action delegation is fine; widen it only if the user says so explicitly.
<!-- cg:section:end=shared-state-auth -->

<!-- cg:section:start=apply-uncontroversial -->
## Apply Uncontroversial Review Findings Without Asking

When CI / CodeRabbit / Copilot posts findings on a PR Claude just opened, **validate first, then apply silently**: findings that pass validation and are small + localised + reversible (lint, type errors, obvious bugs with suggested diffs) get applied → pushed → re-checked → summarised, with no intervening confirmation prompt. This pairs with the shared-state rule above — **fix automatically, merge only on explicit authorisation.**

For the per-finding validation checklist, when to push back instead of applying, and the rule that a target repo's own review config (`.coderabbit.yaml`, `CODEOWNERS`, contributor guide) overrides a principled "out of scope" rejection, load [`notes/apply-uncontroversial-review-findings.md`](notes/apply-uncontroversial-review-findings.md).
<!-- cg:section:end=apply-uncontroversial -->

<!-- cg:section:start=bulk-ops-preflight -->
## Bulk / Destructive Ops — Pre-flight

For any command that iterates over many entities (bulk permission changes, mass invitations, multi-repo edits, remote-URL rewrites, multi-file rename/`sed` sweeps, `--paginate` API mutations, `for X in $(...)` loops), state **target set + exclusion set + single-sample dry-run + quoting/arity check, then wait for approval** before running the full batch. Threshold: more than ~5 entities, or any set you can't visually enumerate before the command runs.

For the per-step pre-flight, the default exclusion set, the shell-splitting tells, and the reversibility carve-out, load [`notes/bulk-ops-preflight.md`](notes/bulk-ops-preflight.md).
<!-- cg:section:end=bulk-ops-preflight -->

<!-- cg:section:start=continuous-improvement -->
## Continuous Improvement

Actively look for opportunities to improve how this workspace works — rules, structure, workflows, templates, conventions. Scope is **meta-framework** (`CLAUDE.md` rules, templates, notes, area files, project structure, workflows), NOT project content or note-style refactors.

- **Prefer structural fixes over behavioural rules.** When a reliability gap depends on Claude remembering to do something, prefer a structural fix (skill, hook, script, settings) over a behavioural rule — behavioural rules fail on the very turn they're most needed; structures don't. Reserve behavioural rules for judgment calls that genuinely can't be automated.
- **Prefer extending existing axes over adding new ones.** When a requirement seems to demand a new dimension (tag, field, category, flag, status, area), first ask whether an existing dimension in the same layer could absorb it. New axes are permanent cost — documentation, migration, tooling, *"which value?"* dilemmas. Surface the tradeoff explicitly when proposing either path.

For the what-to-look-for catalogue and the in-session vs. end-of-session sequencing, load [`notes/continuous-improvement.md`](notes/continuous-improvement.md). When a proposal is worth persisting, it MUST land somewhere with a lifecycle — load [`notes/capturing-improvement-proposals.md`](notes/capturing-improvement-proposals.md) (default: a `meta-framework`-tagged idea file + an `ideas/BACKLOG.md` row). Use [`notes/claude-md-hygiene.md`](notes/claude-md-hygiene.md) to decide whether a promoted rule belongs inline or in a note.
<!-- cg:section:end=continuous-improvement -->

<!-- cg:section:start=dry-principle -->
## DRY Principle — Single Source of Truth

Every piece of information has **one authoritative location**; other files link to it. Domain/system context lives in area files; project status/scope/decisions live in the project folder; workflow steps live in notes or `CLAUDE.md`. When adding information, always ask: *"Is this already documented somewhere? If so, link — don't copy."* When the same detail appears in multiple places, consolidate into the authoritative location and replace the copies with links.
<!-- cg:section:end=dry-principle -->

<!-- cg:section:start=reviewing-insights -->
## Reviewing Insights

When the user asks to review insights (phrases like "review my insights", "go through the drafts", "promote/reject insights", or a triage command on insights), load [`notes/reviewing-insights-workflow.md`](notes/reviewing-insights-workflow.md) for the full promote / reject / defer mechanics. Draft insights are never active rules until promoted.
<!-- cg:section:end=reviewing-insights -->

<!-- cg:section:start=selecting-next-project -->
## Selecting the Next Project to Work On

When the user asks "what should I work on next?" / "pick a project" (open-ended, not naming a specific project), use the ranked procedure — don't guess. In short: read `projects/TODO.md` top-to-bottom (priority then rank), skip Done/On-hold, skip projects already live in another session per `cg active --slugs`, then confirm the pick before starting. Rank within a priority bucket is **user-owned** — suggest re-ranking but never silently reorder rows.

For the full selection procedure, the `cg active` flags/liveness windows, and when else to consult active-work detection, load [`notes/selecting-next-project.md`](notes/selecting-next-project.md).
<!-- cg:section:end=selecting-next-project -->

<!-- cg:section:start=promoting-to-area -->
## Promoting a Project to an Area

When a project accumulates long-lived architectural decisions, spawns multiple distinct feature tracks, or has no credible "done when", consider promoting its durable content to a new **area** and re-bounding the remaining work as discrete projects. For the criteria and the mechanical procedure, load [`notes/promoting-a-project-to-an-area.md`](notes/promoting-a-project-to-an-area.md).
<!-- cg:section:end=promoting-to-area -->

<!-- cg:section:start=tooling-preferences -->
## Tooling Preferences — MCPs and Custom Code

When a task needs external-system integration (Slack, Jira, Confluence, GitHub, Gmail, Drive, Calendar, Notion, etc.), prefer in this order: **official / first-party MCP tools already installed → custom code you fully control (scripts in the workspace) → third-party MCP servers** (last resort; pin a version, audit the code, narrow the tool surface). When an official tool lacks a capability, write a custom script rather than reaching for a third-party MCP.

For the full preference order, the gap-handling rule, and the reasoning, load [`notes/tooling-preferences-mcps.md`](notes/tooling-preferences-mcps.md).
<!-- cg:section:end=tooling-preferences -->

<!-- cg:section:start=conventions -->
## Conventions

- Idea statuses: `raw` → `exploring` → `ready` → `parked` | `rejected`
- Project statuses: `defining` → `in-progress` → `done` | `on-hold`
- Project priorities: `high` | `medium` | `low` — stored in each project's README.md Meta section and in `projects/TODO.md` (sorted by priority)
- **Rank within a priority bucket** in `projects/TODO.md` is the row order and is **user-owned** — suggest re-ranking, but never silently reorder rows.
- Area tags: free-form, multi-valued. The current tag vocabulary is listed in the **Area Tags** section above. **Reuse an existing tag before inventing a near-duplicate.**
- Tags on ideas are free-form. Use them to group and filter; reuse existing values where they fit.
- Areas on all items are mandatory. Use area slugs from `areas/INDEX.md`.
- Not everything becomes a ticket — projects may produce tools, docs, automation, or just notes.
- **Pull request URLs:** Always provide full URLs (e.g., `https://github.com/org/repo/pull/123`) when referencing pull requests, not shorthand like `org/repo#123`. The user needs clickable links to navigate directly.
<!-- cg:section:end=conventions -->

<!-- user:section:start=custom-conventions -->
## Custom Conventions

[Add your own workspace-specific conventions here.]
<!-- user:section:end=custom-conventions -->
