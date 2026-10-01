# Area Rules — Linking, Reference Hubs & External-Repo Lookup

## Meta

- **Category:** process
- **Tags:** `area-rules`, `knowledge-base-structure`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when linking items to areas, creating an area, or starting work on an external repo. Safe to edit for your workspace.

## Summary

Areas are the single source of truth for a domain's context; projects, ideas, and notes link to them rather than duplicating. This note holds the full area rules, the project-linking rule, and the external-repo → area lookup. The "every item needs an `Areas:` field / read the area first" headline stays inline in `CLAUDE.md`.

## Area Rules

1. **Every project, idea, and note MUST have an `Areas:` field** linking to one or more areas. Use the area slug(s).
2. **Areas are reference hubs, not duplicators.** When a project needs context about a system (contacts, constraints, architecture), link to the area file instead of writing it again. If the context doesn't exist in the area yet, add it there first, then reference it.
3. **Items can belong to multiple areas.** Use the primary area first, then secondary areas.
4. **When creating a new area,** use `templates/area.md`, add it to `areas/INDEX.md`, and add any new tags (with short descriptions) to the Area Tags section in CLAUDE.md.
5. **When reading an area for a project,** check the `Last reviewed` date. If it's older than 2 weeks, verify the content is still accurate before relying on it.

## Linking to Areas from Projects

Every project MUST link to at least one area. When starting work on a project, **read its associated area file(s) first** to understand the current context, constraints, and contacts. This is enforced by the Session-Start Rule.

## External Repo → Area Lookup

When the user asks to work on a repository outside this knowledge base (e.g., a tool or service), **check `areas/INDEX.md` for a matching area before starting work.** If an area exists, read it — it contains repo conventions (branch naming, PR title format, CI rules), architecture constraints, and context that must be followed. If no area exists, create one before proceeding. This applies whether the user names the repo explicitly or describes the tool/service by function.

### Reverse direction: finding the project from a descriptive name

The user often refers to work by a **descriptive label that matches no project slug** ("the AI platform gateway deployment" → `ai-gateway-per-client-keys`). When name-matching against `projects/`, `projects/TODO.md`, and `ideas/BACKLOG.md` all come up empty — including the semantic pass in `/match-project` — do **not** conclude the project doesn't exist. Two further probes, in order:

1. **Check `origin/main`, not just the working tree.** A project folder created by a parallel session may exist on `origin/main` while the local `main` is behind or diverged, so `ls projects/` and even a repo-wide `grep` will miss it. Use `git ls-tree origin/main projects/` and `git show origin/main:<path>` to read it without disturbing local state. Expect this whenever per-session worktrees are in use — parallel sessions land to `origin/main` continuously, so the working tree is routinely an incomplete view.
2. **Look for a related repository and read its `README.md`.** Work tracked here often corresponds to a code repo elsewhere on disk. If that repo's README backlinks to the knowledge base — a line naming the project slug and area — it resolves the slug directly. Worth establishing the convention in the other direction too: when a project produces a repo, add that backlink to the repo's README so this probe has something to find.

Both failure modes are quiet: the searches return nothing and look authoritative. Treat an empty result as "not found *here, yet*", not as "does not exist".
