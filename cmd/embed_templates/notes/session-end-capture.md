# Session-End Capture — Notes & Insights Procedures

## Meta

- **Category:** process
- **Tags:** `session-end`, `knowledge-base-structure`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand at session end when capturing notes and insights. Safe to edit for your workspace.

## Summary

Before ending a session, two distinct outputs get captured: **notes** (topic/resource findings) and **insights** (user-work-style observations, always as drafts). This note holds both step-by-step procedures. The trigger and the "insights are drafts only" rule stay inline in `CLAUDE.md`.

## A. Notes — Topic/Resource Findings

Findings from working on a specific topic, tool, or resource (gotchas, patterns, technical learnings).

1. Create or update a note in `notes/` using `templates/note.md`
2. Assign a category: `tool-gotchas`, `workflow`, `architecture`, `process`, `research`, `reference`, or `troubleshooting`
3. Add relevant tags for discoverability
4. Update `notes/INDEX.md` under the appropriate category heading
5. Consolidate with existing notes — extend rather than duplicate
6. If a finding is broadly relevant as a convention/constraint, also add it to CLAUDE.md

## B. Insights — User Work Style Observations (Drafts Only)

Observations about how the user prefers to work with Claude — prompting patterns, communication preferences, decision-making style, collaboration expectations.

1. **Check for duplicates first** — Read `insights/DRAFTS.md` and check if the observation is already captured (as `draft`, `promoted`, or `rejected`). If an existing insight covers the same theme, skip or add new evidence to that file instead of creating a new one.
2. Create an insight file in `insights/YYYY-MM-DD/` (today's date subfolder) using `templates/insight.md` with status `draft`
3. Include concrete evidence (paraphrased examples from the session)
4. Propose a suggested rule that could be added to CLAUDE.md if promoted
5. Add a row to the table in `insights/DRAFTS.md` with the insight name, status `draft`, date, and file link

**CRITICAL: Insights are always created as drafts. Claude MUST NOT apply, follow, or reference draft insights in its behavior. They only become active rules when the user reviews them and promotes them to CLAUDE.md.**
