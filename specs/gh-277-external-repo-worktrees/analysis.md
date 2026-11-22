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
  `b939c7d` (PR #303, draft; S24, S31, S35-S40 and X4 unchanged in substance since `decbfc0`); #300's and #301's analyses on their branches; a cg binary built from `61abdfd` and run
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

- The session-start gate (UserPromptSubmit) warns when the hook's cwd is the main worktree **of the cwd's own
  repository**: `runSessionStartGate` takes `root` from `gitx.CommonRoot(cwd)` (`cmd/session.go:520-524`) and
  `inMainWorktree` compares the cwd's toplevel with it (`internal/session/gate.go:92,174-188`); the warning names
  `cg worktree create` (`internal/session/gate.go:53-55`). So it speaks for an external repo only when a prompt
  arrives while the session's cwd is that repo's primary clone. Run that way (2026-11-22, cg from `61abdfd`), it
  printed the MAIN-worktree warning, and also named a badge file **inside the external repo**
  (`<repo>/.claude/session-context/<id>.json`), where `cg session set-context` refuses ("not inside a Consigliere
  workspace"); no file was written there. Usually the cwd is the workspace when a prompt arrives, and the repo a
  session will write to is not known yet. **Assumption (med):** Claude Code keeps a `cd` across Bash calls only
  inside the session's working directories, so for most external repos the hook's cwd stays the workspace.
- The only per-tool-call hooks cg installs are a PostToolUse dirty marker on `Write|Edit|NotebookEdit` and a PreToolUse
  push-policy gate on `Bash` (`templates/workspace/.claude/settings.json:19-41`). The dirty marker records only that
  the session wrote something (`cmd/session.go:100-130`).
- **The push-policy gate is cg's existing per-call hook for external repos.** It reads a repo's declared policy
  (`direct` or `pr`) from the workspace's area files and allows or denies a Bash `git push`
  (`internal/pushpolicy/pushpolicy.go:28-63,94-112`). It matches only the text `git push`
  (`internal/pushpolicy/pushpolicy.go:67,95`), finds the target from a `cd <dir>` in the command, else
  `tool_input.cwd`, else the hook's cwd (`internal/pushpolicy/pushpolicy.go:115-131`), and finds the workspace (for
  its `areas/`) from the hook's cwd (`cmd/pushpolicy.go:69-74`): it is the one place that already holds both the
  workspace and the external repo.
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
through `CG_WORKSPACE`, so recording depends on #259; (b) **S35 as written is not limited to the session's
repository**: any `cg worktree create` with a valid session ID records `worktree` in the state file under the
badge root of the session root. Once #259 ships, a `cg worktree create` in an external repo with
`CLAUDE_CODE_SESSION_ID` set (which Bash commands have, below) would replace the session's workspace-worktree record
with the external path. S38's re-check against the session repo's worktree list keeps that path out of
`CG_SESSION_WORKTREE`, but the workspace record is lost, and writers then refuse (X4) or fall back (S38b). This
analysis doesn't assume how #259 resolves it; it is listed as input for #259's owner (decision 13). Without a fix
there, external worktrees must never be recorded in `worktree`.

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
PreToolUse output can carry `permissionDecision` (`allow`, `deny`, `ask`, `defer`); a `deny` reason is shown to
Claude and the call doesn't run. It can also carry `additionalContext`, which the page says is added to Claude's context with the tool
result, so Claude reads it **after** the call has run. A hook entry can carry an `if` field holding exactly one
permission rule (such as `Bash(git *)`), with no list syntax.

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
   branch as the landing branch, and `land` defaults to `pr`, not `direct-to-main`; a direct land must be asked for
   with `--strategy direct-to-main`. `land` itself can't find the workspace from a sibling repo (`cmd/worktree.go:49-53`
   reads `.cg.json` in the cwd's repo), so the **policy check goes in the push-policy hook**, which finds the workspace from
   the hook's cwd, and from `$CLAUDE_PROJECT_DIR` when that cwd is outside any workspace (today it then returns
   silently, `cmd/pushpolicy.go:69-74`, which would leave a `cg worktree land` run from inside `<repo>--<slug>`
   unchecked). It also matches `cg worktree land` (with `--strategy x` and `--strategy=x`) and denies a
   `direct-to-main` land in a repo the area files declare `pr`; `pr` and `local` (which doesn't push) pass. That closes the bypass without #259. Path (`<repo>--<slug>`) and branch prefix (`session/`)
   stay as today.
2. **Write-time prompt.** A new hidden `cg session repo-gate` behind a PreToolUse hook in the workspace's
   `settings.json` on `Write|Edit|NotebookEdit`, and on `Bash` for git commands that change the index or branch
   (`git add`, `commit`, `switch`/`checkout`, `stash`, `rebase`, `merge`, `reset`). It resolves the target's git
   toplevel (the file's directory; for Bash, a `cd <dir>` in the command, `git -C <dir>`, else `tool_input.cwd`, else
   the hook's cwd; the push-policy gate parses only the `cd` form today). When that toplevel is **the main worktree of
   a repository other than the workspace**, it **denies the first such call per repo per session** with a reason
   naming the repo and `cg worktree create <slug>` run there, and lets later calls in that repo through, so a session
   that means to work in the primary clone retries once. `additionalContext` alone would arrive after the write it
   warns about (Hook facts). The "already warned" state lives in a cg-owned per-session file under the workspace's
   `.claude/session-context/`, named outside the `*.json` and `*.lock` globs that badge readers and pruning use
   (`internal/session/gate.go:206`, `internal/session/active.go:54`), for example `<id>.repogate`, and pruned with the
   badges. The workspace is found from the hook's cwd, else `$CLAUDE_PROJECT_DIR`, not from `CG_WORKSPACE`, so it
   needs nothing from #259. The deny reason says that retrying is allowed. **Assumption (med):** Claude retries or
   moves to a worktree after such a reason rather than stopping to ask; a subagent may give up instead.
2b. **Start gate outside the workspace.** When the hook's cwd is in another repository, the start gate names that
   repo in its warning and keeps the badge path in the session's workspace (hook cwd, else `$CLAUDE_PROJECT_DIR`) instead of naming a
   badge file inside the external repo.
3. **Docs and `/wrap`.** `docs/cg-subcommands.md` describes the external-repo behaviour; a trigger-only paragraph in
   `notes/area-rules.md` (External Repo lookup) says to work in a `cg worktree` there; `/wrap` end mode lists
   worktrees of external repos it finds by branch prefix (`git worktree list` in repos touched this session) and
   removes the ones an unforced `cg worktree remove` accepts.

**Part 2 (after #259 merges):**

4. **Record.** `cg worktree create`/`remove` run outside the workspace add/remove an entry in a **separate** key of
   the session state file (for example `repoWorktrees: [{repo, path, branch}]`), found through `CG_WORKSPACE` and
   the session ID; #259's `worktree` key is untouched, which needs S35 scoped to the session's repository
   (decision 13). With no `CG_WORKSPACE` or no session ID, the worktree is still created and cg prints that it isn't
   recorded.
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
  best effort, as the push-policy gate is; an `if` rule such as `Bash(git *)` won't match `cd x && git add`, so the
  Bash side filters in cg, not only in `if`; the remote's default branch when `origin/HEAD` is unset
  (`gitx.DefaultBranch` then falls back to `main`, `internal/gitx/gitx.go:323-327`, common after `git init` plus
  `git remote add`): ask the remote (`git ls-remote --symref origin HEAD`) or refuse with a hint;
  submodules and LFS in the new worktree (out of scope: say so in the docs);
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
- **Security:** the trigger only refuses or annotates a call; it grants nothing. Item 1 narrows what `land` can push. The repo-gate
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
- **Dependencies:** part 2 needs #259's `CG_WORKSPACE` and session-ID handling (cg 2.0.0), and S35 scoped to the
  session's repository (decision 13). Part 1 needs nothing in flight: the policy check and the prompt's state find
  the workspace from the hook's context, not from `land` or `CG_WORKSPACE`.
- **Hosting:** the `pr` strategy needs the GitHub CLI and opens GitHub PRs only (`internal/worktree/land.go:589-593`);
  an external repo on another host can't land with the new default and would push the branch by hand. Decision 5.
- **Hook cwd:** both the push-policy gate and the proposed repo-gate find the workspace from the hook's cwd; if a
  session's cwd stays in an external repo (an added working directory), that lookup fails and the gate stays silent
  today (`cmd/pushpolicy.go:69-74`). `CLAUDE_PROJECT_DIR` in the hook environment is the fallback (assumption, high:
  the hook wrappers already use it, `templates/workspace/.claude/settings.json:7`).
- **Denying once:** the first write per repo fails by design; a session that meant to write the primary clone retries
  (assumption, med, above). A subagent or scripted run sees the same refusal once and may stop instead.
- **Who else is affected:** every workspace on `cg sync` gets the new hook entry in `settings.json` and the note
  paragraph; workspaces that edited `settings.json` see it as drifted. External repos see `session/<slug>` branches
  pushed when a `pr` land runs.
- **Fit and layer (CONTRIBUTING, impact review):** core. The worktree commands, session state and push-policy gate
  are core today; an extension would have to reach into the session state file it doesn't own.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Scope | Recommended option: part 1 now, part 2 after #259 merges | open |
| 2 | Prompt strength | `deny` the first write per repo per session with a reason, then let calls through. (`additionalContext` only: never blocks, but arrives after the write; `ask`: the user decides each time; `deny` always: forces worktrees) | open |
| 3 | What the prompt watches | `Write`/`Edit`/`NotebookEdit` plus Bash git commands that change the index or branch; best effort | open |
| 4 | Command surface | Reuse `cg worktree` from the repo's directory; optionally `--repo <path>` so it runs from anywhere | open |
| 5 | `land` outside a workspace | Landing branch = remote default branch; default strategy `pr`; `direct-to-main` only with the flag, and denied by the push-policy hook (workspace from hook cwd, else `$CLAUDE_PROJECT_DIR`) when the area files declare the repo `pr`. On a non-GitHub host `pr` fails (needs `gh`): the docs say to push the branch by hand | open |
| 6 | Branch name in external repos | `session/<slug>` by default, `--branch <name>` to follow a repo's convention | open |
| 7 | Where the record lives | A separate state-file key (name to agree, e.g. `repoWorktrees`); never #259's `worktree` | open |
| 8 | Sessions not started with `cg` (no `CG_WORKSPACE`) | Create works, record skipped with a printed note; no `--workspace` flag | open |
| 9 | `/wrap` cleanup when the PR is still open | Keep the worktree, report it; remove only what an unforced `cg worktree remove` accepts | open |
| 10 | `cg active` output | Add to `--json` and warn at `create`; leave the default tab-separated output unchanged | open |
| 11 | Release | Part 1 as `feat` (the old `land` default outside a workspace is undocumented); part 2 after 2.0.0. `feat!` if the owner counts the `land` default as public | open |
| 12 | Layer | Core, not an extension | open |
| 13 | Input for #259's owner (not decided here) | Raise on #259 before it merges: scope S35's `worktree` record to worktrees of the session's own repository, so a `cg worktree create` in an external repo doesn't replace it | open |
| 14 | Start gate outside the workspace (item 2b) | In Part 1: name the repo, keep the badge path in the workspace | open |

## Review

**Review 1**: `specs/gh-277-external-repo-worktrees/analysis.md` @ content hash `e26e4a3` (commit `65d4e8c`) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis document)
- Findings: 1 blocker, 4 should-fix, 3 nit → fixed: Part 1's push-policy refusal had no way to find the workspace
  (moved into the push-policy hook); start-gate claim corrected (it uses the cwd's repo; reproduced) and item 2b
  added; `additionalContext` arrives after the call (decision 2 now recommends deny-once); #259 S35 interaction
  restated (decision 13); `pr` needs `gh` (hosting constraint, decision 5); nits on `cd`/`-C` parsing, the
  `origin/HEAD` fallback and single-rule `if`. Dropped in validation: "the gate and dirty marker write badge files
  into the external repo" (no file is created: the badge must already exist, and the run left the repo clean).
- Builder questions: 5 → answered by fixes: how Part 1 finds the workspace; non-GitHub default (decision 5);
  owner's: deny vs annotate (decision 2), the S35 dependency (decision 13), whether the start-gate fix belongs here
  (decision 14).
- Verified true: defaults outside a workspace; fixed `main`; `land` pushes inside cg past `gitPushRe`; `/wrap` scope;
  #259 S24, S31, S35-S38, S40, X4 at `b939c7d`; hooks facts; no private context; no in-flight decision assumed.
- Not verified: the empirical runs and the `EnterWorktree` description (not reproducible by the reviewer); whether
  Claude Code resets the cwd after a `cd` outside the working directories.

**Review 2** (delta): same file @ content hash `610f745` (commit `0243469`) · 2026-11-22
- Reviewers: doc-reviewer (delta: builder check, claim check, missed option, hidden constraints, governing rules)
- Findings: 0 blocker, 3 should-fix, 2 nit → fixed: the workspace lookup in items 1, 2 and 2b now falls back to
  `$CLAUDE_PROJECT_DIR` (the push-policy hook returns silently today when the hook cwd is outside a workspace);
  the deny-once state file is named outside the badge globs; "retries once" tagged as an assumption (med) and the
  reason text says retrying is allowed; nits: only `direct-to-main` is denied (`local` doesn't push), the hooks
  quote is paraphrased. Round 1 findings 1-8: confirmed resolved.
- Builder questions: 2 → answered by fixes (land with no `cd` and the hook cwd outside the workspace; both
  `--strategy` forms).
- Not verified: #259 at `decbfc0` (reviewer had `b939c7d` only); the empirical runs; Claude Code's cwd after `cd`.

## Verdict

pending review

## Q→A log

- (none yet)
