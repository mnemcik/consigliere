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

## How `cg sync` will use it (planned)

For each managed artifact, compare the workspace's current content hash against
the manifest hash:

- **untouched** (hashes match) → replace with the new framework content, update
  the stored hash. Hands-off.
- **drifted** (hashes differ — the user edited a framework artifact) → **never
  clobber.** Present a diff and require an explicit keep-mine / take-theirs choice.
- **new** (a managed artifact the new framework version added) → insert.
- **removed** (dropped by the new version) → flag for the user.

`cg sync` emits a structured report; the optional `/cg-sync` Claude skill reads
it plus the user's `user:section` rules to flag *semantic* contradictions
(a new framework rule that conflicts with a user rule) for a decision — the part
hashing can't do.

Drift handling is hash-classification + conflict-or-choose to start; a
stored-baseline 3-way merge is a possible later upgrade. Both use this same
manifest + ownership contract, so the upgrade is non-breaking.
