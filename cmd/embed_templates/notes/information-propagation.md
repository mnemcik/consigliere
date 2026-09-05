# Information Propagation — Procedure & Lookup

## Meta

- **Category:** process
- **Tags:** `information-propagation`, `knowledge-base-structure`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when new information surfaces during a session. Safe to edit for your workspace.

## Summary

After any session where new information is discussed or identified, existing tracked items (areas, projects, ideas, notes) may need updating so context doesn't go stale or contradictory. This note holds the trigger list, the step-by-step propagation procedure, and the new-information-type → what-to-update lookup table. The trigger + the "this is not optional" headline stay inline in `CLAUDE.md`.

## When to trigger

- A decision is made or a constraint is discovered
- A contact's role or availability changes
- A system's status changes (e.g., deployed, deprecated, blocked)
- New architecture or technical details surface
- A project's status or scope changes
- A meeting or sync produces information relevant to tracked items

## How to propagate

1. **Identify affected areas** — which area(s) does the new information touch?
2. **Update the area file** — add the new information to the appropriate section (Architecture & Constraints, Current State, Key Contacts, etc.). Update the `Last reviewed` date.
3. **Check associated items** — read the area's Associated Items section. For each linked project/idea/note, check if the new information changes its status, scope, dependencies, or open questions. Update if needed.
4. **Check cross-area impact** — read the area's Related Areas section. If the new information affects a related area, update that too.
5. **Update indexes** — if a project status changed, update `projects/TODO.md`. If an idea status changed, update `ideas/BACKLOG.md`.

## What to look for

| New information type | Check and update |
|---------------------|-----------------|
| Decision made | Area decisions/constraints, project `decisions.md` |
| New contact or role change | Area Key Contacts, project Stakeholders |
| System status change | Area Current State, project Dependencies |
| Scope change | Project Scope, area Overview |
| Blocker or risk | Project `README.md` Dependencies, project `todo.md`, area Architecture & Constraints |
| Meeting notes | All areas mentioned, project `log.md` for session record |

**This is not optional.** Stale information in areas and projects leads to duplicated or contradictory context across sessions.
