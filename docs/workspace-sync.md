# Workspace Content Sync

How a Consigliere workspace's **content** (the `CLAUDE.md` rules, framework
notes, and the skills, slash commands, hook wrappers and status line under
`.claude/`) is upgraded in place when the framework ships new or changed content —
without manual copy-paste, and without clobbering the user's own customizations.

> **Status:** foundation landed (manifest + ownership contract). The `cg sync`
> command and `/cg-sync` skill are in progress. This document is the contract
> they build against.

## Two kinds of "update" — don't confuse them

| Concern | Command | What it changes | Owner |
|---|---|---|---|
| Replace the `cg` **binary** with a newer release | `cg update` | the executable on `$PATH` | release pipeline |
| Reconcile a workspace's **content** with the framework | `cg sync` | `CLAUDE.md` sections, framework notes and framework `.claude/` files in the workspace | this subsystem |

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

The same goes for the framework's **`.claude/` files**: the slash commands
(`.claude/commands/`), the skills (`.claude/skills/`), the hook wrappers
(`.claude/hooks/`) and `.claude/statusline.sh`. `cmd/init.go`'s
`frameworkClaudeFiles` is the single list; `cg init` installs it and `cg sync`
reconciles it. `.claude/settings.json` and `.claude/cg/session-gate.md` are
**user-owned** — written once by `cg init`, never by `cg sync`. Hooks an
extension installs are the extension's, not the framework's, and are not
tracked here either.

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
- **`files`** — keyed by workspace-relative path under `.claude/`, with the
  SHA-256 of the **whole** file. Unlike notes, frontmatter is included: a
  skill's `name:` and `description:` are framework content. `cg init` records a
  file only when what is on disk equals what it ships. Absent from manifests
  written before cg tracked these files — see *Adopting untracked files* below.
- **`schemaVersion`** — bumped only on an incompatible manifest format change.
  Adding `files` is not one: older binaries read the manifest fine, but one that
  rewrites it drops the `files` key. Nothing is lost by that: the next sync on a
  newer binary recovers the records the same way it adopts untracked files.

## How `cg sync` uses it

`cg sync` classifies every managed artifact by comparing three content hashes —
on disk, manifest-recorded (what `cg` last wrote), and framework (what this
binary ships):

- **up-to-date** — on-disk content already equals the framework's. Nothing to do.
- **updatable** — on-disk content equals what `cg` recorded and the framework has
  since changed it. The user never touched it, so it is safe to auto-update.
- **drifted** — on-disk content matches neither. The user edited a framework
  artifact: **never clobbered.** Reported for a manual / skill-driven decision.
  Also: an artifact the framework ships but the manifest never recorded, where
  a *different* file already sits on disk. cg cannot prove it wrote those bytes.
- **new** — a managed artifact the current framework adds, absent on disk or
  already byte-identical. Inserted, and recorded.
- **removed** — recorded but no longer shipped. Flagged (not deleted).
- **missing** — recorded but gone from disk. Flagged.

After an apply, every **up-to-date** artifact's record points at the framework
content too. That matters when a drift is resolved by taking the framework copy
by hand: the next framework change then reads as updatable, not as drift again.

Without flags, `cg sync` is a **dry run**: it prints a grouped report and writes
nothing. With **`--apply`** it writes the safe changes — updates *updatable*
sections in place (preserving sentinels), notes (whole-file, keeping any
workspace frontmatter) and `.claude/` files (whole-file, with the mode `cg init`
installs them with), inserts *new* ones, updates the manifest hashes for what it wrote, and bumps the recorded
framework version. Drifted, removed, and missing artifacts are never modified;
they are reported. Apply is idempotent.

### Adopting untracked files

A workspace initialised before cg tracked `.claude/` files has none of them in
its manifest. The first `cg sync --apply` sorts each one by what is on disk:

| On disk | Status | Apply |
|---|---|---|
| absent | new | installs it and records it (how a skill that shipped after `cg init` arrives) |
| identical to what the binary ships | new | records it; the bytes do not change |
| identical to a version an earlier cg release shipped | updatable | updates it to the current version |
| anything else | drifted | leaves it alone and unrecorded; resolve it by hand |

The middle case is what makes this work for an old workspace: its files are
mostly older shipped versions, unedited. cg knows them from
`cmd/claude_file_history_gen.go`, the SHA-256 of every version of every
`.claude/` file in every release, generated from the git tags by
`go generate ./cmd`. `TestClaudeFileHistoryCoversEmbed` fails if a file changes
without regenerating it.

A workspace that declined the Claude Code integration at `cg init` (the
wizard's slash-command question) has none of the files and no records. `cg
sync` reads that as "not wanted" and does not install them. It counts as opted
in once a file is recorded, or once one of the files on disk is a version cg
shipped. A file of your own at one of these paths, such as a `statusline.sh` you
wrote, does not opt you in to the rest.

**Hooks need wiring.** A hook wrapper only runs if `.claude/settings.json` calls
it, and `settings.json` is yours, so `cg sync` never edits it. A hook that a
later cg release adds is installed by sync but does nothing until you add it to
`settings.json`. A hook cg stops shipping is reported as `removed`, and
`settings.json` keeps calling it until you take it out. Skills and slash
commands need no wiring: Claude Code picks them up from their directories.

The optional `/cg-sync` Claude skill reads the report plus the user's
`user:section` rules to flag *semantic* contradictions (a new framework rule
that conflicts with a user rule) for a decision — the part hashing can't do.

Drift handling is hash-classification + leave-and-report to start; a
stored-baseline 3-way merge is a possible later upgrade. Both use this same
manifest + ownership contract, so the upgrade is non-breaking.
