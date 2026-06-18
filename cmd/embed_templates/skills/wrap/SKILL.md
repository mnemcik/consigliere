---
name: wrap
description: >-
  Session wrap-up — review the conversation, persist all findings and decisions to the
  knowledge base, update project files, capture notes and insights, and commit everything.
  Two modes: end-of-session (default — lands work to main, removes worktree, session is closeable)
  and pause (mid-work — captures resume.md, WIP-commits to the session branch, keeps worktree alive).
  Triggers on: "/wrap", "wrap up", "save session", "end session", "persist everything", "capture session",
  "before I close" (end mode); "/wrap pause", "pause this", "pause the session", "pausing for now",
  "save state", "save my place" (pause mode).
user_invocable: true
---

# Wrap — Session Wrap-Up

You are performing a session wrap-up. Your job is to ensure **nothing valuable from this session is lost** — every finding, decision, status change, and observation gets persisted to the right place in the knowledge base before the session closes (end mode) or pauses (pause mode).

## Why this exists

The user runs a personal knowledge base in `~/source/personal-workspace/` that tracks projects, areas, ideas, notes, and insights. During a session, valuable context accumulates in the conversation — decisions made, things learned, status changes, new tasks discovered — but **none of it persists unless explicitly written to files**. If the user closes the terminal without wrapping up, that context is gone forever. This skill is the safety net.

## Mode

The skill has two modes. Most phases are identical; only Phase 6.5 (new), Phase 7, Phase 8, and Phase 9 diverge.

| Mode | When | What's different |
|------|------|-----------------|
| **end** *(default)* | Session is genuinely finished — work landed, todo step closed, ready to close terminal. | Phase 7 lands the commit to `main` via the workspace's landing wrapper. Phase 8 removes the worktree unconditionally. Phase 9 marker is `✅ Wrap complete — session can be closed.` and clears the dirty flag. |
| **pause** | Session must stop mid-work — half-edited files, in-flight todo step, mental context that won't survive in the codebase but matters for picking up later. | **Phase 6.5 (new)** writes `projects/<slug>/resume.md` capturing the cursor (active todo, files, mental context, next action, blockers). Phase 7 makes a `WIP pause: <one-line>` commit and pushes the **branch** to `origin/session/<slug>` — does **not** land to `main`. Phase 8 **skips** worktree removal — worktree + branch survive. Phase 9 marker is `⏸️ Session paused — resume by re-entering project (worktree at <path>) or via /resume <slug>.` and **leaves the dirty flag set**. |

### Detecting mode

- `pause` mode is selected when the user invokes `/wrap pause` or uses pause phrasing ("pause this", "pause the session", "pausing for now", "save state", "save my place").
- Otherwise `end` mode is the default.
- **If genuinely ambiguous** (uncommitted in-flight changes plus no explicit signal either way), ask once: *"End-of-session wrap or pause for later resumption?"* Do not auto-pick.

## What you must do

Work through each phase below. Be thorough but concise — the user wants completeness, not essays. If a phase has nothing to capture, say so briefly and move on.

---

### Phase 1: Identify the active project(s)

Determine which project(s) were worked on during this session:

1. Read `projects/TODO.md` to see the project index
2. Look through the conversation history to identify which project(s) were touched
3. For each active project, read its current `README.md`, `todo.md`, `decisions.md`, and `log.md`

If no project was active (e.g., the session was exploratory or off-project), note that and proceed — phases 2-4 still apply.

---

### Phase 2: Update project files

For **each** active project, check whether any of these files need updating. Read the current file content first, then make only the changes warranted by this session's work.

#### README.md
Update if any of these changed during the session:
- Project status (e.g., `defining` → `in-progress`)
- Scope (new items in/out of scope)
- Stakeholders (new people involved)
- Dependencies (new blockers or resolved ones)
- Current status note (if the bottom-of-file summary is now stale)

**Do NOT update README.md just to say "session happened."** Only update if substantive project metadata changed.

