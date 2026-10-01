# Idea → Project — Lifecycle & Status Vocabulary

## Meta

- **Category:** workflow
- **Tags:** `idea-workflow`, `knowledge-base-structure`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when an idea is captured, explored, turned into a project, parked, or rejected. Safe to edit for your workspace.

## Summary

Ideas move through a lightweight lifecycle from first capture to either a project or a closed status. This note holds the step-by-step lifecycle and the status vocabulary. The one-line trigger stays inline in `CLAUDE.md`.

## Lifecycle

1. Capture an idea in `ideas/` using `templates/idea.md`. Add it to `ideas/BACKLOG.md` with status `raw`.
2. When exploring, update status to `exploring` and flesh out the idea file.
3. When the idea is mature enough, mark it `ready` and create a project folder in `projects/` using `templates/project/`. Link the idea file as the origin in README.md.
4. Ideas that won't be pursued get marked `parked` (maybe later) or `rejected` (won't do).

## Status vocabulary

- `raw` — just captured, not yet explored.
- `exploring` — actively being fleshed out.
- `ready` — mature enough to become a project.
- `parked` — not pursuing now, maybe later.
- `rejected` — won't do.
