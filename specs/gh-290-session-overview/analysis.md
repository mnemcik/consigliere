---
title: "Analysis — one view of which sessions are running, waiting on the user, or ready for review (#290)"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/290"
size: full
verdict: pending review
---

# Analysis — one view of which sessions are running, waiting on the user, or ready for review (#290)

- **Item:** https://github.com/mnemcik/consigliere/issues/290 (triaged 2026-11-22, Priority Low: scope not settled)
- **Evidence base:** `origin/main` at `61abdfd`; #259's branch `gh-259-extension-commands` (PR #303) as fetched
  on 2026-11-22; mnemcik/cg-extensions `main` on 2026-11-22; Claude Code 2.1.330 and its docs read on 2026-11-22.
- **Status:** analysis only, nothing implemented. Decisions are the owner's.

## Question

Work in a workspace runs as several agent sessions in parallel, one worktree each. Nothing shows in one place
which sessions are working, which are **waiting on the user** (a permission dialog, a question), and which have
output **ready for review**. The owner reports that a session waiting on a dialog went unnoticed for minutes on
2026-11-22 (not in the issue text). The issue
asks for one place that shows all three.

This is first a **"whether and what scope"** item (the issue says scope isn't settled, and names "who acts next
for a todo" as an open question), then a "how" for the part that holds up.

## Current state (evidence)

**What cg records about a session today**

- The session state file ("badge") is `.claude/session-context/<session-id>.json` under the badge root
  (`internal/session/context.go:20-30`). It holds `area`, `project`, `dirty` and the pause marker, nothing about
  whether the session is working or waiting (`context.go:43-51`). Writers update it as a generic map, so fields
  they don't own survive (`context.go:32-40`, `readContextMap` at `:122-145`).
- The badge exists only after the agent runs `cg session set-context` (`context.go:89-120`); `mark-dirty` is a
  no-op without it (`internal/session/markdirty.go:19-27`). A session that never claims a project is invisible
  to cg (#204).
- `cg active` lists badges with a project plus paused projects (`cmd/active.go:28-55`,
  `internal/session/active.go:47-110`). "Live" means the badge file was written within 4 h (clean) or 48 h
  (dirty) (`internal/workspace/workspace.go:24-25`, `active.go:88-95`), not that the process is alive or what it
  is doing (#232). Its JSON has project, area, dirty, mtime, session_id and state (`live` / `paused`)
  (`cmd/active.go:84-93`).
- The badge root comes from the hook's `cwd` (`cmd/session.go:108-112`, `sessionRoots` at `:585-595`), so a
  session whose current directory is an external repo resolves a different root (#277; #259 adds
  `CG_WORKSPACE`).
- The Claude Code hooks a new workspace gets: SessionStart (pull-latest), UserPromptSubmit (gate), PostToolUse
  on `Write|Edit|NotebookEdit` (mark-dirty), SessionEnd (release), PreToolUse on `Bash` (push policy)
  (`templates/workspace/.claude/settings.json`). None listens to `Notification`, `PermissionRequest` or `Stop`.
- `cg sync` never edits `.claude/settings.json`, so a hook added to the template reaches only new workspaces;
  existing ones must wire it by hand (`docs/workspace-sync.md:143-146`). An extension's `hooks` contribution, by
  contrast, merges into `settings.json` at install (`docs/extensions.md:186-190`), with an empty matcher until
  #301 lands.
- The status line shows `[area/project]` from the badge (`internal/session/statusline.go:32-47`).

**What the other rows of the issue's table are today**

- Task queue: `projects/TODO.md` and each project's `todo.md`, no "who acts next" field (issue text; templates
  unchanged).
- Paused work: `projects/<slug>/resume.md`, already in `cg active` as `paused` (`active.go:82-86`).
- Review gate: open PRs, CI and review threads live in the forge. The planned `pr-workflow` extension
  (mnemcik/cg-extensions#45) owns the PR status block and `cg pr watch`; CONTRIBUTING puts third-party tools in
  extensions, not core (`CONTRIBUTING.md`, "How a PR gets to merge", step 1, Fit and layer).

**What Claude Code itself exposes (verified 2026-11-22)**

- `claude agents --json` "Print[s] active sessions (interactive and background) as a JSON array and exit[s] (for
  scripts; works without a terminal)" (`claude agents --help`, Claude Code 2.1.330). Run on this machine it
  listed every open interactive session with `kind`, `status` (`busy` / `idle` observed), `sessionId`, `cwd`,
  `pid`, `name`, `startedAt`, in about 0.6 s per call. The `sessionId` equals `CLAUDE_CODE_SESSION_ID`, which is
  the badge's file name (`cmd/session.go:343-357`): the two sources join on it.
- The docs give `status` as `busy | waiting | idle` and, while waiting, `waitingFor` as `permission prompt`,
  `input needed`, `sandbox request`, `worker request` or `dialog open`
  (https://code.claude.com/docs/en/agent-view, read 2026-11-22; that part was read as a summary, not
  verbatim). **Not observed:** no session was waiting while this was checked, so `waiting` / `waitingFor` are
  documented, not seen.
- Agent view (`claude agents` without `--json`) is "one screen for all your background sessions: what's running,
  what needs your input, and what's done", but "Interactive sessions you have open in other terminals don't
  appear until you [background them]" with `/bg` (same page). It is a research preview: "The interface and
  keyboard shortcuts may change" (same page).
- Hook events (https://code.claude.com/docs/en/hooks, read 2026-11-22): `Notification` with types including
  `permission_prompt` (after the prompt has waited about six seconds), `idle_prompt` (about 60 s after the turn
  ends with no typing) and `elicitation_dialog`; `PermissionRequest` fires before a permission dialog is shown;
  `Stop` fires when the agent finishes responding but **not** on a user interrupt. No event fires when the user
  answers a dialog.
- Notifications: Claude Code already notifies on permission and idle prompts. Ghostty, kitty and iTerm2 can show
  a desktop notification (iTerm2 only once its alert settings are on); other terminals get none by default; and
  notifications are sent only when the user appears to be away
  (https://code.claude.com/docs/en/terminal-config, read 2026-11-22). A user looking at another pane or terminal
  may therefore get no notification at all. Inside tmux, desktop notifications need
  tmux passthrough, which the `tmux` extension turns on (cg-extensions `tmux/README.md`). OSC 7501 lets a
  terminal show working / waiting / done per interactive session, but not inside tmux (terminal-config page).
- The status line runs on events (new assistant message, mode change, …) plus an optional `refreshInterval`; it
  is hidden during permission prompts (https://code.claude.com/docs/en/statusline, read 2026-11-22).

**Recon checked:**

- consigliere `main`: `cmd/active.go`, `cmd/session.go`, `cmd/status.go`, `internal/session/*`,
  `internal/workspace/workspace.go`, `templates/workspace/.claude/settings.json` and hook wrappers,
  `docs/workspace-sync.md`, `docs/extensions.md`, `CONTRIBUTING.md`: found `cg active` and the badge; no
  waiting/working state anywhere.
- Related open issues: #204 (badge depends on set-context), #205 (end-of-turn state line), #232 (liveness follows
  badge writes), #266 (Claude Code parts into an opt-in extension), #276 (PRs in the status line), #277 (external
  repos). None records whether a session waits on the user.
- In-flight work: #259 / PR #303 spec (`specs/gh-259-extension-commands/spec.md` on its branch, S35-S40: the
  state file records `worktree`); #300 (extension skills) and #301 (hook filters) issue texts. See
  [Relation to in-flight work](#relation-to-in-flight-work).
- cg-extensions: `tmux` (launcher, notification passthrough), `live-runs` (watches headless runs only),
  `pr-workflow` issues #44-#50 (PR status, `cg pr watch`). No session overview.
- Installed plugins and skills in this session: a `pr-workflow` plugin (PR rules and watcher), nothing that shows
  session state.
- External: Claude Code agent view and `claude agents --json`, terminal notifications, OSC 7501 (above). The
  issue says an open-source mission-control app was evaluated and doesn't run against a workspace as-is; not
  re-evaluated here.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes for "waiting on the user": cg records nothing about it, and the incident of 2026-11-22 shows the cost. Partly for "running": `cg active` exists but means "claimed recently", not "alive and working". | `context.go:43-51`; `active.go:88-95`; #232 |
| Is the expected behaviour right? | "One place" is right for the pull view. It alone doesn't stop a dialog going unnoticed unless that place is on screen or something pushes; the push side is notifications, which Claude Code already sends. | terminal-config page; hooks page |
| Is it still current? | Yes. Nothing merged or open adds a waiting state; #259 adds only `worktree` to the state file. | #259 spec S35-S40 |
| Is it the right problem? | Partly. "Ready for review" is forge state and belongs to the `pr-workflow` extension. The task queue already exists; "who acts next for a todo" is a separate question. The core gap is per-session **who acts next**: the agent or the user. | CONTRIBUTING Fit and layer; cg-extensions#45 |

**Verdict: partly valid.** Real problem: *nothing shows, across parallel sessions, which ones are waiting on the
user (and which are working or done), joined with what cg knows about them (project, worktree, paused).* The
review gate and the todo queue are out of this item's core scope.

## Options

### Recommended — an overview that joins cg's sessions with the agent's live status, shipped as an extension

- **What:** an extension command (name open, decision 3) that reads cg's session records and `claude agents --json`,
  joins them on session ID, and prints one list grouped by **who acts next**. It calls
  `claude agents --json --all`, since without `--all` completed background sessions are left out
  (`claude agents --help`). For a background session `state` decides the group and `status` only adds detail; for
  an interactive session (no `state`) `status` decides:
  1. *Waiting on you*: background `state: blocked`, or interactive `status: waiting`; with `waitingFor` when the
     agent gives it (it does only while `status` is `waiting`, so a blocked session whose process has exited shows
     no reason);
  2. *Turn ended*: background `state: done`, or interactive `status: idle` (the agent finished; your next prompt is
     due);
  3. *Working*: background `state: working` (even when its process is between steps), or interactive
     `status: busy`;
  4. *Ended*: background `state: failed` / `stopped`;
  5. *Claim only*: a cg badge with no matching agent session (the session is gone or isn't a Claude Code one).
     Badges are deleted only on release, and a crashed or dirty session releases nothing, so this group applies
     `cg active`'s usual liveness windows and shows each badge's age; older ones appear only with the
     `--everywhere`-style flag. A badge whose agent session is `failed` / `stopped` goes in *Ended*, since the
     IDs join;
  6. *Paused projects* from cg (`resume.md`).

  **Which sessions:** `claude agents --json` lists every session on the machine (40 in the capture), and its
  `--cwd` filter applies to background sessions only (`claude agents --help`). The overview keeps a session when
  it has a badge in this workspace, or when its `cwd` is at or under the workspace root or one of its worktrees;
  sessions in external repos without a badge (#277) and sessions of other workspaces are left out unless a flag
  such as `--everywhere` is given (decision 2). Each row carries cg's project, area and worktree when the session
  has a badge, and "no project" when it doesn't, so a session that skipped `set-context` (#204) is still seen.

  **Durations:** the agent reports no timestamp for when waiting began (only `startedAt`, the session start), so
  a one-shot run shows no duration; `--watch` shows "waiting since" from the first time it saw the state. A
  `--watch` flag redraws every few seconds, for a terminal or tmux pane kept open as the "mission control" screen.

  **Stale, not zero:** when the agent status can't be read (`claude` missing, too old, output changed), the list
  says so ("agent status unavailable") instead of showing every session as idle (the principle the issue
  borrows).

  **Core change needed:** today `cg active --json` drops badges without a project (`internal/session/active.go:71`)
  and clean badges not written for 4 h (`active.go:88-95`), so the join would lose cg's data for exactly the
  idle-but-open sessions #232 describes. `cg active` also always leaves out the caller's own session
  (`cmd/active.go:34,76`), so an overview run from inside a session would show that session as "no project". The
  overview needs a small, agent-neutral `cg active` option that lists every badge regardless of the liveness
  windows and without excluding the caller, with its `worktree` (from #259) in the JSON (decision 5). It
  doesn't change `cg active`'s default output or liveness rules. Reading the badge files directly from the
  extension would tie it to a core-internal file format instead.

  **Which workspace:** `cg active` finds its root from the current directory only (`cmd/active.go:64-68`,
  `cmd/session.go:585-595`), so the binary runs it with its working directory set to the workspace that served it
  (`CG_EXTENSION_WORKSPACE`, #259 S28), not wherever it was called from.
- **Why this one:** the agent already knows exactly when a dialog is open, including the cases a hook can't see
  (an answered dialog, an interrupted turn). Reading it means no new state to keep correct, no hook on every tool
  call, and no `settings.json` migration. Keeping the Claude Code-specific part in an extension follows #266 and
  CONTRIBUTING's fit-and-layer rule; another agent's adapter could sit beside it later.
- **Trade-off:** depends on a Claude Code interface marked research preview and on a field (`waitingFor`) not yet
  observed here. Claude Code only. A pull view helps only while it's on screen.
- **Wrong choice when:** the owner wants the state recorded agent-neutrally in cg (other agents in use), or wants
  no dependency on a preview interface (then the runner-up); or needs it on Windows soon: extension commands
  don't start on Windows yet (`docs/extensions.md:192-196`, #239, open), so an extension command doesn't run
  there until #239 is fixed, whatever `claude` does (decision 7).
- **Rabbit holes:** parsing beyond the documented fields (stay with `sessionId`, `status`, `waitingFor`, `cwd`);
  turning `--watch` into a TUI; mapping `cwd` to a project for sessions without a badge (show the directory
  instead); folding in PR state before `pr-workflow` exists.
- **Durability** (it builds on a third-party interface): owner Anthropic; Claude Code licence is proprietary
  (couldn't verify the exact licence text in this pass); local, nothing hosted; no cost beyond the existing plan
  (`claude agents --json` reads local state); exit path: the runner-up (hook-recorded state) replaces the data
  source without changing the overview's output.

### Alternative — record "who acts next" in the cg badge from hooks (runner-up)

- **What:** core gets an agent-neutral `cg session state <working|waiting|idle> [--reason …]` that writes `state`
  and `stateAt` into the badge; `cg active` shows them and sorts waiting first. Claude Code hooks call it:
  UserPromptSubmit → working; `Notification` `permission_prompt` / `elicitation_dialog` → waiting;
  `Stop` / `idle_prompt` → idle; PostToolUse (any tool) → working when the badge says waiting.
- **Trade-off:** agent-neutral and under cg's control, but approximate: after the user answers a dialog nothing
  fires until the tool finishes (a long Bash call stays "waiting"); an interrupted turn skips `Stop` and stays
  "working"; only sessions with a badge are covered (#204). A PostToolUse hook on every tool call adds a `cg`
  process per call. Existing workspaces get the hooks only by hand-editing `settings.json`
  (`docs/workspace-sync.md:143-146`) unless they come from an extension. Writes would need #259's root resolution
  (`CG_WORKSPACE`), since today's hook bodies resolve the root from `cwd`.
- **Wrong choice when:** Claude Code is the only agent in use and its own status is good enough.
- **Rabbit holes:** making the states exact; extending badge liveness (#232) through the extra writes by
  accident (each write refreshes the mtime `cg active` reads); a status-line counter of other sessions waiting
  (the status line is hidden during permission prompts and doesn't refresh while idle unless `refreshInterval` is
  set).
- **Wins instead if:** the owner wants the record in cg for any agent, or `claude agents --json` turns out not to
  report `waiting` reliably (checking that is the first step, decision 1).

### Alternative — do nothing in cg: use Claude Code's agent view and notifications

- **What:** run parallel sessions as background sessions (`claude --bg`, or `/bg` in an open one) and use
  `claude agents` as the screen; make sure desktop notifications reach the user (terminal choice, tmux
  passthrough via the `tmux` extension).
- **Trade-off:** zero code, and agent view already groups "needs input" at the top. But it shows no cg project,
  worktree or paused work; interactive sessions started by `cg start` don't appear until backgrounded;
  background sessions run outside cg's tmux launcher; research preview.
- **Wrong choice when:** sessions are started with `cg` in terminals or tmux, as today.
- **Wins instead if:** the owner is willing to move parallel work to background sessions.
- **Stopgap within this option:** a documented one-liner that filters `claude agents --json` to sessions whose
  `cwd` is under the workspace and shows `status` / `waitingFor`. No cg change; it fits Priority Low until the
  overview exists, but has no project, worktree or paused data.

### Alternative — a full mission-control board

- **What:** one screen over sessions, the todo queue with a "who acts next" field per todo, paused projects and
  PRs ready for review, with an action log, as the issue's notes sketch.
- **Trade-off:** answers the whole issue, but the scope is unsettled: a new todo field, PR state from
  `pr-workflow` (not built yet), and an action log are each their own decision.
- **Wins instead if:** the owner wants the whole screen now and accepts building it after `pr-workflow`. The
  recommended overview is a first slice of it and doesn't block it.

## Relation to in-flight work

- **#259 (PR #303):** its state file is the same badge file; it adds `worktree` (S35-S37) and says a file with
  only `worktree` shows the same in the badge and `cg active` (S40). The recommendation reads that file only
  through `cg active` and adds no field to it. Listing every badge with its `worktree` in `cg active --json`
  (decision 5) is a small change after #259 merges; it would make `cg active` show more than S40 describes only
  under the new option, not by default. #259 also replaces the manifest's `subcommands` with manifest-v2
  `commands` and sets a naming rule for new namespaces (its spec, "Manifest shape", Q1, Q8, X6), so the
  extension's manifest shape and command name depend on what #259 merges. Recommendation: build after #259
  merges, against what it merged, without presuming its pending amendment (S2 word pattern, S9 owned commands,
  S25 unreadable session `.cg.json`). The runner-up would
  add `state` / `stateAt` to the same file; map-based writes keep both (`context.go:32-33`), and its hook writes
  should resolve the root through `CG_WORKSPACE` as #259 settles it, not assume S25's outcome.
- **#300 (extension skills):** no overlap.
- **#301 (hook filters):** only the runner-up shipped as an extension would benefit (a `Notification` matcher);
  without #301 the hook body filters itself.
- **#204 / #232:** the recommendation shows sessions without a badge and the agent's real liveness for Claude Code
  sessions in its own view; it doesn't change `cg active`'s liveness rules, which stay with #232.
- **#205:** per-message state line; complementary, not replaced.

## Hidden constraints

- **Reversibility:** an extension; removing it removes the command. No data written.
- **Failure mid-operation:** read-only; a failed `claude agents` call shows "agent status unavailable".
- **Data and compliance:** the output shows local paths and session names; it stays on the machine. Nothing is
  posted anywhere.
- **Security:** runs `claude` and `cg` from `PATH`, read-only. No hook added.
- **Performance:** `claude agents --json` took about 0.6 s per call here: fine for a command or a refresh every
  few seconds, too slow for the status line.
- **Dependencies:** Claude Code with `claude agents --json` (present in 2.1.330; the minimum version wasn't
  established); #259 for the manifest shape. **Windows:** extension commands don't start on Windows yet (#239),
  and whether `claude agents --json` works there wasn't verified; the manifest has no platform field, so the
  extension's README would say it, as `tmux`'s does ("macOS or Linux").
- **Operations:** none; nothing runs in the background unless `--watch` is used.
- **Who else is affected:** workspaces that don't install the extension see no change; core stays agent-neutral.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Data source for "waiting on the user": Claude Code's own status (`claude agents --json`) or hook-recorded state in the cg badge | Claude Code's status, after first confirming on a real pending dialog that it reports `waiting` with `waitingFor` | open |
| 2 | Scope of #290: sessions only (waiting / turn ended / working / ended / paused), with "ready for review" left to `pr-workflow` and "who acts next for a todo" split into its own issue; and which sessions are listed | Sessions only; split the todo field out; PRs join later from `pr-workflow`. List sessions with a badge in this workspace or a `cwd` in the workspace or its worktrees; others only with a flag such as `--everywhere` | open |
| 3 | Where it ships and what it's called: a new cg-extensions extension, or part of the opt-in Claude Code extension #266 proposes; the command name | A small extension in cg-extensions, built as a manifest-v2 command after #259 merges, folded into #266's extension if that is built; name chosen by the owner under #259's naming rule (`session` is a built-in namespace) | open |
| 4 | Push side: is the 2026-11-22 incident a missing view or a notification that didn't reach the user (terminal, tmux, notification setting)? | Owner to say which setup it happened in; if the notification didn't arrive, fix that setup separately, not in #290 | open (question) |
| 5 | Core change: a `cg active` option that lists every badge regardless of the liveness windows (and badges without a project, and the caller's own), with #259's `worktree` in the JSON | Yes, a small agent-neutral core change after #259 merges; the default output stays as it is | open |
| 6 | Order relative to other work, given Priority Low | After #259 merges; independent of #300 and #301 | open |
| 7 | Windows: accept that the overview doesn't run on Windows until #239 (extension commands on Windows) is fixed, or wait for #239 | Accept; say so in the extension's README | open |

## Review

**Review 1** — `specs/gh-290-session-overview/analysis.md` @ content 1c307e9 (commit 49ffe6f) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 6 should-fix, 3 nit → fixed: `cg active` windows drop the idle sessions the join needs
  (core option, decision 5); which sessions are listed (filter, decision 2); no source for the waiting duration;
  background `state` mapping; Windows and #239 (decision 7); #259's manifest shape and naming rule; Apple Terminal
  claim softened; incident labelled as the owner's report; stopgap one-liner added. owner's: none; rejected: none;
  dropped in validation: none
- Builder questions: 6 answered (session filter, cg-side source, manifest version and name, Windows, background
  mapping, which workspace the binary asks), 0 minor, 0 owner's beyond decisions 2, 3, 5, 7
- Verified true: badge path, fields and map writes; `cg active` windows, fields and paused handling; root from
  `cwd`; session ID resolution; template hooks; sync doesn't wire hooks; extension hooks merge; `claude agents
  --help` text and the observed capture; agent-view fields and preview label; hook timings; OSC 7501 and tmux;
  status line refresh; #259 S35-S40; no private details
- Not verified: `Stop` skipping user interrupts; Claude Code licence and the minimum version with
  `claude agents --json`; `claude agents --json` on Windows; `waiting` for interactive sessions (decision 1)

**Review 2** — same path @ content 16bc028 (commit 9dfd3b0), delta from 1c307e9 · 2026-11-22
- Reviewers: doc-reviewer (same lenses, delta scope, regression check of round 1)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 2 should-fix, 3 nit → fixed (in 623a685): `state` decides for background sessions,
  `waitingFor` only while waiting; call `claude agents --json --all` and rename the overview's own flag
  (`--everywhere`); run `cg active` from `CG_EXTENSION_WORKSPACE` and include the caller's session (decision 5);
  notification conditions corrected (away condition, other terminals none); one rule for subdirectories ("at or
  under"). owner's: none; rejected: none; dropped in validation: none
- Builder questions: 2 answered (badge without a live agent session → *Claim only* group; `state`/`status`
  precedence)
- Verified true: round-1 items 1-9 resolved; `active.go:71`, `:88-95`; `--cwd` background-only and `--all` in
  `claude agents --help`; `subcommands` not on Windows; #259 S28, S40, X6, Q1; no private details
- Not verified: whether `--cwd` with `--json` filters interactive sessions; `claude agents` on Windows

**Review 3** — same path @ content 460238c (commit 2d58a6d), delta from 16bc028 · 2026-11-22
- Reviewers: doc-reviewer (same lenses, delta scope, regression check of round 2)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: decision 2 still named the old `--all` flag; *Claim only*
  had no bound (now uses the liveness windows, older badges behind the flag). owner's: none; rejected: none;
  dropped in validation: none
- Builder questions: 2 answered (*Claim only* bound; a failed/stopped session with a badge goes in *Ended*)
- Verified true: round-2 items resolved apart from decision 2's flag name; `cmd/active.go:34,64-68,76`;
  `claude agents --help` `--all` and `--cwd`; terminal-config notification conditions; no private details
- Not verified: `waitingFor` only while `waiting` (documented, not observed)

## Verdict

pending review

## Q→A log

- (none yet)
