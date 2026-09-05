# CLAUDE.md Hygiene — Extract vs Inline

## Meta

- **Category:** workflow
- **Tags:** `claude-md`, `documentation`, `context-window`, `knowledge-base-structure`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when editing CLAUDE.md. Safe to edit for your workspace.

## Summary

CLAUDE.md is loaded into context for **every** session in this workspace. Content that only applies in specific circumstances bloats that context for sessions that don't need it. This note defines when a rule belongs inline in CLAUDE.md versus extracted to a `notes/` file loaded on demand, and the checklist to run before any edit.

## Pre-edit checklist

Run this before writing any change to CLAUDE.md:

1. **Remote-sync** — `git fetch origin` and pull if behind. Another session may already have edited CLAUDE.md or landed related content.
2. **Every-session question** — Does the new rule apply to every session, or only when a specific trigger appears? If only-on-trigger → extract to a note, keep a ≤3-sentence pointer here (trigger + headline + load path).
3. **DRY check** — Is the content already documented elsewhere (an area file, an existing note, an index)? If yes, link instead of copying.
4. **Pointer-pattern shape** — If you are keeping content inline, does it follow trigger + one-line headline + note path? Multi-paragraph inline procedures belong in a note.
5. **Reduction pass** — While you are here, can an adjacent stale or duplicated section be trimmed? A single hygiene pass can often extract several occasional-use sections at once, materially shrinking the always-loaded prompt.

If a rule feels like it *must* be inline but is over 3 sentences, extract the procedure into a note and shrink the inline text to the trigger pattern.

## The rule

For any rule, convention, or procedure being added to CLAUDE.md, ask:

> **Does this apply to every session, or only when a specific trigger appears?**

- **Every session** → keep inline in CLAUDE.md.
- **Only on specific triggers** → extract the mechanics to a note, keep a short pointer in CLAUDE.md.

## What stays inline in CLAUDE.md

Rules that apply regardless of what the user is doing today:

- Session-Start / Session-End rules
- Project Structure & file conventions
- Areas system overview
- Git Workflow (especially commit scope — this guards every commit)
- DRY Principle
- Idea Capture
- Information Propagation Rule
- Continuous Improvement Rule
- The core memory/profile pointers

The test: can you imagine a valid session where this rule isn't relevant? If no, keep inline.

## What gets extracted

Rules that only apply when a specific topic, tool, or action comes up. Each extracts to its own `notes/<topic>.md` with a short inline pointer. Typical candidates:

- **Credential handling** → loaded only in sessions that touch secrets.
- **Tooling / integration preferences** → loaded only when wiring up an external system.
- **Domain-specific standards** (an issue-tracker's conventions, a compliance checklist) → loaded only in sessions on that domain.
- **Periodic review workflows** (insight review, backlog grooming) → loaded only when that review runs.
- **Digest / report procedures** → loaded only when that digest is requested.

## The pointer pattern

Each extracted section leaves a pointer in CLAUDE.md that contains, at minimum:

1. **The trigger** — the phrase, topic, or action that should cause the note to be loaded.
2. **A one-line headline** — the most important fact, so Claude can act correctly even without loading the full note.
3. **The note path** — so loading is a single step.

Example of a well-formed pointer (a credentials rule):

> **Use the workspace's designated credential store.** When a session touches a credential (API tokens, session cookies, webhook secrets, service accounts, DB passwords, signing keys) or does any security analysis involving credential handling, load `notes/credentials.md` for the full rules. Never silently skip the store — surface it as the chosen option or as an explicit "considered and rejected because X".

That's three sentences: trigger + headline + full-rules pointer.

## When to apply this rule

- **Every time a new insight or rule is promoted to CLAUDE.md** — route the promotion through the every-session question before saving.
- **Every time CLAUDE.md gets a new section or subsection** — ask the every-session question first.
- **Periodically, when CLAUDE.md feels heavy** — scan for inline sections that are occasional-use candidates and extract them. Extracting several occasional-use sections in one pass is the fastest way to bring the always-loaded prompt back under control.

## Edge cases

- **Rules that apply broadly but not universally** (e.g., the DRY Principle): keep the core principle inline, but put detailed mechanics or examples in a note if they grow long.
- **Rules with a mandatory invariant** (e.g., "drafts are not active rules"): always keep the invariant inline — the pointer doesn't substitute for a hard constraint.
- **Inline pointer + note dual content:** never duplicate the full text in both places. The pointer states the trigger and headline; the note carries the mechanics. Duplication drifts out of sync.
