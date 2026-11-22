---
title: "Analysis — session worktrees for repositories outside the workspace (#277)"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/277"
size: full
verdict: pending review
---

# Analysis — session worktrees for repositories outside the workspace (#277)

- **Item:** https://github.com/mnemcik/consigliere/issues/277 (triaged 2026-11-22, Priority Low: scope not settled;
  analysed early by the owner's choice)
- **Size:** full: several decisions, and part of the recommendation depends on #259's session state.
- **Evidence base:** consigliere `origin/main` at `61abdfd`; #259's spec on branch `gh-259-extension-commands` at
  `decbfc0` (PR #303, draft); #300's and #301's analyses on their branches; a cg binary built from `61abdfd` and run
  against throwaway local repositories (2026-11-22); Claude Code 2.1.330 and its hooks reference
  https://code.claude.com/docs/en/hooks (read 2026-11-22; the page shows no date).
- **Status:** analysis only. Nothing is implemented.

## Question

A session working in a repository outside the workspace (a service repo, a docs repo) gets none of the protection the
workspace's own session worktrees give: nothing points it to a worktree when it starts writing there, two sessions in
one clone share its index, `cg active` can't show who has a checkout of that repo, and worktrees made by hand are
never cleaned up. It affects every workspace whose sessions also work in other repositories.

This is a **"how"** item. The issue states the gap and leaves the shape open; the questions are which parts cg owns,
what triggers the prompt, and how it fits #259's session state.

## Current state (evidence)

### The worktree commands already run in any git repository

`cg worktree` resolves its repository from the current directory, not from the workspace:
`worktreeRootConfig` takes `gitx.CommonRoot` of the cwd (`cmd/worktree.go:49`) and reads `.cg.json` there
(`cmd/worktree.go:53`). `workspace.Detect` returns `nil, nil` when there is no `.cg.json`
(`internal/workspace/workspace.go:274-294`), and `WorktreeSettings` is nil-safe and fills the defaults
(`internal/workspace/workspace.go:198-213`): branch prefix `session/`, landing branch `main`, strategy
`direct-to-main` (`internal/workspace/workspace.go:21-23`).

Run in a plain repository with no `.cg.json` and an `origin` whose default branch is `main` (2026-11-22, cg built
from `61abdfd`):

| Step | Result |
|---|---|
| `cg worktree create try1` | exit 0; created `<repo>--try1` on `session/try1` off `origin/main` |
| `cg worktree list` | lists it with slug `try1`, ahead `0` |
| commit, then `cg worktree land` in it | exit 0; **pushed `session/try1` straight to `origin/main`** (direct-to-main) |
| `cg worktree remove try1` | exit 0; worktree and branch removed |

With an `origin` whose default branch is `master`, `cg worktree create t` fails: `git fetch origin main ...: couldn't
find remote ref main` (exit 128). The landing branch is the fixed default `main`, not the remote's default branch,
although `gitx.DefaultBranch` already reads `origin/HEAD` (`internal/gitx/gitx.go:323-329`).

None of this is documented for external repos: `docs/cg-subcommands.md:24-27,188-191` describe the commands and
defaults for the workspace only.

### Nothing points a session to a worktree in another repo

- The session-start gate (UserPromptSubmit) warns only when the cwd is the **workspace's** main worktree:
  `inMainWorktree(ctx, in.CWD, root)` compares the cwd's toplevel with the workspace root
  (`internal/session/gate.go:92,174-188`), and the warning text names `cg worktree create`
  (`internal/session/gate.go:53-55`). The repo a session will write to is not known when the prompt arrives.
- The only per-tool-call hooks cg installs are a PostToolUse dirty marker on `Write|Edit|NotebookEdit` and a PreToolUse
  push-policy gate on `Bash` (`templates/workspace/.claude/settings.json:19-41`). The dirty marker records only that
  the session wrote something (`cmd/session.go:100-130`).
- **The push-policy gate is cg's existing per-call hook for external repos.** It reads a repo's declared policy
  (`direct` or `pr`) from the workspace's area files and allows or denies a Bash `git push`
  (`internal/pushpolicy/pushpolicy.go:28-63,94-112`). It matches only the text `git push`
  (`internal/pushpolicy/pushpolicy.go:67,95`).
- **Side finding: `cg worktree land` gets past that gate.** Its pushes run inside cg (`internal/worktree/land.go:213,596`),
  so the Bash command the hook sees is `cg worktree land`, which `gitPushRe` doesn't match. Run in an external repo
  declared `pr`, the default `direct-to-main` strategy would push to its default branch unchecked (code reading plus
  the run above; not run against a repo declared `pr`, and the host's branch protection may still refuse).

### Session state, `cg active` and `/wrap`

- The session state file (the "badge") holds `area`, `project`, `dirty`, `paused`, `pausedAt`
  (`internal/session/context.go:43-52`). Writers update it as a map, so keys they don't own survive
  (`internal/session/context.go:32-40,93-118`).
- `cg active` lists sessions by project from those files (`internal/session/active.go:20-27,53-109`); it has no notion
  of a checkout of another repository.
- `/wrap` end mode removes the **workspace** session worktree (`templates/skills/wrap/SKILL.md:255-266`). For
  external repos it only deletes local branches whose PR merged (`templates/skills/wrap/SKILL.md:268-277`) and says
  "for external repos that require review, fall back to the repo's default: branch-and-PR"
  (`templates/skills/wrap/SKILL.md:237`). It never removes a worktree of an external repo.
- The area rules already tell a session to read the matching area before working on an external repo
  (`templates/notes/area-rules.md:25-27`); nothing there mentions worktrees.

### #259 and the session state (in flight, not decided here)

#259's spec (branch `gh-259-extension-commands` at `decbfc0`, PR #303, draft; cg 2.0.0):

- `cg start` exports `CG_WORKSPACE`, the session's root (S31), so a cg command run from an external directory can
  find the session's workspace (S24; this also closes #248).
- The session state file gains one `worktree` key: the session's **workspace** worktree, written by
  `cg worktree create` when a session ID is known and cleared by `cg worktree remove` (S35-S37). Dispatch passes it to
  extension binaries as `CG_SESSION_WORKTREE`, the place they write workspace data (S38, X4).
- A state file with only `worktree` shows in the badge and `cg active` as one without it (S40).

Two consequences for this item: (a) a record written from an external repo can find the session's state file only
through `CG_WORKSPACE`, so recording depends on #259; (b) an external repo's worktree **must not** go into #259's
`worktree` key, or an extension such as `cg shop` would write workspace data into the external repo (X4).

#300 (extension skills) and #301 (extension hook filters, decided 2026-11-22) don't touch worktrees or session state.
#301's `if` and `matcher` fields would matter only if the trigger below shipped as an extension hook instead of in core.

### Session ID in Bash

cg resolves the session ID from `--session-id`, then `CG_SESSION_ID`, then `CLAUDE_CODE_SESSION_ID`
(`cmd/session.go:343-357`). In this Claude Code session `CLAUDE_CODE_SESSION_ID` is set in Bash commands (observed
2026-11-22), so `cg worktree create` run by the agent knows its session without a flag.

### Claude Code's own worktrees (external)

Claude Code 2.1.330 has an `EnterWorktree` tool that creates a worktree under `.claude/worktrees/` of the **current**
repository, or enters an existing one of the current repository or a repository nested inside the launch directory;
paths registered by neither are rejected (tool description, Claude Code 2.1.330). The hooks reference documents
`WorktreeCreate`/`WorktreeRemove` hooks that replace git's behaviour for `--worktree`, `isolation: "worktree"` and
background sessions (https://code.claude.com/docs/en/hooks, read 2026-11-22). Neither covers a sibling repository the
session reaches by `cd`, and both are specific to one agent, while cg supports other agents (`CG_SESSION_ID`,
`cmd/session.go:343-345`).

### Hook facts the trigger relies on

From https://code.claude.com/docs/en/hooks (read 2026-11-22): PreToolUse input carries a top-level `cwd`;
`Write`/`Edit` carry an absolute `tool_input.file_path`; Bash carries `tool_input.command` and no cwd of its own.
PreToolUse output can carry `permissionDecision` (`allow`, `deny`, `ask`, `defer`) and `additionalContext`, a string
added to Claude's context and not shown as a chat message. A hook entry can carry an `if` permission rule such as
`Bash(git *)`.

**Assumption (high):** hooks in the workspace's `.claude/settings.json` run for every matching tool call in the
session, wherever the file is, because matchers select on the tool name and `if` on the permission rule, not on the
project directory. The hooks reference doesn't say so in the parts read; cg's own dirty marker relies on the same
behaviour today.

**Recon checked:** consigliere `cmd/` and `internal/` (worktree, session, pushpolicy, workspace, gitx); the
templates (workspace `CLAUDE.md`, `settings.json`, hook wrappers, `/wrap`, notes); `docs/cg-subcommands.md`;
`CONTRIBUTING.md`; issues #248 and #259's branch (analysis and spec), #300's and #301's analyses; the plugins in this
session (`pr-workflow`, `independent-review`, `analysis`: no worktree handling for other repos); Claude Code's
`EnterWorktree`/`ExitWorktree` and worktree hooks (above). The owner's extension repo (mnemcik/cg-extensions) was
not checked beyond what #259's spec says about it. Not found: any cg record of external repos a session touches.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes for the trigger, visibility, collisions and leaks. **No** for "no worktree support": the commands work in any repo, with defaults made for the workspace | gate: `internal/session/gate.go:92,174-188`; `cg active`: `internal/session/active.go:20-27`; `/wrap`: `templates/skills/wrap/SKILL.md:255-277`; the run above |
| Is the expected behaviour right? | Yes: the issue's shape (sibling folder, `session/<slug>` off the remote default branch, recorded, shown, removed) matches the workspace model; the default landing does not fit external repos | `cmd/worktree.go:80-82`; `internal/workspace/workspace.go:21-23` |
| Is it still current? | Yes. #259 records only the workspace worktree; nothing else in flight covers it | #259 spec S35-S40 |
| Is it the right problem? | Partly. The failure in the issue is that nothing pointed the session to a worktree at the moment it wrote; tooling helps only once something points to it. Plus a hazard the issue doesn't name: `cg worktree land` in an external repo pushes to its default branch past the push-policy gate | issue body; `internal/pushpolicy/pushpolicy.go:67,95`; `internal/worktree/land.go:213` |

**Verdict:** partly valid. The real problem: (1) no prompt at the moment a session writes to the primary clone of a
repo outside the workspace; (2) no record of such worktrees, so no visibility and no cleanup; (3) `cg worktree`
already works there, but undocumented and with unsafe defaults (fixed `main`, direct push past the push policy).

## Options

### Recommended — reuse `cg worktree` outside the workspace, add a write-time prompt, record after #259

In two parts, so the prompt ships without waiting for cg 2.0.0.

**Part 1 (independent of #259):**

1. **Defaults outside a workspace.** When the repository has no `.cg.json`, `cg worktree` uses the remote's default
   branch (`gitx.DefaultBranch`) as the landing branch, and `land` defaults to `pr`, not `direct-to-main`. A direct
   land must be asked for with `--strategy direct-to-main`, and is refused when the area files declare the repo `pr`
   (`pushpolicy.Lookup`), which closes the push-policy bypass. Path (`<repo>--<slug>`) and branch prefix
   (`session/`) stay as today.
2. **Write-time prompt.** A new hidden `cg session repo-gate` behind a PreToolUse hook in the workspace's
   `settings.json` on `Write|Edit|NotebookEdit`, and on `Bash` for git commands that change the index or branch
   (`git add`, `commit`, `switch`/`checkout`, `stash`, `rebase`, `merge`, `reset`). It resolves the target's git
   toplevel (the file's directory; for Bash, a leading `cd <dir>` or `git -C <dir>`, else `cwd`, as the push-policy
   gate does). When that toplevel is **the main worktree of a repository other than the workspace**, it adds
   `additionalContext` naming the repo and `cg worktree create <slug>` run there. It doesn't block, and it speaks
   once per repo per session (state in the session's own state file). The workspace's own main worktree keeps the
   start-gate warning.
3. **Docs and `/wrap`.** `docs/cg-subcommands.md` describes the external-repo behaviour; a trigger-only paragraph in
   `notes/area-rules.md` (External Repo lookup) says to work in a `cg worktree` there; `/wrap` end mode lists
   worktrees of external repos it finds by branch prefix (`git worktree list` in repos touched this session) and
   removes the ones an unforced `cg worktree remove` accepts.

**Part 2 (after #259 merges):**

4. **Record.** `cg worktree create`/`remove` run outside the workspace add/remove an entry in a **separate** key of
   the session state file (for example `repoWorktrees: [{repo, path, branch}]`), found through `CG_WORKSPACE` and
   the session ID; #259's `worktree` key is untouched. With no `CG_WORKSPACE` or no session ID, the worktree is
   still created and cg prints that it isn't recorded.
5. **Visibility.** `cg active --json` carries each live session's external worktrees; a `cg worktree create` in a
   repo where another live session already records a worktree prints that session (a warning, not a refusal).
6. **Cleanup.** `/wrap` end mode removes the session's recorded external worktrees with an unforced
   `cg worktree remove`, as it does for the workspace worktree, and reports any it refuses (open PR, dirty tree).

- **Trade-off:** reuses tested code (create/land/remove/list and their guards) and the push-policy hook's pattern;
  costs one more PreToolUse hook process per write and per matching git command, and a behaviour change in
  `cg worktree land` outside a workspace (today direct, then `pr`).
- **Wrong choice when:** the owner wants cg to stay out of other repositories altogether (then: docs only, below), or
  sessions mostly write external repos through tools the hook can't see (subagents in other tools, scripts).
- **Rabbit holes:** parsing every shell form of a git write (compound commands, aliases, `-C` with quoting): keep it
  best effort, as the push-policy gate is; submodules and LFS in the new worktree (out of scope: say so in the docs);
  a branch name the external repo's conventions require (`feat/…`): use `--branch` (decision 6), not per-repo config;
  recording worktrees from sessions not started with `cg` (decision 8).

### Alternative — docs and `/wrap` only (smallest thing that works; runner-up)

Document that `cg worktree` works in any repository, fix the two defaults (item 1), and add the trigger-only note
and the `/wrap` listing (item 3). No hook, no record.

- **Trade-off:** small, no new hook, no dependency on #259; but nothing speaks at the moment of need, which is the
  failure the issue reports (the session read the warnings it had and still wrote in the primary clone).
- **Wrong choice when:** sessions often work in external repos in parallel, where a prompt-only rule has already
  failed once.
- **Rabbit holes:** none beyond item 1.
- **Wins instead if:** the owner prefers no extra per-call hook in core, or wants evidence the note alone fails
  before adding one; the hook can follow later without rework.

### Alternative — Claude Code's own worktrees

Point sessions to `EnterWorktree`, or register a `WorktreeCreate` hook backed by `cg worktree create`.

- **Trade-off:** no new cg commands; but `EnterWorktree` accepts only the current repository or one nested in the
  launch directory, so a sibling repo reached by `cd` is rejected, and `WorktreeCreate` fires only for `--worktree`,
  subagent isolation and background sessions. It is specific to one agent.
- **Wrong choice when:** as today: cg supports agents other than Claude Code, and external repos are siblings.

### Do nothing

Keep the manual recipe. Credible only because the issue is Low; it leaves the push-policy bypass in
`cg worktree land`, which item 1 alone fixes and is worth doing regardless.

## Hidden constraints

- **Reversibility:** parts 1 and 2 are additive except the `land` default outside a workspace. That is undocumented
  behaviour today; whether the change is `feat` or `feat!` is decision 11. The new state key is additive (#259 S40's
  rule: unknown keys don't change the badge).
- **Failure mid-operation:** `create` already creates the worktree before anything else; the record (part 2) is
  written after, so a failed record leaves an unrecorded worktree, which the `/wrap` listing (item 3) still finds.
- **Security:** the trigger only adds context; it grants nothing. Item 1 narrows what `land` can push. The repo-gate
  reads paths from hook input: it must not follow them anywhere but `git rev-parse` in that directory.
- **Performance:** today a PreToolUse hook already starts on every Bash call (push policy), and a PostToolUse hook on
  every write. The repo-gate adds one process per write and per matching git command; an `if` rule
  (`Bash(git *)`) narrows the Bash side. The check is two `git rev-parse` calls.
- **Windows:** paths through `filepath` and compared after `EvalSymlinks` (as `inMainWorktree` does); the hook is a
  bash wrapper like the others, so it needs Git for Windows (`CONTRIBUTING.md`, Supported platforms); tests skip or
  run per platform.
- **Permissions (assumption, med):** a sibling `<repo>--<slug>` directory may sit outside the session's allowed
  working directories, so in Claude Code's default permission mode edits there can prompt. Docs should say so; cg
  doesn't change permissions.
- **Dependencies:** part 2 needs #259's `CG_WORKSPACE` and session-ID handling (cg 2.0.0). Part 1 needs nothing in
  flight.
- **Who else is affected:** every workspace on `cg sync` gets the new hook entry in `settings.json` and the note
  paragraph; workspaces that edited `settings.json` see it as drifted. External repos see `session/<slug>` branches
  pushed when a `pr` land runs.
- **Fit and layer (CONTRIBUTING, impact review):** core. The worktree commands, session state and push-policy gate
  are core today; an extension would have to reach into the session state file it doesn't own.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Scope | Recommended option: part 1 now, part 2 after #259 merges | open |
| 2 | Prompt strength | `additionalContext`, once per repo per session, never blocks (not `ask` or `deny`) | open |
| 3 | What the prompt watches | `Write`/`Edit`/`NotebookEdit` plus Bash git commands that change the index or branch; best effort | open |
| 4 | Command surface | Reuse `cg worktree` from the repo's directory; optionally `--repo <path>` so it runs from anywhere | open |
| 5 | `land` outside a workspace | Landing branch = remote default branch; default strategy `pr`; `direct-to-main` only with the flag and refused when the area files declare the repo `pr` | open |
| 6 | Branch name in external repos | `session/<slug>` by default, `--branch <name>` to follow a repo's convention | open |
| 7 | Where the record lives | A separate state-file key (name to agree, e.g. `repoWorktrees`); never #259's `worktree` | open |
| 8 | Sessions not started with `cg` (no `CG_WORKSPACE`) | Create works, record skipped with a printed note; no `--workspace` flag | open |
| 9 | `/wrap` cleanup when the PR is still open | Keep the worktree, report it; remove only what an unforced `cg worktree remove` accepts | open |
| 10 | `cg active` output | Add to `--json` and warn at `create`; leave the default tab-separated output unchanged | open |
| 11 | Release | Part 1 as `feat` (the old `land` default outside a workspace is undocumented); part 2 after 2.0.0. `feat!` if the owner counts the `land` default as public | open |
| 12 | Layer | Core, not an extension | open |

## Review

pending

## Verdict

pending review

## Q→A log

- (none yet)
