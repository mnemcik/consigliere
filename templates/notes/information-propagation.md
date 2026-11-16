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
2. **Update the area file** — add the new information to the appropriate section (Architecture & Constraints, Current State, Key Contacts, etc.). Update `last_reviewed` to today and add a dated entry to the area's `## Review History` saying what changed (in an area still using `## Meta`, update its `**Last reviewed:**` bullet).
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
| Work that finishes another project's todo item | That project's `todo.md` (search other projects' todos for the work's key terms): tick the item there, pointing to the evidence (commit, decision or log entry) |

### Known miss — another project's todo item

Work in one project often completes an item that sits in a *different* project's `todo.md`: a shared service gets configured, a credential gets set up, a check gets added along the way. The item lives only in the project that owns the work (rule 8 in [`project-structure.md`](project-structure.md)), and the "check off every copy" rule only searches the current project's folder, so the owner's item stays open and keeps advertising work that is done, and a later session re-plans it. So when a session finishes work, it searches the other projects' `todo.md` files for the work's key terms (a service name, a setting, an ID), as rule 4 does within one project, and ticks each open item the work completed in the project that owns it, pointing to the evidence (commit, decision or log entry).

**This is not optional.** Stale information in areas and projects leads to duplicated or contradictory context across sessions.