#### todo.md
- Check off items that were completed during this session
- Add new action items that were discovered or discussed
- Reorder if priorities shifted

This is often the most important file to update — it's the "what's next" for the project.

#### decisions.md
Append any decisions made during the session. Use the project's existing format (check the file). Each decision should include:
- What was decided
- Why (the reasoning or constraint that drove it)
- Status: `active`
- Date

If a previous decision was superseded, update its status to `superseded` and add a `Superseded by:` reference.

**Only log actual decisions**, not observations or findings (those go in notes).

#### log.md
Add a dated entry summarizing what happened in this session. Format:
```
## YYYY-MM-DD

- [Bullet points of what was done, learned, or decided]
- Keep it brief — 3-7 bullets is typical
- Include specific details (file names, rule IDs, tool names) so future sessions can pick up context
```

**If an entry for today's date already exists** (from earlier in the session or a previous wrap), extend it with additional bullets rather than creating a duplicate dated heading.

This entry should give a future session (or the user) a quick picture of what happened today.

#### references.md
If any new external links came up (Confluence pages, Slack threads, GitHub PRs, documentation URLs), add them here. Create the file from `templates/project/references.md` if it doesn't exist yet.

---

### Phase 3: Capture notes

Review the session for **technical findings, gotchas, workflow learnings, or reference material** that would be useful beyond this specific project. These go in `notes/`.

The key test: **would this help someone working on a _different_ project in the same domain?** If yes, it's a note. If it only matters for this project, it belongs in the project's `log.md` instead.

Examples of note-worthy findings:
- A tool behaves unexpectedly (e.g., "GitVersion ignores pre-release tags on detached HEAD")
- A workaround was needed for a platform limitation
- A workflow pattern was discovered that applies across projects
- A reference (API docs, config schema, CLI flags) was hard to find and worth bookmarking

Examples of things that are NOT notes (they go in project files):
- "We decided to use approach X for this project" → `decisions.md`
- "Completed the API review" → `log.md`
- "Need to follow up with Morgan" → `todo.md`

For each finding worth capturing:

1. Check if an existing note in `notes/` already covers this topic (read `notes/INDEX.md`)
2. If yes — extend the existing note with the new finding
3. If no — create a new note using `templates/note.md`, categorize it (`tool-gotchas`, `workflow`, `architecture`, `process`, `research`, `reference`, `troubleshooting`), and add it to `notes/INDEX.md`

**Skip this phase if** the session produced only project-specific work with no broadly reusable findings.

---

### Phase 4: Capture insights (drafts only)

Review the session for **observations about how the user prefers to work with Claude** — communication preferences, decision-making patterns, collaboration style, prompting patterns.

1. Read `insights/INDEX.md` to check for existing insights on the same theme
2. If the observation is already captured, skip or add new evidence to the existing file
3. If it's new, create a draft insight in `insights/YYYY-MM-DD/` using `templates/insight.md`
4. Add a row to `insights/INDEX.md`

**CRITICAL: Insights are always drafts. Never apply them as rules.**

**Skip this phase if** the session was too short or mechanical to reveal anything about work style.

---

### Phase 5: Update area files and propagate information

Area files (`areas/`) are the reference hubs for the entire knowledge base. Every future session that touches a domain reads the area file first — stale area files mean stale starting context. Treat area updates with the same importance as project file updates.

#### Area file updates

1. **Identify affected areas.** Look at the project's `Areas:` field and any domains discussed during the session. Read `areas/INDEX.md` if needed to find the right area files.
2. **Read each affected area file.** Check these sections against what was learned in the session:

| Section | Update when... |
|---------|---------------|
| **Overview** | Scope or purpose of the system/practice changed |
| **Architecture & Constraints** | New technical constraints, patterns, or limitations were discovered (e.g., "APIM uses header-based versioning") |
| **Current State** | Deployment status, versions, migration state, or health changed |
| **Key Contacts** | New people became relevant or roles shifted |
| **Associated Items** | New projects, ideas, or notes now reference this area |
| **Conventions** | New repo conventions, naming patterns, or workflow rules were established |

