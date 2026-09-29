# Selecting the Next Project — Procedure + Active-Work Detection

## Meta

- **Category:** process
- **Tags:** `projects`, `prioritization`, `active-projects`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when the user asks what to work on next. Safe to edit for your workspace.

## Summary

When the user asks "what should I work on next?" / "pick a project" (open-ended, as opposed to naming a specific project), use this ranked procedure instead of guessing. Covers the selection ranking and active-work detection via `cg active`.

## Selection procedure

1. **Read `projects/TODO.md` top-to-bottom.** Priority sections (High → Medium → Low) are ordered, and **row order within each priority bucket is the rank** — the topmost project in its bucket is the one to pick up first.
2. **Skip these status groups** unless the user says otherwise: `Done` (already closed) and `On hold` (deliberately paused).
3. **Run `cg active --slugs`** and skip any project whose slug appears — it already has a live session somewhere, and picking it up here would double-book and risk worktree/index conflicts. If the only top candidates are active, offer the next non-active project down the list, OR tell the user which session is already on it (`cg active` without flags shows the `mtime` / `session` columns) and ask whether to pick a different one.
4. **Confirm the pick with the user before starting work** — don't just start. A one-line "top non-active High-priority project is X, want me to start there?" is the right shape.

## Rank ownership

Rank within a priority bucket is **mutable and user-owned**. Claude may suggest re-ranking (e.g., "this looks like it should be higher given the deadline") but MUST NOT silently reorder rows. Priorities themselves (`high` | `medium` | `low`) live in each project's README Meta section; when one changes there, also update `projects/TODO.md` to keep them in sync.

## `cg active` — the active-work detector

`cg active` lists sessions that still claim a project (from the per-session badge files under `.claude/session-context/*.json`) plus paused projects (from `projects/<slug>/resume.md`). A session claims a project with `cg session set-context` and releases it with `cg session release`, which the end-mode wrap runs, so a wrapped session is not listed. The caller's own session is always excluded.

| Invocation | Output |
|---|---|
| `cg active` | One line per entry (project / area / dirty / mtime / session / state columns); state is `live` or `paused`. |
| `cg active --slugs` | Distinct **live** project slugs, one per line — ideal for `grep` / `if` checks. Paused projects are not included. |
| `cg active --json` | A JSON array of entries, each with a `state` field — ideal for piping into other tooling. |

**Paused projects** are not live: nobody is working on them, so they are pickable. Picking one up means resuming it (read its `resume.md`); running `cg session set-context` on it releases the pausing session's claim.

**Liveness windows** only matter for claims that were never released (crash, closed terminal, no wrap): such a claim counts as live while its badge was written recently, within 48h by default when `dirty` and 4h when clean. Both are configurable in `.cg.json` under `session.dirtyWindowMin` / `session.activeWindowMin`.

### When to consult it

1. **Selecting the next project** — to avoid double-booking (the procedure above).
2. **Before a risky cross-session action** — editing a file another session may have open, reordering `projects/TODO.md`, rewriting an area file.
3. **When the user asks "what am I working on?" / "what sessions are live?"** — surface the output directly.

### Stale dirty sessions

A listed live session that the user says is not running is a claim that was never released: the session crashed or ended without a wrap. Flag it and suggest either rejoining to wrap it, or releasing it with `cg session release --session-id <session_id>`.

## Related

- `CLAUDE.md` → **Selecting the Next Project** (inline trigger + pointer)
- [`project-structure.md`](project-structure.md) — how to read a project once picked
