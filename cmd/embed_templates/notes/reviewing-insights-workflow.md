# Reviewing Insights Workflow

## Meta

- **Category:** process
- **Tags:** `insights`, `claude-md`, `triage`, `review-process`
- **Framework note:** shipped by Consigliere and loaded on demand when reviewing draft insights for promotion. Safe to edit for your workspace.

## Summary

Insights live in `insights/` and are indexed in `insights/DRAFTS.md`. New insights are created with status `draft` and are **not** active rules — Claude must not apply them. They become rules only after the user reviews and explicitly promotes them. This note defines the mechanics of that review.

## When to load this note

- User says "review my insights", "let's go through the drafts", "promote/reject insights", or invokes a triage command on insights.
- Claude is about to change an insight's status outside of a full review sweep (e.g., promoting a single one mid-session).

## How to run

1. Read `insights/DRAFTS.md` and select rows with status `draft`.
2. Go through them one at a time (a `triage`-style skill, if installed, is ideal).
3. For each insight, present: the **observation**, the **evidence** (paraphrased), and the **Suggested Rule** verbatim so the user can judge the exact wording that would land in `CLAUDE.md`.
4. Offer options: **promote**, **edit-then-promote**, **reject**, or **defer** (skip).
5. Apply the decision using the mechanics below.
6. At the end of the sweep, present a summary (promoted / rejected / deferred counts) and commit all changes in a single commit. Stage files by name — never `git add -A`/`-a` (commit-scope discipline).

## Promote mechanics

1. **Pick the target section in `CLAUDE.md`.** Match the rule's subject to an existing section. If it fits none, propose a new section or subsection before editing.
2. **Add the rule** to `CLAUDE.md`. Prefer concise wording — trim the insight's "Suggested Rule" where needed, keep the essence. Show the user the final wording and target section before saving.
3. **Decide inline vs. note.** If the rule applies every session, it belongs inline in `CLAUDE.md`. If it only applies in specific circumstances, add a short trigger + pointer in `CLAUDE.md` and put the full detail in a dedicated note (the load-on-demand pattern — see [`claude-md-hygiene.md`](claude-md-hygiene.md)). This keeps `CLAUDE.md` lean.
4. **Update the insight file meta:** `Status:` → `promoted`; add `- **Promoted:** YYYY-MM-DD — added to CLAUDE.md → "<section>"` (or to `<note path>` if extracted).
5. **Update the index row** in `insights/DRAFTS.md`: change Status `draft` → `promoted`. Do not move the row — the table is single-source.
6. If promotion makes an older insight redundant, mark the older one `superseded` in the same pass (or fold its content in).

## Reject mechanics

1. **Ask for a one-line reason.**
2. **Update the insight file meta:** `Status:` → `rejected`; add `- **Rejected:** YYYY-MM-DD — <reason>`.
3. **Update the index row:** Status `draft` → `rejected`.
4. **Keep the file** — rejected insights stay as history documenting what was considered and why it didn't apply.

## Defer

Leave the insight as `draft` and move on. No file changes required. If the user gives a reason (e.g., "wait for another occurrence"), append `- **Reviewed:** YYYY-MM-DD — deferred, <reason>` to the meta so the next review knows it was already seen.

## One-table convention

`insights/DRAFTS.md` holds all insights regardless of status — `draft`, `promoted`, `rejected`. The Status column sorts them. Don't split into separate files; history and drafts live together so the review trail stays discoverable.

## Related

- `CLAUDE.md` → **Reviewing Insights** (inline trigger + pointer)
- [`claude-md-hygiene.md`](claude-md-hygiene.md) — inline-vs-note decision for a promoted rule
- [`session-end-capture.md`](session-end-capture.md) — how draft insights are created in the first place