3. **Update the area file** with the new information. Be specific and factual — area files are reference material, not narrative.
4. **Bump the `Last reviewed` date** to today on every area file you update.

#### Other propagation

- **Ideas** — If an idea's status changed (e.g., was explored, got parked, or is ready for promotion), update `ideas/BACKLOG.md` and the idea file.
- **Cross-project impact** — If a finding in one project affects another project's scope, dependencies, or timeline, update that project's files too.

**Skip this phase if** the session was purely mechanical (file renames, formatting) with no new information about any domain.

---

### Phase 6: Continuous improvement check

Briefly review the session for improvement opportunities to the knowledge base system itself:

- Rules that were violated or hard to follow
- Missing rules that would have prevented a mistake
- Stale or contradictory rules
- Template gaps
- Workflow friction

If you spot something:
- **Small obvious fixes** (typos, broken links, stale dates): fix directly
- **Substantive improvements**: mention them to the user with a one-line proposal. Don't implement without confirmation.

---

### Phase 6.5: Capture resume.md (pause mode only)

**Skip this phase entirely in end mode.** It exists only to preserve the in-flight cursor when the user is pausing mid-work.

In pause mode, write `projects/<slug>/resume.md` from the workspace's `templates/project/resume.md`. This file is the **single artifact that survives the session** — when the user comes back, it is the authoritative pickup context. Everything that's in your head right now and isn't already in code or in `log.md` must land here.

Sections (the template enforces them):

1. **Cursor** — Active todo item (copy the in-flight `todo.md` line if there is one), current file(s) being edited, and the *step inside the step* — what was happening **inside** the todo item ("wrote the regex but haven't tested IPv6", "stub of `applyMigration` written, no tests yet").
2. **Mental context** — The thoughts/assumptions/half-formed hypothesis that won't survive in the codebase. *Why* the current approach was chosen, *what* alternative was rejected, *which* edge case was on your mind.
3. **Dirty state** — Branch (`session/<slug>`), WIP commit SHA (filled after Phase 7 commits — leave a `<TBD>` placeholder for now and overwrite after the commit), pushed-to-origin (yes/no), uncommitted files at pause time.
4. **Next concrete action** — A *single specific step*. Not a goal ("make tests pass"); an action ("run `bun test integration/auth.test.ts` and read the failure for `accepts expired refresh tokens`").
5. **Open blockers / questions** — Anything stopping progress, or anything the user needs to think about away from the keyboard.
6. **Pause notes** — Anything else that doesn't fit. Keep brief.

**If a `resume.md` already exists for this project** (pause-on-paused-session), overwrite it. The latest cursor wins. Old context that still matters should already be in `log.md` or `notes/`.

After Phase 7 commits, return to this file and replace the `<TBD>` SHA with the actual WIP commit SHA. The resume side reads this to know what was committed.

---

### Phase 7: Commit and push

After all updates are written, commit in each repo you touched this session (the personal-workspace, plus any external repos).

**Commit scope: this session's changes only.** Stage only files this session created or modified. Never sweep up pre-existing uncommitted changes — their owner is someone else (a parallel worktree, an earlier session in the same chat, or the user's own local edits), and their state is unknown.

