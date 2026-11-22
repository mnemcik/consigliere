---
title: "Analysis — continuing the same project in a fresh session (#275)"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/275"
size: full
verdict: needs clarification
---

# Analysis — continuing the same project in a fresh session (#275)

- **Item:** https://github.com/mnemcik/consigliere/issues/275 (triaged 2026-11-22, Priority Low: scope not settled;
  analysed early by the owner's choice).
- **Size (proposed):** Full. More than one open decision (mechanism, scope against #261 and #191, layer), and one
  assumption (why the user restarts instead of clearing) could change the recommendation.
- **Kind:** a "how" item. That the rollover costs manual steps on every handoff is shown below; the open part is how
  to remove them.
- **Evidence base:** consigliere `origin/main` at `61abdfd` (2026-11-22). In-flight branches read, not touched:
  `gh-259-extension-commands` (spec at `decbfc0`), `gh-300-extension-skills`, `gh-301-extension-hook-filters`.
  Claude Code docs fetched 2026-11-22 (URLs inline).

## Question

When a session's work is done but the project continues, the user rolls over by hand: `/wrap`, confirm the worktree
removal, exit the agent, start it again, ask for the project's status, then pick the next item. Only the pick needs
the user's judgement. The status answer also varies with how the question is phrased, and the project the old
session was on is known at wrap time but lost by the next session. This affects every cg workspace user who works a
project across several sessions, many times a day for the owner.

## Current state (evidence)

### What happens today, step by step

| # | Step | What cg does today | Evidence |
|---|---|---|---|
| 1 | `/wrap` | End mode lands the work with `cg worktree land`, then Phase 8 cleans up and Phase 9 releases the claim. | `templates/skills/wrap/SKILL.md:28`, `:237`, `:305-314` |
| 2 | Confirm worktree removal | Phase 8: "Every destructive action requires explicit user confirmation"; for the worktree: "Offer the removal command to the user, then run it on confirmation". Removal itself is "unconditional", and an unforced `cg worktree remove` already refuses on unlanded commits or a dirty tree. | `SKILL.md:251`, `:257`, `:262-264` |
| 3–4 | Exit, start again | Nothing in cg. Claude Code's `/clear` starts a new conversation with empty context in the same process (see below), so these two steps can already be one command. | [commands](https://code.claude.com/docs/en/commands.md) |
| 5 | "Where do we stand?", then pick | Free-form. The session-start rule reads `README.md` and `todo.md` (step 4) and `resume.md` only for a paused project (step 5). Nothing fixes what is shown or in which shape. #191 records a real misread (newest-first `log.md` read with `tail`). | `templates/workspace/CLAUDE.md:51-52`; #191 |

**The project is not carried over.** The end-mode wrap releases the session claim (`cg session release` deletes the
badge file, `SKILL.md:309-314`), which is the only place cg records the session's project
(`internal/session/context.go:43-50`). The next session starts with no record of it.

### Claude Code constraints (docs fetched 2026-11-22)

- **The model can't clear or end a session.** Only some built-in commands are reachable through the Skill tool
  (`/init`, `/security-review`); "Other built-in commands such as `/compact` are not", and `/clear` is not listed
  ([skills](https://code.claude.com/docs/en/skills.md)). No hook can exit or clear: SessionStart is "Context only",
  SessionEnd has no decision control and its output is discarded
  ([hooks](https://code.claude.com/docs/en/hooks.md)). So at least one user action between sessions stays.
- **`/clear`** fires SessionEnd with reason `clear` and SessionStart with source `clear`; the SessionStart matcher
  filters on the source (`startup`, `resume`, `clear`, `compact`, `fork`) and can add `additionalContext`
  ([hooks](https://code.claude.com/docs/en/hooks.md)). `CLAUDE_CODE_SESSION_ID` "is updated on `/clear`" for Bash
  tool and hook subprocesses ([env-vars](https://code.claude.com/docs/en/env-vars.md)), so the cleared session has a
  new ID that `cg session set-context` picks up from the environment (`cmd/session.go:343-355`). Whether the Bash
  working directory survives `/clear` is not documented: **assumption (med)** that it does; the design below doesn't
  depend on it.
- **Skills** can take arguments (`$ARGUMENTS`) and see `${CLAUDE_SESSION_ID}`
  ([skills](https://code.claude.com/docs/en/skills.md)). `allowed-tools` grants permission only "during the turn
  that invokes the skill" (same page).
- **`claude "<prompt>"`** starts an interactive session with an initial prompt
  ([cli-reference](https://code.claude.com/docs/en/cli-reference.md)); `cg start [args…]` passes arguments to the
  agent unchanged (`cmd/start.go:26-55`). Whether an initial prompt that is a slash command runs the skill is not
  documented: **unverified**.
- **Claude Code's own `--worktree`** puts worktrees under `.claude/worktrees/` on `worktree-<name>` branches
  ([worktrees](https://code.claude.com/docs/en/worktrees.md)). That is a different model from cg's
  `session/<slug>` worktrees and doesn't help here.

### cg pieces a fresh session would use

- **Hooks:** `pull-latest` is registered for SessionStart with matcher `startup` only
  (`templates/workspace/.claude/settings.json:5`), so it doesn't run after `/clear`. The gate runs on
  UserPromptSubmit but lets every prompt starting with `/` through silently (`internal/session/gate.go:18`), so a
  slash command that starts work has to do the gate's job itself.
- **`settings.json` is user-owned:** `cg sync` never edits it (`docs/workspace-sync.md:143-147`), so a new or changed
  hook registration reaches only new workspaces unless users edit their own file.
- **`cg worktree create <slug>`** is idempotent, fetches the landing branch first (not under the `local` strategy),
  and prints the path (`cmd/worktree.go:86-99`). Under the push and `local` strategies, a worktree created right
  after a land therefore holds the landed state, even when the main checkout's `main` is behind (push). Two limits:
  it **reuses** an existing worktree for the slug, so a second session on a live project would share that worktree
  and its index; and it **exits 2** when the branch has unlanded commits (`cmd/worktree.go:94-95`). Under the `pr`
  strategy, a land only opens a pull request and the wrap stays pending until it merges (`SKILL.md:237,240`), so the
  branch keeps unlanded commits, `cg worktree remove` refuses (`SKILL.md:263`) and a new `create` on that slug exits
  2. "Unlanded" is `git log <landing>..HEAD` (`internal/worktree/worktree.go:158`), so after a squash merge the
  branch's commits still count as unlanded and `create` keeps exiting 2 until the worktree is removed with
  `--force`.
- **`cg session set-context`** claims the project; it requires `--area` and `--project` (`cmd/session.go:286-293`);
  for a paused project it releases the pausing session's claim and prints the worktree command and `resume.md` path
  (`cmd/session.go:262-282`). The session ID comes from `--session-id`, then `CG_SESSION_ID`, then
  `CLAUDE_CODE_SESSION_ID` (`cmd/session.go:350-355`); `/clear` updates only the last one. A project's areas are in
  its README frontmatter (`areas:`, `templates/project/README.md:5`).
- **`cg active`** lists live and paused projects (`templates/notes/selecting-next-project.md`).
- **`/match-project`** runs in an isolated fork, so what it reads doesn't stay in the conversation (#191's comment).

### Related and in-flight work

| Item | Relation to #275 |
|---|---|
| #261 (open) wrap asks to approve routine cleanup | Step 2 of this issue is #261's worktree case. Solving it twice would conflict. |
| #191 (open) `cg brief <slug>` | The fixed status output step 5 needs. Its comment adds "list and pick" and "load, then stop", which is exactly step 5's shape. |
| #259 / PR #303 (in flight, cg 2.0.0) | Records the session's worktree in the session state file (S35-S37) and exports `CG_WORKSPACE` from `cg start` (S31). #275 neither needs nor changes either: the next session gets its own ID and its own worktree record when it runs `cg worktree create`. Nothing here assumes #259's open points. |
| #300 (in flight) extension skills | Only matters if the new skill goes in an extension (D5). Core ships `wrap` as a framework file today (`cmd/init.go:572-583`). |
| #301 (in flight) extension hook matchers | Only matters for the automatic alternative, if it lived in an extension. |
| #266 (open) Claude Code parts into an opt-in extension | A new core skill is one more Claude Code file for #266 to move; it would move together with `wrap`. |
| #232 (open) liveness by badge writes | Unchanged; the next session writes a fresh badge. |

**Recon checked:** this repo (wrap skill, workspace `CLAUDE.md`, `settings.json` and gate templates, `cmd/session.go`,
`cmd/start.go`, `cmd/worktree.go`, `internal/session/{gate,end,context}.go`, `docs/workspace-sync.md`, project
templates, the selection note); open issues (#191, #232, #261, #266, #290: its branch has no analysis yet, nothing
found); in-flight specs on `gh-259-…`, `gh-300-…`, `gh-301-…`; Claude Code docs (hooks, commands, sessions, skills,
env-vars, cli-reference, permissions, worktrees). Plugins installed in this session: none offers a session handoff
(nothing found). External tools: none found that clear an agent session from inside it; the Claude Code docs
document no handoff feature.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes. Steps 1, 2 and 5 are as described; the project record is deleted at wrap end. | `SKILL.md:251,262,309-314`; `CLAUDE.md:51-52` |
| Is the expected behaviour right? | Mostly. "Lands in a fresh session … waiting for the user to pick" is reachable except for one user action between sessions, which Claude Code requires. | hooks and skills docs above |
| Is it still current? | Yes on `main` at `61abdfd`. Step 2 is already filed as #261; step 5's content is #191. | #261, #191 |
| Is it the right problem? | Partly. Exit and restart (3–4) can already be one `/clear`. The cause behind step 5 is that there is no fixed "pick up this project" procedure: the session-start rule is written for identifying a project from a free-form request, not for resuming a known one. | `CLAUDE.md:40-53` |

**Verdict:** partly valid. Real problem: after a wrap there is no fixed path into a fresh session on the same project;
the user has to restate the project and get a status whose shape depends on the phrasing. The confirmation in step 2
is #261's; exit and restart can already be `/clear`.

## Options

### Recommended — a fixed pick-up path: `/clear`, then one command the wrap prints

The rollover becomes: `/wrap` → `/clear` → `/pickup <slug>` (name open, D4), the last line copied from the wrap's
output. With #261 fixed, nothing in between asks.

- **What:**
  1. **A core skill `/pickup [<slug>]`**, shipped and synced like `wrap`. With a slug it:
     - runs every `cg` command with `CG_SESSION_ID="$CLAUDE_CODE_SESSION_ID"`, because a `CG_SESSION_ID` left in
       the environment would win over Claude Code's ID and doesn't change on `/clear` (so `set-context` and
       anything that records by session ID, such as #259's worktree record, file under the same new ID). That prefix
       is shell syntax: on Windows it needs Git Bash, which cg's hooks already require; where a command takes
       `--session-id`, passing the flag is the portable form;
     - checks `cg active`: when another session holds the project, it stops and asks before touching any worktree
       (D9), because `cg worktree create` would reuse that session's worktree. That claim may be a leftover rather
       than a parallel session: a wrap whose land failed never releases its claim, and a dirty claim survives
       `/clear` (`internal/session/end.go:9-11`). The skill says so and asks, as the selection note does for stale
       claims (`templates/notes/selecting-next-project.md:46`): rejoin that conversation (`/resume`) and finish the
       wrap, or release the claim;
     - runs `cg session set-context --area <from the README's areas:> --project <slug>`;
     - runs `cg worktree create <slug>` (or, for a paused project, the command `set-context` printed) and works
       there; when `create` exits 2 (unlanded commits: an open PR under the `pr` strategy, or a land that didn't
       complete), it stops and reports that instead of forcing (D10);
     - shows the project's status in one fixed shape (current state, open todo items, recent log and decisions,
       latest commits on the project, `resume.md` when paused);
     - then **stops and waits** for the user's pick. For a paused project, deleting `resume.md` and committing the
       deletion stays as the session-start rule says: after the user confirms work has resumed (`CLAUDE.md:52`).

     Without a slug it lists the pickable projects (the selection note's order) and asks (D8). The list is read from
     the landing branch after a fetch (or with `local`, from the local landing branch), not from the main checkout,
     which can be behind after `/clear` because `pull-latest` runs only on `startup`.
     Because it is a slash command, the gate doesn't fire (`gate.go:18`), so the skill carries the gate's area and
     project steps itself.
  2. **The status shape comes from `cg brief <slug>` (#191)**, an agent-neutral command, so the content is the same
     for every agent and every phrasing; the skill only calls it and presents it (D3). It reads from the new
     worktree, which `cg worktree create` has just synced, so no SessionStart pull is needed after `/clear`.
  3. **The end-mode wrap names the next step.** Just before the completion marker (which must stay last,
     `SKILL.md:383-399`), the summary gets one line: "To continue on `<slug>`: `/clear`, then `/pickup <slug>`" (D6).
     This is how the project carries over: by the printed command, not by stored state.
- **Trade-off:** two typed commands remain (`/clear`, `/pickup …`), against zero hidden state, no race between
  parallel sessions, and nothing that depends on how hook events pair up. It adds one Claude Code file to core and
  makes #191 a dependency.
- **Wrong choice when:** the owner wants the fresh session to start by itself after `/clear` with no second command
  (then the runner-up), or the user restarts for a reason `/clear` doesn't cover (D1). Also weaker under the `pr`
  strategy: until the PR merges there is no landed state to pick up from (D10).
- **Rabbit holes:**
  - Re-describing the session-start gate inside the skill. Keep it to calling `set-context` and `worktree create`;
    the rules stay in `CLAUDE.md`.
  - Open PRs per project: cg doesn't track them (#276). Show what the project's files record; don't query forges.
  - Making the skill also pick the next item. The pick is the user's (the issue says so).

### Alternative — automatic hand-over on `/clear` (runner-up)

- **What:** the wrap records a hand-over (`project`, `area`) when the user asks to continue. SessionEnd (reason
  `clear`) moves it to a workspace-level pending file with a timestamp; SessionStart (matcher `clear`) consumes the
  newest pending file within a few seconds and injects `additionalContext` telling Claude to run the pick-up for that
  project. Flow: `/wrap` (and say "continue") → `/clear`.
- **Trade-off:** one command less, but the old and new session IDs are linked only by timing (the SessionStart input
  carries the new `session_id` and `cwd`, nothing about the old session), so two sessions clearing in the same
  workspace at once can swap hand-overs. SessionEnd hooks have a 1.5 s budget (hooks docs), already shared with the
  claim release (`internal/session/lock.go:22`). It needs a new `settings.json` registration, which `cg sync` won't
  add to existing workspaces (`docs/workspace-sync.md:143-147`).
- **Wrong choice when:** parallel sessions in one workspace are common (they are for the owner), or the workspace
  doesn't use Claude Code (#266 direction).
- **Rabbit holes:** the timing window; a `/clear` meant to drop context, not to hand over, consuming a stale record;
  migrating `settings.json`.
- **Wins instead if:** the owner values the last typed command above determinism, and a key that links the cleared
  session to the new one turns up (Claude Code doesn't document one today). It can be added later on top of the
  recommended skill: the injected context would just say "run `/pickup <slug>`".
- **Ruled out inside Claude Code:** `/clear [name]` takes its argument as a conversation name for the `/resume`
  picker ([commands](https://code.claude.com/docs/en/commands.md)), so a command can't be chained after it.

### Alternative — relaunch through `cg start`

- **What:** the user exits and types `cg start "/pickup <slug>"` (or a short alias) instead of `/clear`; startup
  hooks and a launcher (tmux) run as for any new session.
- **Trade-off:** a fresh process (new binary, settings, launcher), agent-neutral through `session.agent`; but it is
  three actions again (exit, type, start) and whether a slash command works as the initial prompt is unverified.
- **Wrong choice when:** the user has no reason to restart; it then only adds steps.
- **Rabbit holes:** making `cg start` itself parse a project argument (it passes everything to the agent by design,
  `cmd/start.go:26-40`); launcher-specific behaviour (tmux).
- **Wins instead if:** the user restarts on purpose (D1). It composes with the recommendation: same skill, other
  entry point.

### Alternative — do nothing beyond #261 and #191

- **What:** fix #261 (no confirmation) and #191 (`cg brief`), and document "`/clear`, then ask to continue `<slug>`".
- **Trade-off:** no new skill; but step 5 stays phrasing-dependent unless the gate rule points at `cg brief`, and the
  project still isn't named for the user.
- **Wrong choice when:** the handoff happens many times a day (the owner's case): the project is restated and the
  status phrased by hand each time.
- **Rabbit holes:** trying to make a free-form "continue X" deterministic through more `CLAUDE.md` wording, which
  costs every session's context and still depends on being followed.
- **Wins instead if:** the owner prefers no new Claude Code file in core before #266 settles.

## Hidden constraints

- **Reversibility:** a skill and a summary line; removable in one release. No data format changes.
- **Failure mid-operation:** `/pickup` runs after `/clear`, so it can't see whether the wrap completed. Two things
  cover that instead: the next-step line is printed only on a complete wrap (before the marker), and an incomplete
  land leaves the old claim in place (no release ran), which the `cg active` check reports as a possible leftover
  (D9); if the claim is gone, the session branch still has unlanded commits, so `cg worktree create` exits 2 and the
  skill stops (D10).
  The remaining gap is a push that went through but couldn't be verified (`SKILL.md:237`): the branch is removed or
  not, depending on how far the wrap got, so the skill could pick up from a landing branch that lacks the commit.
  The status's "latest commits on the project" line makes that visible; it is not prevented.
- **Parallel sessions:** two sessions on one project would share a worktree and index through `cg worktree create`'s
  reuse, the case the wrap's staging rules guard against (`SKILL.md:227,243`); the `cg active` check (D9) is what
  prevents it.
- **Security:** no new trust surface; the skill runs existing `cg` commands. No `permissions.allow` widening is
  proposed (prefix rules are per subcommand: [permissions](https://code.claude.com/docs/en/permissions.md)).
- **Performance:** `cg worktree create` fetches; the same cost as today's manual flow.
- **Operations:** a new framework `.claude/` file needs an entry in `frameworkClaudeFiles` (`cmd/init.go:572`) and
  `go generate ./cmd` (CONTRIBUTING.md, "Updating templates"); existing workspaces get it on `cg sync`.
- **Windows:** skill text and `cg` commands only; no shell scripts added.
- **Who else is affected:** workspaces without the Claude Code integration get only `cg brief` (#191); the rest is
  Claude Code-specific, like `wrap`.
- **Dependencies:** #191 (hard for the fixed status, D3); #261 (soft: without it, step 2 remains).

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| D1 | Is exiting and restarting (steps 3–4) needed for something `/clear` doesn't give (a launcher such as tmux, a new cg build, startup hooks)? | No: `/clear` is the step between sessions. If yes, add the `cg start "/pickup <slug>"` entry point as well. | open (owner question) |
| D2 | Mechanism: a printed command the user types after `/clear`, or an automatic hand-over on `/clear`. | Printed command (`/pickup <slug>`); automatic hand-over not now. | open |
| D3 | Where the status shape lives. | In `cg brief <slug>` (#191), done first; the skill only calls it. #191 becomes a hard dependency of #275. Pointing the session-start rule's step 4 at `cg brief` stays part of #191, as #191 proposes. | open |
| D4 | The command's name. | `/pickup`. `/continue` reads like Claude Code's `--continue`, which resumes the old conversation, the opposite of a fresh one. | open |
| D5 | Layer: core framework skill, or an extension (after #300 ships). | Core, next to `wrap`, which it pairs with; #266 moves both together. | open |
| D6 | Should the end-mode wrap print the next-step line always, or only when asked to continue? | Always, for the session's project, when it still has open todo items. | open |
| D7 | Scope against #261: fold the worktree-confirmation fix into #275, or leave it to #261. | Leave it to #261 and link the two; #275's flow is complete once both land. | open |
| D8 | Without a slug, should `/pickup` list and ask (#191's "list and pick")? | Yes, from the landing branch after a fetch. | open |
| D9 | When another session's claim is on the project (a parallel session, or a leftover from a wrap that didn't finish). | Stop and ask before any worktree is touched, saying it may be a leftover; offer rejoin-and-wrap or release for a leftover; don't create a second worktree under another slug without the user saying so. | open |
| D10 | Under the `pr` strategy (or any unlanded branch), where `cg worktree create` exits 2. | Stop and report (the open PR, or the incomplete land); never `--force`. Continuing on the PR's branch is the user's call. | open |

## Review

**Review 1** — `specs/gh-275-session-handoff/analysis.md` @ content hash `c6642aa` (uncommitted) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 4 should-fix, 4 nit → fixed: shared worktree when the project is live elsewhere (now D9 and a
  `cg active` check); unbuildable "completion marker" guard (replaced by the printed line plus `create`'s exit 2);
  `pr` strategy not covered (now stated, D10); no-slug list read stale state (now read from the landing branch);
  nits: `gate.go:18`, missing wrong-choice and rabbit holes on two alternatives, `CG_SESSION_ID` precedence (explicit
  `--session-id`), `/clear [name]` can't chain a command. Owner's: none. Rejected: none. Not taken: the reviewer's
  note on SessionStart `initialUserMessage` (not re-verified here; not needed for any option).
- Builder questions: 5 → answered in the text now: area from the README's `areas:`; worktree when live elsewhere
  (D9); `pr` and unverified push (D10, Hidden constraints); `resume.md` deletion stays with the session-start rule;
  the step-4 rule change stays in #191 (D3).
- Verified true: `/clear` not reachable by the model; `CLAUDE_CODE_SESSION_ID` updated on `/clear`; SessionStart
  sources and `additionalContext`; SessionEnd 1.5 s and no decision control; gate skips slash commands;
  `pull-latest` only on `startup`; `cg sync` never edits `settings.json`; `cg worktree create` idempotent and
  fetching; the wrap's release deletes the only project record; `cg start` passes arguments through;
  `frameworkClaudeFiles` and `go generate`; #259 S31 and S35-S37 as described and not relied on; no private context.
- Not verified: whether a slash command as `claude "<prompt>"` runs the skill; whether the Bash cwd survives
  `/clear`; whether `${CLAUDE_SESSION_ID}` in a skill updates after `/clear`.

**Review 2** — same file @ content hash `ee0f6f0` (delta from `c6642aa`) · 2026-11-22
- Reviewers: doc-reviewer (same lenses, delta scope)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 2 nit → fixed: the D9 check would misreport a leftover dirty claim from a wrap
  whose land failed (it survives `/clear`) as a parallel session and hide D10 (now named as a possible leftover with
  rejoin-or-release, D9 and Hidden constraints updated); nits: `/clear [name]` wording; `cg worktree create` could
  record under a stale `CG_SESSION_ID` (now every `cg` call gets `CG_SESSION_ID="$CLAUDE_CODE_SESSION_ID"`).
  Owner's: none. Rejected: none.
- Regression check: all round-1 findings, nits and builder questions resolved.
- Builder questions: 1 (does D9 cover a leftover claim) → answered in the text now.
- Verified true: new citations (`gate.go:18`, `worktree.go:86-99,94-95`, `session.go:286-293,350-355`,
  `SKILL.md:227,237,240,243,263`, README template `areas:`, `CLAUDE.md:52`, `start.go:26-40`); paused projects are
  not live, so D9 doesn't block resuming one.
- Not verified by the reviewer, checked by the author: "unlanded" is `landing..HEAD`
  (`internal/worktree/worktree.go:158`), so squash-merged `pr` branches still count (now stated).

**Review 3** — same file @ content hash `cfce5a0` (delta from `ee0f6f0`) · 2026-11-22 — **clean**
- Reviewers: doc-reviewer (same lenses, delta scope)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 0 should-fix, 1 nit → fixed after the round (text only, not re-reviewed): the
  `CG_SESSION_ID=…` prefix needs Git Bash on Windows (now stated, with `--session-id` as the portable form).
  Owner's: none. Rejected: none.
- Regression check: all round-2 findings resolved; a dirty flag is never cleared (`internal/session/markdirty.go:31`),
  so a leftover claim after a failed land does survive `/clear`.
- Builder questions: none.
- Verified true: squash-merge and `remove --force` behaviour (`internal/worktree/remove.go:60,96-99`,
  `worktree.go:158,193,204-209`); `cg active` excludes by the same resolved ID (`cmd/active.go:76`); the Review 2
  record matches its round.
- Not verified: whether `/resume` into the old conversation restores its session ID (relevant to "rejoin and finish
  the wrap" in D9).

Reviews cover content hash `cfce5a0` plus a one-clause wording fix; not verified: a slash command as the initial
prompt of `claude "<prompt>"`, the Bash cwd after `/clear`, `${CLAUDE_SESSION_ID}` after `/clear`, and the session
ID after `/resume`.

## Verdict

**needs clarification:** the review is clean (round 3), but the recommendation rests on D1 (that `/clear` covers
what the user restarts for), which only the owner can answer and which would add the relaunch entry point if
answered "no"; D2–D10 are open for the owner's decision.

## Q→A log

- (none yet)
