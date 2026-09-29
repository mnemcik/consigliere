# `cg` subcommands — promoted shell mechanics

This document tracks the subcommands that promote the personal-workspace bash
helpers (`scripts/*.sh`, `.claude/hooks/*.sh`, `.claude/statusline.sh`) into the
`cg` Go binary, and the `.cg.json` configuration they read. Promoting these into
Go removes the Bash 3.2 portability constraint, drops the hardcoded
`~/source/personal-workspace` assumptions, and makes the mechanics work on
Windows.

Hooks themselves ship as tiny bash **wrappers** that `exec cg …` (see the
project decision log): the wrapper is user-readable and editable, degrades to a
no-op when `cg` is missing, and survives `cg init --force` rewrites without
clobbering a user's `settings.json`. `cg init` installs the whole `.claude/`
tree — `hooks/*.sh` + `statusline.sh` (framework-owned, rewritten on `--force`,
executable), `settings.json` and `cg/session-gate.md` (user-owned, never
clobbered) — and points `.cg.json`'s `session.gateTemplate` at the gate
template.

## Subcommand surface

| Subcommand | Promotes | Status |
|------------|----------|--------|
| `cg worktree create <slug> [--force]` | `create-session-worktree.sh` | **shipped** |
| `cg worktree land [<sha>] [--strategy …]` | `land-worktree-commit.sh` | **shipped** |
| `cg worktree remove <slug> [--force]` | `remove-session-worktree.sh` | **shipped** |
| `cg worktree list` | `git worktree list` parsing | **shipped** |
| `cg session start-gate` | `session-start-gate.sh` | **shipped** |
| `cg session mark-dirty` | `mark-session-dirty.sh` | **shipped** |
| `cg session pull-latest` | `pull-latest-main.sh` | **shipped** |
| `cg session statusline` | `statusline.sh` | **shipped** |
| `cg session set-context [--session-id …] --area … --project …` | manual badge-file write | **shipped** |
| `cg session release [--session-id …]` | wrap's `jq` `dirty: false` edit | **shipped** |
| `cg push-policy lookup <owner/repo>` | `lookup-push-policy.sh` | **shipped** |
| `cg push-policy gate` | `external-repo-push-policy.sh` | **shipped** |
| `cg active [--slugs\|--json] [--session-id …]` | `active-projects.sh` | **shipped** |
| `cg tags` | `area-tags.sh` | **shipped** |
| `cg colors check` | `colors-check.sh` | **shipped** |

## Exit-code contract

Promoted from `land-worktree-commit.sh`; callers (e.g. the wrap skill) branch on
these. Defined in `internal/cgerr`:

| Code | Name | Meaning |
|------|------|---------|
| 1 | `ExitUsage` | argument / usage error |
| 2 | `ExitDirty` | unlanded or uncommitted work blocks the operation |
| 3 | `ExitConflict` | rebase conflict; a rebase is left in progress |
| 4 | `ExitPushFail` | push to the remote failed after retries |
| 5 | `ExitAssertFail` | a post-operation assertion failed |

`cg worktree create` exits **2** when the target branch/worktree has unlanded
commits (unless `--force`).

## `.cg.json` v1.1 schema

All three blocks below are **optional and additive**. An absent block (or an
absent `.cg.json`) yields the documented defaults, so existing workspaces need
no migration. Existing top-level fields (`type`, `version`, `indexes`) are
unchanged.

```jsonc
{
  "type": "consigliere",
  "version": "1.x.y",
  "indexes": { "...": "..." },

  "worktree": {
    "root": "",                          // override the worktree path prefix;
                                          //   "" = the main workspace root.
                                          //   Worktree dir = "<root>--<slug>".
    "branchPrefix": "session/",          // branch = "<branchPrefix><slug>"
    "landingBranch": "main",             // sessions land onto origin/<this>
    "landingStrategy": "direct-to-main"  // or "pr"
  },

  "session": {
    "activeWindowMin": 240,              // clean-session liveness window (4h)
    "dirtyWindowMin": 2880,              // dirty-session liveness window (48h)
    "pruneDays": 7,                      // stale session-context cleanup age
    "gateTemplate": "",                  // path to a user-owned gate template;
                                          //   "" = built-in framework-neutral text
    "statuslineUpstream": "",            // shell command (run via `bash -c`) whose
                                          //   stdout is prepended to the badge; `cg
                                          //   init` seeds it from the user's prior
                                          //   statusLine command. "" = badge only
    "badgeFormat": "[{area}/{project}]"  // status-line badge format
  },

  "pushPolicy": {
    "source": "areas"                    // where external-repo push policies are
                                          //   declared (the areas/ directory)
  }
}
```

### Defaults (baked into the binary)

| Field | Default |
|-------|---------|
| `worktree.root` | main workspace root |
| `worktree.branchPrefix` | `session/` |
| `worktree.landingBranch` | `main` |
| `worktree.landingStrategy` | `direct-to-main` |
| `session.activeWindowMin` | `240` |
| `session.dirtyWindowMin` | `2880` |
| `session.pruneDays` | `7` |
| `session.badgeFormat` | `[{area}/{project}]` |
| `pushPolicy.source` | `areas` |

Workspace-specific gate wording and status-line behavior stay **out of the
binary**: the binary emits framework-neutral defaults, and a workspace supplies
its own wording via `session.gateTemplate` (a file it owns). This keeps the
framework/workspace boundary clean — the binary ships mechanics, the workspace
ships its content.

## Session badge file

Each session's badge state lives in `.claude/session-context/<session-id>.json`
under the **main worktree root**, so a session in a linked worktree reads and
writes the same file. `cg session set-context` creates it; the directory is
git-ignored.

```jsonc
{
  "area": "platform",       // area slug, set by `cg session set-context`
  "project": "api-gateway", // project slug, set by `cg session set-context`
  "dirty": false            // true once the session edits files
                            //   (`cg session mark-dirty`); the end-mode wrap
                            //   clears it
}
```

- The session ID must match `[A-Za-z0-9_-]+`.
- Writers merge into the existing file and keep fields they do not own.
- `cg session statusline` renders `area` and `project` through
  `session.badgeFormat`.
- The session ID comes from `--session-id`, else `$CG_SESSION_ID`, else
  `$CLAUDE_CODE_SESSION_ID`.

### Lifecycle

| Step | Command | Effect |
|------|---------|--------|
| Claim | `cg session set-context` | Creates or updates the badge. If the project is paused (`projects/<slug>/resume.md` exists), the pausing session's badge (written no later than `resume.md`) is deleted: this session is resuming it. |
| Work | `cg session mark-dirty` (PostToolUse hook) | Sets `dirty` on the first file edit. |
| End | `cg session release` (end-mode wrap) | Deletes the badge. |
| Pause | none | The badge stays; `cg active` reports the project as paused while `resume.md` exists. |

`cg active` never lists the caller's own session. It lists a badge as
`paused` when its project has a `resume.md` and the badge was written no later
than it, whatever the badge's age. A badge written after `resume.md` belongs to
the session resuming the project and is `live`, so `resume.md` can stay until
that session has read it. A paused project with no badge, and no live session
resuming it, is listed from its `resume.md`. Otherwise a badge is
`live` while its modification time is within `session.activeWindowMin`, or
within `session.dirtyWindowMin` when `dirty` is true. The windows only matter
for claims that were never released (crash, closed terminal, no wrap). Files
older than `session.pruneDays` are removed by the session gate.