1. **Verify you are in a worktree if the workspace requires it.** Before any staging, run `git rev-parse --show-toplevel` and check the workspace's `CLAUDE.md` under Git Workflow → Worktrees for Parallel Sessions (or equivalent). If the workspace mandates per-session worktrees and your cwd is the main workspace root, **stop and surface this to the user before committing** — the workspace likely ships a wrapper (e.g., `scripts/create-session-worktree.sh <slug>`) that does the right thing idempotently. The steps below (stage-by-name, `git status` check) prevent self-inflicted scope leaks, but they cannot protect you from a concurrent session's `git add` in the shared index — only worktree isolation can.
2. **Track your edits as you go.** Keep an explicit list of the paths you wrote to this session. Stage those paths by name.
3. **Run `git status` before staging.** If the working tree shows modified or untracked files you did *not* touch this session, do not include them. If unsure, list the extras to the user and ask before staging.
4. **Stage by name only.** Use `git add <path1> <path2> ...`. **Never `git add -A`, `git add .`, or `git add -u`** — those sweep up work you did not author this session.
5. **Verify the staged set.** Run `git status --short` (or `git diff --cached --stat`) and confirm the list matches what you intended to stage, nothing more. If extras appeared between your staging command and this check, a parallel session probably staged them into the shared index — abort, unstage them, and revisit step 1.
6. **Commit** with a clear message:
   - **End mode:** summarize what was captured (e.g., `Wrap session: update versioning-guidelines log, add APIM header versioning todo`).
   - **Pause mode:** prefix with `WIP pause:` followed by a one-line cursor (e.g., `WIP pause: half-finished IPv6 regex in apim-ip-logging-adr — next: test IPv6 case`). After the commit lands locally, **return to `resume.md` and replace the `<TBD>` Dirty-state SHA with the actual commit SHA**.
   - If the working tree was clean (no edits since the last commit) **and you are in pause mode**, skip the commit step. Note in `resume.md` Dirty state: `WIP commit SHA: none — worktree clean before pause`.
7. **Push.**
   - **End mode:** use the workspace's landing wrapper (`scripts/land-worktree-commit.sh` in `personal-workspace`). It lands the branch to `main` via fetch + push + rebase-on-non-ff + assertion in one atomic lifecycle. If the workspace ships no wrapper, fall back to the repo's default: direct push to `main` for direct-to-main workspaces, branch-and-PR for external repos that require review.
   - **Pause mode:** push the **branch** to its tracking remote — `git push origin session/<slug>`. Do **not** land to `main`. The branch acts as a remote backup so the WIP survives disk loss. If there is nothing to push (nothing committed in step 6), skip.
8. **Verify the commit reaches the right ref.**
   - **End mode:** `git merge-base --is-ancestor <wrap-sha> origin/main`. This assertion is a trust-boundary check between the wrap skill and whatever performed the push — tautological under the `personal-workspace` ephemeral-branch-worktree model (where push inherently carries the branch to `main`) but kept as defense-in-depth because the cost is one `merge-base` call and it catches entire classes of silent-failure: a push-command that returned 0 but actually no-op'd (e.g., refs/main pinned by a hook), a landing wrapper that crashed mid-push and left `origin/main` stale, or a user-side mistake that took the session out of the worktree before pushing. If the assertion fails, do **not** report the wrap as green; surface the divergent SHAs to the user and point at the workspace's landing wrapper (e.g., `scripts/land-worktree-commit.sh`) as the recovery path.
   - **Pause mode:** `git merge-base --is-ancestor <wip-sha> origin/session/<slug>`. Same defense-in-depth — confirms the branch backup actually reached origin. Skip if nothing was committed in step 6.

These rules exist because of a sequence of incidents. On 2026-04-20 an earlier version of this skill bundled four unrelated in-flight files into a wrap commit (`030c8a0`), pushing mid-work state from a parallel session straight to `main`; a follow-up the same day (`6c15665`) showed that even a correctly scoped `git add <path>` leaks parallel-session work when the index is shared — the fix was per-session worktrees plus the pre-commit staged-set check (step 5). Between 2026-04-22 and 2026-04-23, three further failures (`c034b55`, `0590d30`, `5593a6a`) hit the detached-HEAD worktree model: wrap commits advanced only the detached HEAD and never fast-forwarded `main`, and cherry-pick-only landing scripts silently orphaned chain ancestors; step 8 and the landing script were the incremental mitigations. On 2026-04-24 the `personal-workspace` repo replaced the detached-HEAD model with ephemeral `session/<slug>` branches per worktree, making orphans structurally impossible and shrinking the landing script accordingly — see that repo's `projects/ephemeral-branch-worktrees/` and `notes/worktree-wrap-commit-landing.md` (historical). The authoritative current rules live in the workspace's `CLAUDE.md` → Git Workflow → "Commit Scope — This Session Only", "Worktrees for Parallel Sessions", and "External Repos — Parallel Sessions".

