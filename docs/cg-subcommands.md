# `cg` subcommands — promoted shell mechanics

This document tracks the subcommands that promote the personal-workspace bash
helpers (`scripts/*.sh`, `.claude/hooks/*.sh`, `.claude/statusline.sh`) into the
`cg` Go binary, and the `.cg.json` configuration they read. Promoting these into
Go removes the Bash 3.2 portability constraint, drops the hardcoded
`~/source/personal-workspace` assumptions, and makes the mechanics work on
Windows.

Hooks themselves continue to ship as tiny bash **wrappers** that `exec cg …`
(see the project decision log): the wrapper is user-readable and editable, can
emit a clear error if `cg` is missing, and survives `cg init --force` rewrites
without clobbering a user's `settings.json`.

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
| `cg push-policy lookup <owner/repo>` | `lookup-push-policy.sh` | **shipped** |
| `cg push-policy gate` | `external-repo-push-policy.sh` | **shipped** |
| `cg active [--slugs\|--json]` | `active-projects.sh` | **shipped** |
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
    "statuslineUpstream": "",            // path to an upstream statusline to wrap
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
