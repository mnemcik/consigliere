# Continuous Improvement — What to Look For, How to Suggest, Where to Capture

## Meta

- **Category:** process
- **Tags:** `meta-framework`, `improvements`, `feedback-loop`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when looking for ways to improve the way of working: the workspace itself and the skills, plugins and tools it relies on. Safe to edit for your workspace.

## Summary

Claude proactively looks for opportunities to improve the workspace's own rules, structure, workflows, templates, and conventions, and for shortfalls in the skills, plugins and tools the work relies on. `CLAUDE.md` carries the every-session trigger and the two preference rules (structural-over-behavioural, extend-existing-axes). This note carries the what-to-look-for catalogue, the how-to-suggest sequencing, and the where-to-capture handoff to [`capturing-improvement-proposals.md`](capturing-improvement-proposals.md).

## What to look for

- **Rules that were violated** — if Claude broke a rule this session (skipped the Session-Start Rule, missed a step), ask: is the rule hard to discover? Should it be linked from more places? Should it be promoted from a draft insight?
- **Rules that are missing** — if the user had to correct Claude on something no existing rule covers, propose adding it.
- **Rules that are stale or contradictory** — if a rule no longer matches how work is actually done, flag for update or removal.
- **Workflow friction** — unnecessary steps, manual lookups, repeated context-loading that could be streamlined.
- **Template gaps** — a new artifact type created without a template.
- **Cross-reference gaps** — important context exists in one place but isn't linked from where it's needed.
- **Draft insights ready for promotion** — a draft insight with multiple pieces of evidence across sessions.
- **Skill and plugin shortfalls** — a skill or plugin that got something wrong, skipped a step or missed something.
- **Tool shortfalls** — a built-in tool that misbehaved or made the work harder.

## How to suggest improvements

1. **During the session** — when you notice an opportunity, mention it briefly (a sentence or two). Don't interrupt flow for minor things; batch for session end if needed.
2. **At session end** — wrap's continuous improvement check reviews the session and captures each finding where its owner will see it (see Where to capture). A proposed change to rules, skills, hooks or code is explained (what happened, which file, what to add or modify) and committed only once the user agrees.
3. **Small, obvious fixes** (typos in rules, broken links, stale dates) can be committed directly without asking.

## Where to capture

Once an improvement is worth persisting (accepted, or parked pending triage), it MUST be captured with a lifecycle and a triage surface — never left as a passive mention in a topical note. Load [`capturing-improvement-proposals.md`](capturing-improvement-proposals.md) for the full procedure.

**Summary:** a finding goes to whoever owns its surface — the workspace's routing note, or a project that owns it — and otherwise becomes a proposal: a full idea file in `ideas/` by default; a single-sentence proposal may stay inline in the motivating note provided a one-line row is added to `ideas/BACKLOG.md`. Every proposal carries the `meta-framework` tag, one `surface-` tag, a target surface (which skill / hook / rule / template would change), and a cross-link back to the motivating evidence.

## Scope

Improvements apply to **the way of working** — `CLAUDE.md` rules, templates, notes, area files, project structure, workflows, and the skills, plugins and tools the work relies on. This does NOT mean refactoring project content or rewriting notes for style.

## Related

- `CLAUDE.md` → **Continuous Improvement** (inline rule + trigger + the two preference rules)
- [`capturing-improvement-proposals.md`](capturing-improvement-proposals.md) — routing and persistence mechanics (owner of the surface, or idea file + BACKLOG row, with surface tags)
- [`claude-md-hygiene.md`](claude-md-hygiene.md) — decides whether a promoted rule belongs inline in `CLAUDE.md` or in a note
- [`reviewing-insights-workflow.md`](reviewing-insights-workflow.md) — promote/reject/defer for draft insights
