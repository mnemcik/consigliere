# Capturing Improvement Proposals

## Meta

- **Category:** process
- **Tags:** `meta-framework`, `continuous-improvement`, `ideas`
- **Framework note:** shipped by Consigliere and loaded on demand when a workspace-improvement proposal needs persisting. Safe to edit for your workspace.

## Summary

When Claude notices a **harness improvement proposal** — a suggestion to change how the workspace itself works (`CLAUDE.md` rules, skills, hooks, templates, indexes) — it MUST be persisted somewhere with a **lifecycle and a triage surface**. Inline mentions in a topical note are not enough: they have no status, no backlog row, and no promotion path. This note defines the single capture procedure so future sessions stay consistent.

## What counts as a harness improvement proposal

- Suggested changes to `CLAUDE.md` rules or workflows
- Suggested changes to skills (e.g., `wrap`, `review`, `triage`)
- Suggested changes to hooks under `.claude/hooks/`
- Suggested changes to templates under `templates/`
- Suggested changes to indexes or meta-structure (`areas/INDEX.md`, `notes/INDEX.md`, `projects/TODO.md`)
- Suggested automation or tooling that would prevent a recurring failure mode or remove recurring friction

**Not** a harness improvement proposal:
- A fix in a specific project's code/docs → belongs in that project's `todo.md`
- A gotcha or finding about an external tool → belongs in a `notes/` note (no lifecycle needed)
- An observation about the user's work style → belongs in `insights/` (draft)

## Decision rule — where does the proposal live?

| Proposal size | Where to put it |
|---|---|
| Single sentence, no bundling candidates | Keep inline in the motivating note's body **and** add a one-line row to `ideas/BACKLOG.md` pointing at the note. The backlog row is the triage surface. |
| More than a few lines, or bundles multiple related suggestions sharing one incident/goal | Create a full idea file under `ideas/<slug>.md` via `templates/idea.md`, status `raw`, tagged `meta-framework`. Cross-link back to the motivating note as evidence. Add a row to `ideas/BACKLOG.md`. |

When in doubt, prefer the full idea file — a backlog row pointing at a note is an edge-case shortcut, not the default.

## Required metadata

Every harness improvement proposal (inline or full idea file) MUST:

1. Include `meta-framework` as a tag — the filter that separates harness proposals from product/domain ideas in the backlog.
2. Have a backlog row in `ideas/BACKLOG.md` with a status from the standard lifecycle (`raw → exploring → ready → promoted | parked | rejected`).
3. Cross-link back to the motivating note / incident / insight so the evidence stays discoverable.
4. Name a target surface in the idea body (which skill / hook / rule / template would change). Proposals without a target are too vague to triage.

## Mechanical steps

1. **Classify size.** Single sentence, no bundling? → inline route. Otherwise → full idea file.
2. **Check for duplicates.** Search `ideas/` + `ideas/BACKLOG.md` for existing proposals targeting the same surface. Extend rather than create if a relevant idea already exists.
3. **Write the proposal** in the chosen location, including the required metadata (tag, target surface, cross-link).
4. **Add/update the BACKLOG row.**
5. **Edit the motivating note**: replace any inline "candidate fixes" wording with a one-line pointer to the idea, so the note stays about *what happened* and the idea owns *what to do about it*.

## Triage

Harness proposals flow through the same `ideas/BACKLOG.md` triage as product ideas. Filter on the `meta-framework` tag when reviewing harness improvements specifically. Promotion produces a project (or a small direct edit if trivial); rejection preserves the reasoning so the same observation isn't re-opened blindly later.

## Related

- `CLAUDE.md` → **Continuous Improvement** (the *why* and *when to notice*; this note is the *how*)
- `CLAUDE.md` → **Workflow: Idea → Project** (the downstream lifecycle once an idea is promoted)
- [`idea-to-project-workflow.md`](idea-to-project-workflow.md) — the idea lifecycle and status vocabulary
- `templates/idea.md` (the template for full idea files); `ideas/BACKLOG.md` (the triage surface)