---

### Phase 8: Cleanup check

Sessions accumulate transient artifacts — worktrees for projects that are now complete, local branches whose PRs have merged, scratch directories, background processes. Without a prompt to look for them, they build up silently over weeks until `git worktree list` is a forest and `git branch` is a junkyard. This phase is the prompt.

Run the checks below. Only act on items that apply this session; skip the rest without narration. **Every destructive action requires explicit user confirmation** — list what you would remove, then ask.

**Pause mode skips the worktree subsection entirely** — the worktree is the resume vehicle and must survive. The other subsections (merged branches on external repos, scratch files, background processes) still run in pause mode.

#### Worktrees (end mode only)

If this session worked in a git worktree (cwd is outside the repo's main working tree — verify with `git rev-parse --show-toplevel` against the workspace's canonical path), removal is **unconditional**, regardless of the project's status. Project status describes work state ("am I done with this initiative?") and is independent of session activity ("am I editing right now?"); conflating them caused stale worktrees to accumulate for `in-progress` projects between focused sessions. Recreation on the next session that touches the same project is the workspace's Session-Start Rule's job (worktree-creation wrappers are idempotent), so removal does not block continued work.

Procedure:

1. `cd` out of the worktree first — never run `git worktree remove` (or its wrapper) from inside it.
2. Offer the removal command to the user, then run it on confirmation:
   - If the workspace ships a removal wrapper (e.g., `scripts/remove-session-worktree.sh <slug>` in `personal-workspace`), use it — the wrapper atomically removes the worktree and deletes the associated `session/<slug>` branch, and refuses if the branch has unlanded commits. Phase 7 has already enforced a commit, so the unforced path is the norm.
   - Otherwise fall back to `git worktree remove <path>` followed by the appropriate branch cleanup (e.g., `git branch -d session/<slug>`).
3. If the user explicitly opts out (mid-feature pause without commit, deliberate uncommitted scratch state, etc.), leave the worktree in place. The default is removal; opt-out requires a stated reason.

#### Merged branches on external repos

For each external repo touched this session (not the personal-workspace, which commits directly to `main`):

