# Workspace Content Sync

How a Consigliere workspace's **content** (the `CLAUDE.md` rules and framework
notes) is upgraded in place when the framework ships new or changed content —
without manual copy-paste, and without clobbering the user's own customizations.

> **Status:** foundation landed (manifest + ownership contract). The `cg sync`
> command and `/cg-sync` skill are in progress. This document is the contract
> they build against.

## Two kinds of "update" — don't confuse them

| Concern | Command | What it changes | Owner |
|---|---|---|---|
| Replace the `cg` **binary** with a newer release | `cg update` | the executable on `$PATH` | release pipeline |
| Reconcile a workspace's **content** with the framework | `cg sync` | `CLAUDE.md` sections + framework notes in the workspace | this subsystem |

They are sequential — self-update the binary, then sync the workspace — but
independent. The verbs are kept disjoint on purpose.

## Ownership contract: two zones

Every `CLAUDE.md` is split into framework-owned and user-owned zones, delimited
by HTML-comment sentinels:

- **`<!-- cg:section:start=ID -->` … `<!-- cg:section:end=ID -->`** — **framework-owned.**
  `cg sync` may rewrite these. Users should not edit them; an edit is *detected*
  (see drift, below) and surfaced — never silently kept or silently overwritten.
- **`<!-- user:section:start=ID -->` … `<!-- user:section:end=ID -->`** — **user-owned.**
  `cg sync` never reads or writes these. This is where your own rules, Purpose,
  area tags, and custom conventions live. **If you want to change framework
  behavior, put your override in a `user:section` — never edit a `cg:section`.**

Framework **notes** (`notes/<topic>.md` shipped by the framework) are
framework-owned too, but a whole file can't carry an in-band sentinel, so their
ownership is recorded in the manifest rather than in the file.

## The manifest

`cg init` writes `.cg/manifest.json` recording every framework-managed artifact
and a content hash of what `cg` last wrote, plus the framework version. It is
**durable, committed workspace state** (not gitignored): it must survive a
re-clone so `cg sync` can reconcile after the binary self-updates.

```json
{
  "schemaVersion": 1,
  "frameworkVersion": "1.2.0",
  "sections": {
    "session-start": { "hash": "<sha256-hex>" },
    "memory-policy": { "hash": "<sha256-hex>" }
  },
  "notes": {}
}
```

- **`sections`** — keyed by `cg:section` id. The hash is the SHA-256 of the
  section's *inner* content (the bytes between the start and end markers, with
  surrounding newlines trimmed).
- **`notes`** — keyed by workspace-relative note path (forward-slash), with the
  SHA-256 of the note's content. `cg init` copies every framework note shipped
  in the embed tree into the workspace's `notes/` directory and registers it
  here automatically, so each record always has a backing file. Empty today —
  the framework ships no notes yet; the load-on-demand work populates the embed
  tree and these records light up with no further `cg init` changes.
- **`schemaVersion`** — bumped only on an incompatible manifest format change.

## How `cg sync` uses it

`cg sync` classifies every managed artifact by comparing three content hashes —
on disk, manifest-recorded (what `cg` last wrote), and framework (what this
binary ships):

- **up-to-date** — on-disk content already equals the framework's. Nothing to do.
- **updatable** — on-disk content equals what `cg` recorded and the framework has
  since changed it. The user never touched it, so it is safe to auto-update.
- **drifted** — on-disk content matches neither. The user edited a framework
  artifact: **never clobbered.** Reported for a manual / skill-driven decision.
- **new** — a managed artifact the current framework adds. Inserted.
- **removed** — recorded but no longer shipped. Flagged (not deleted).
- **missing** — recorded but gone from disk. Flagged.

Without flags, `cg sync` is a **dry run**: it prints a grouped report and writes
nothing. With **`--apply`** it writes the safe changes — updates *updatable*
sections in place (preserving sentinels) and notes (whole-file), inserts *new*
ones, updates the manifest hashes for what it wrote, and bumps the recorded
framework version. Drifted, removed, and missing artifacts are never modified;
they are reported. Apply is idempotent.

The optional `/cg-sync` Claude skill reads the report plus the user's
`user:section` rules to flag *semantic* contradictions (a new framework rule
that conflicts with a user rule) for a decision — the part hashing can't do.

Drift handling is hash-classification + leave-and-report to start; a
stored-baseline 3-way merge is a possible later upgrade. Both use this same
manifest + ownership contract, so the upgrade is non-breaking.
