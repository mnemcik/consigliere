# Capturing Improvement Proposals

## Meta

- **Category:** process
- **Tags:** `meta-framework`, `continuous-improvement`, `ideas`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when an improvement finding needs persisting, and by wrap's continuous improvement check before it captures. Safe to edit for your workspace.

## Summary

When Claude notices a **harness improvement proposal** — a suggestion to change how the workspace itself works (`CLAUDE.md` rules, skills, hooks, templates, indexes), or a shortfall in a skill, plugin, tool or workflow the work relied on — it MUST be persisted somewhere with a **lifecycle and a triage surface**. Inline mentions in a topical note are not enough: they have no status, no backlog row, and no promotion path. This note defines the single capture procedure so future sessions stay consistent.

## What counts as a harness improvement proposal

- Suggested changes to `CLAUDE.md` rules or workflows
- Suggested changes to skills (e.g., `wrap`, `review`, `triage`)
- Suggested changes to hooks under `.claude/hooks/`
- Suggested changes to templates under `templates/`
- Suggested changes to indexes or meta-structure (`areas/INDEX.md`, `notes/INDEX.md`, `projects/TODO.md`)
- Suggested automation or tooling that would prevent a recurring failure mode or remove recurring friction
- A skill or plugin that got something wrong, skipped a step or missed something during the work
- A built-in tool that misbehaved or made the work harder
- Friction in a workflow run through sessions

**Not** a harness improvement proposal:
- A fix in a specific project's code/docs → belongs in that project's `todo.md`
- How an external tool works (a gotcha, a workaround, a reference) → belongs in a `notes/` note (no lifecycle needed). A tool that **misbehaved** in the work is a proposal (above).
- An observation about the user's work style → belongs in `insights/` (draft)

## Surface tags

Every proposal carries **one** surface tag that says what it concerns, beside `meta-framework`:

| Tag | Concerns |
|---|---|
| `surface-cg-framework` | the framework: its skills, hooks, framework notes and `CLAUDE.md` sections |
| `surface-workspace-rules` | the workspace's own rules, notes, templates and structure |
| `surface-plugin-<name>` | a skill or plugin outside the framework, e.g. `surface-plugin-<plugin-name>` |
| `surface-claude-code` | a built-in tool of the agent's harness |
| `surface-session-workflow` | a workflow run through sessions (reviews, releases, digests, …) |

Filter on `meta-framework` for all improvement proposals, and on a `surface-` tag for one surface.

## Routing — who owns the fix

Send each finding to the place that owns its surface, in this order:

1. **The workspace's routing note**, if it has one: `notes/improvement-routing.md` maps surfaces to owners (for example, a plugin to the project that maintains it). It is workspace-owned; the framework doesn't ship it. When it sends a surface to the backlog, the finding is a proposal (step 3).
2. **The `todo.md` of a project that owns the surface**, if one exists. A finding routed to a project's `todo.md` is a todo item, not a proposal: it needs no tags and no backlog row.
3. **Otherwise, a proposal** in the backlog, by the decision rule below, with its surface tag.

If an item for the same miss already exists, add the new evidence to it instead of creating another.

## Decision rule — where does the proposal live?

| Proposal size | Where to put it |
|---|---|
| Single sentence, no bundling candidates | Keep inline in the motivating note's body **and** add a one-line row to `ideas/BACKLOG.md` pointing at the note. The backlog row is the triage surface. |
| More than a few lines, or bundles multiple related suggestions sharing one incident/goal | Create a full idea file under `ideas/<slug>.md` via `templates/idea.md`, status `raw`, tagged `meta-framework` and one surface tag. Cross-link back to the motivating note as evidence. Add a row to `ideas/BACKLOG.md`. |

When in doubt, prefer the full idea file — a backlog row pointing at a note is an edge-case shortcut, not the default.

## Required metadata

Every harness improvement proposal (inline or full idea file) MUST:

1. Include `meta-framework` as a tag — the filter that separates harness proposals from product/domain ideas in the backlog.
2. Have a backlog row in `ideas/BACKLOG.md` with a status from the standard lifecycle (`raw → exploring → ready → promoted | parked | rejected`).
3. Cross-link back to the motivating note / incident / insight so the evidence stays discoverable.
4. Name a target surface in the idea body (which skill / hook / rule / template would change). Proposals without a target are too vague to triage. This free-text target is more specific than the surface tag in item 5.
5. Carry one surface tag (see [Surface tags](#surface-tags)), in the idea's tags and the BACKLOG row's Tags column.

## Mechanical steps

1. **Route, then classify size.** If [Routing](#routing--who-owns-the-fix) sends the finding to a project's `todo.md` or another place outside the backlog (directly or through the routing note), write it there and stop. If it goes to the backlog, continue. Otherwise: single sentence, no bundling? → inline route. Otherwise → full idea file.
2. **Check for duplicates.** Search `ideas/` + `ideas/BACKLOG.md` for existing proposals targeting the same surface. Extend rather than create if a relevant idea already exists.
3. **Write the proposal** in the chosen location, including the required metadata (tags, target surface, cross-link).
4. **Add/update the BACKLOG row.**
5. **Edit the motivating note**: replace any inline "candidate fixes" wording with a one-line pointer to the idea, so the note stays about *what happened* and the idea owns *what to do about it*.

## Triage

Improvement proposals flow through the same `ideas/BACKLOG.md` triage as product ideas. Filter on the `meta-framework` tag when reviewing improvement proposals specifically, and on a `surface-` tag for one surface (for example, everything about one plugin). Promotion produces a project (or a small direct edit if trivial); rejection preserves the reasoning so the same observation isn't re-opened blindly later.

## Related

- `CLAUDE.md` → **Continuous Improvement** (the *why* and *when to notice*; this note is the *how*)
- `CLAUDE.md` → **Workflow: Idea → Project** (the downstream lifecycle once an idea is promoted)
- [`idea-to-project-workflow.md`](idea-to-project-workflow.md) — the idea lifecycle and status vocabulary
- `templates/idea.md` (the template for full idea files); `ideas/BACKLOG.md` (the triage surface)