1. `git fetch --prune origin` to sync remote state and drop deleted remote refs.
2. For the branch this session pushed, check PR state: `gh pr view <branch> --json state,mergedAt`.
3. If the PR was merged:
   - Switch off the branch first (`git switch main` or the repo's default).
   - Offer to delete the local branch. Try `git branch -d <branch>` first; if git reports the branch as unmerged (common for squash-merges), confirm with the user before falling back to `-D`.
4. Do not delete branches with open, closed-without-merge, or unknown PR state.

#### Scratch files and temp directories

Look for transient artifacts created this session that were never meant to persist:

- Gitignored working directories (`.secrets/`, `scratch/`, `tmp/`)
- One-off script output, cached API responses, exported credentials

List any found, with their purpose. Offer to remove only the ones clearly transient. Leave anything that might still be useful — the user can clean up manually.

#### Background processes

If any long-running background processes were started during this session (dev servers, file watchers, `run_in_background` bash commands) and are still running:

- List them so the user knows what will continue after the session closes.
- **Do not auto-kill.** The user may want them to keep running; that's their call, not the skill's.

#### When to skip this phase entirely

Skip with a one-line note if the session had no worktree, no external-repo branch, no scratch artifacts, and no background processes — which covers most short sessions.

---

### Phase 9: Summary

Present a brief summary to the user. Sections shown are mode-dependent.

**End mode:**

```
## Session wrapped

**Project files updated:**
- [project-name] — log.md (session entry), todo.md (2 items added, 1 checked off)

**Notes captured:**
- Updated notes/azure-apim-gotchas.md with header versioning finding

**Insights:** None this session

**Propagation:** Updated areas/api-management.md with header versioning detail

**Committed:** abc1234 — pushed to main

**Cleanup:** Removed worktree personal-workspace--old-project (project done); deleted local branch feat/xyz (PR #42 merged)
```

**Pause mode:**

```
## Session paused

**Project files updated:**
- [project-name] — log.md (session entry), todo.md (1 item added)

**Resume context captured:** projects/[project-name]/resume.md
- Cursor: <one-line summary of where work stopped>
- Next action: <one-line summary of next step>

**WIP commit:** abc1234 — pushed to origin/session/<slug> (branch backup; not landed to main)

**Worktree:** kept alive at /Users/.../personal-workspace--<slug>
```

Keep it scannable. The user should be able to glance at this and confirm nothing was missed.

#### Session-dirty flag

If the workspace maintains a per-session "dirty" indicator (check for `.claude/session-context/<session_id>.json` at the workspace root, and a `dirty` field in the schema — see the workspace's `CLAUDE.md`):

- **End mode:** clear the flag — the session is safe to close.

  ```sh
  ctx=".claude/session-context/<session_id>.json"
  [ -r "$ctx" ] && tmp=$(mktemp) && jq '. + {dirty: false}' "$ctx" > "$tmp" && mv "$tmp" "$ctx"
  ```

- **Pause mode:** **leave the flag set.** Paused state is dirty by design — the status-line should still indicate work-in-progress. Skip the `jq` step entirely.

Skip silently if the workspace doesn't use a dirty flag or the context file doesn't exist.

#### Terminal completion marker

After the summary block (and after handling the dirty flag if applicable), print one final line that is unambiguously the end of the wrap. The marker is mode-specific:

- **End mode:**

  ```
  ✅ Wrap complete — session can be closed.
  ```

- **Pause mode:**

  ```
  ⏸️ Session paused — resume by re-entering project (worktree at <path>) or via /resume <slug>.
  ```

This line is the signal that every phase ran to completion. It must be the **last** thing the skill outputs — if it is missing, the user knows the skill exited early and something still needs attention. Do not add anything after it.

---

## Important behaviors

- **Be thorough, not verbose.** Check every phase, but don't pad empty phases with filler text.
- **Read before writing.** Always read the current state of a file before updating it. Don't write based on assumptions about what's in the file.
- **Don't duplicate.** If something is already captured (e.g., from earlier in the session), don't re-capture it.
- **Preserve existing content.** When updating files like `todo.md` or `log.md`, append — don't overwrite or reorganize existing content.
- **Stage only what you touched this session.** A file "looking ready" is not authorisation to commit it — its owner is someone else. See Phase 7 for the full rule.
- **Verify worktree isolation before committing.** If the workspace requires per-session worktrees (check its `CLAUDE.md`) and you are in the main workspace, stop before staging — a shared index can leak parallel-session work into your commit regardless of how carefully you stage by name.
- **Ask if uncertain.** If you're not sure whether a finding is worth capturing or where it belongs, ask the user rather than guessing.
- **Trivial sessions are OK.** If the session was a quick lookup or a single file rename, say "Nothing to capture — session was trivial" and stop. Don't manufacture output.
- **Pause is not the default.** Pause mode requires an explicit signal (`/wrap pause`, pause phrasing, or an unambiguous resolution to the ambiguity prompt). When the signal is absent, treat the wrap as end-of-session — the historical default. Pause is opt-in because it leaves the worktree alive and the dirty flag set; users who don't ask for that get standard end-of-session behavior.
- **Pause from the main workspace is invalid.** Pause mode requires a worktree on a `session/<slug>` branch — that's the resume vehicle. If the wrap is invoked from the main workspace in pause mode, refuse: surface the issue and tell the user to re-issue from inside a worktree (the workspace's `scripts/create-session-worktree.sh <slug>` is the wrapper).
