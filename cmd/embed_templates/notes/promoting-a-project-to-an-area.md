# Promoting a Project to an Area

## Meta

- **Category:** process
- **Tags:** `project-lifecycle`, `areas`, `classification`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when a project has outgrown single-project tracking. Safe to edit for your workspace.

## Summary

Projects are short-to-mid-term and must end; areas persist. When a project accumulates long-lived architectural decisions, spawns multiple distinct feature tracks, or starts looking like "maintenance with no end", promote its durable content to a new area and re-bound the remaining work as discrete projects.

## When to promote (criteria)

Any **one** of these signals is usually enough; two or more makes it obvious:

- **Decisions-log outgrowing a single release.** The project's `decisions.md` is dominated by architectural invariants that will still be true after the current release ships, not decisions about *this* release.
- **Multiple distinct feature tracks in one todo list.** The `todo.md` contains items that are clearly separate initiatives (different scope, stakeholders, or exit criteria).
- **No credible "done when" you can write.** If the exit criterion is some variant of "forever" / "whenever we stop using it", the project is an area in disguise.
- **Related items want to link to it as context, not reference it as a peer.** Other projects/notes reference the tool/practice to explain constraints — that's area behaviour ("reference hub"), not project behaviour ("bounded deliverable").

## When NOT to promote

- The project really is bounded and the open items are just polish. Ship the polish and mark it `done`.
- Architectural-looking decisions are actually release-scoped (e.g., "use library X for v1" where v1 is the whole project).
- The work has one clear end state even if it will take months.

## Mechanical procedure

1. **Create the area file** at `areas/<slug>.md` from `templates/area.md`. Summarise active architectural invariants from the project's `decisions.md` in the area's **Architecture & Constraints** section, linking back to `decisions.md` for the full supersession history. Populate **Current State** with the open feature tracks as bullet candidates (each destined to become its own project).
2. **Register the area.** Add it to `areas/INDEX.md`; reuse existing **Area Tags** before inventing near-duplicates (add any genuinely new tag to the workspace's tag vocabulary).
3. **Retire the source project.**
   - Rewrite `README.md`: a title reflecting the bounded release, status `done`, an explicit `Done when:` checklist with every box ticked, `Areas:` pointing to the new area.
   - Rewrite `todo.md`: point forward to the area's open feature tracks; preserve the full `Completed` list as the release's history.
   - Prepend a retirement entry to `log.md` explaining the promotion, listing new cross-references, and naming the first spawned project.
4. **Spawn the first bounded project** from the area's open feature tracks (usually just the one you're picking up now). Each new project gets an explicit `Done when:` and links its `Areas:` field to the new area.
5. **Update `projects/TODO.md`.** Mark the retired project `Done`, repoint its Areas column to the new area, and add a row for each spawned project.
6. **Update cross-references in related areas.** Any area that listed the old project under **Associated Items → Projects** should remove that entry and add the new area to its **Related Areas** list. Don't duplicate project links across both the owning area and related areas — only the owning area lists the projects.
7. **Update the session-context badge** if you're continuing in the same session — the active area/project now differ from session start.

## Why the procedure matters

- The project folder remains the authoritative *history* of the release (decisions log, completed-todo list, `log.md`). Don't move those into the area — they'd clutter its reference role.
- The area becomes the authoritative *current state and forward reference*. Future sessions read the area first, then follow links into specific spawned projects.
- Splitting spawned projects up front (even just the first) prevents the "let's add one more thing to this project" drift that caused the promotion in the first place.

## Related

- [`project-structure.md`](project-structure.md) — the standard project files and lifecycle.
- [`area-rules.md`](area-rules.md) — area linking, reference-hub discipline, and the freshness check.
