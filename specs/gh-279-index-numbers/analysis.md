---
title: "Analysis — parallel sessions allocate the same index number (#279)"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/279"
size: full
verdict: proceed  # proceed | needs clarification | don't proceed
---

# Analysis — parallel sessions allocate the same index number (#279)

- **Evidence base:** `origin/main` at `61abdfd` (2026-11-21). git 2.54.0 for the experiment below.
- **Status:** analysis only; nothing implemented. Decisions are the owner's.

## Question

`cg init` creates two index tables with a `#` column: `projects/TODO.md` and `ideas/BACKLOG.md`. Nothing allocates
the number, so every session takes "highest + 1" from the checkout it sees. With several sessions running in parallel
worktrees, two of them take the same number, and the clash shows up only when the second one lands: a rebase conflict
on the index, then a hand renumber. It affects every workspace that runs parallel sessions, which is now the normal
way of working.

This is a **"how" item** with a "whether" inside it: before choosing *how* to allocate numbers, settle *whether* the
index needs sequential numbers at all.

## Current state (evidence)

**What cg creates.** The project index has `| # | Project | Status | Areas | Folder |` in both of its tables
(`cmd/init.go:694-707`); the idea backlog has `| # | Idea | Status | Areas | Tags | One-liner |` (`cmd/init.go:721-731`).
The areas index, by contrast, has no `#` column and identifies a row by its slug (`cmd/init.go:709-719`). The notes and
insights indexes have no number either.

**What the framework says the number means: nothing.** No template, note, skill or doc defines the `#` column or says
how to fill it. The steps that add rows just say "add the project to `projects/TODO.md`" (`templates/notes/project-structure.md:64`)
and "add it to `ideas/BACKLOG.md` with status `raw`" (`templates/notes/idea-to-project-workflow.md:15`); the same
holds for `templates/notes/capturing-improvement-proposals.md:58-59` and `templates/skills/wrap/SKILL.md:173`. A
search of `templates/`, `cmd/`, `internal/`, `docs/` and `README.md` for wording about numbering, renumbering or the
`#` column found nothing. The identity of an item is its file: the idea file and the project folder carry no number
in their frontmatter (`templates/idea.md:1-8`, `templates/project/README.md:1-9`).

**What the framework says rank is: row order.** "Row order within each priority bucket is the rank"
(`templates/notes/selecting-next-project.md:15`), and rank is user-owned (`:22`, `templates/workspace/CLAUDE.md:312`).
A number column is not needed for rank.

**What reads the column: nothing.** cg's only index parser, `parseProjectIndex` (`cmd/match.go:174-241`), takes the
name, status, areas and folder from cells 1–4 and looks at cell 0 only to detect the separator row (`:215`, `:221-226`).
It is used by `cg match` (`cmd/match.go:63`), `cg status` (`cmd/status.go:87`) and `cg share` (`cmd/share.go:302`,
`:511`). It finds columns **by position** and skips every row with fewer than 5 cells (`cmd/match.go:203-205`), so a
4-column table without `#` parses to **zero projects, silently**: `cg status` says `Projects: 0`, `cg match` finds
nothing, `cg share` reports the project missing from the index. That is the failure #270 describes. Removing the
column therefore needs a parser change first, and breaks under any cg binary older than that change. Nothing in cg parses `ideas/BACKLOG.md` (`cg status` counts idea files, `cmd/status.go:52`).
The owner's extension repos have no reader either: `git grep` for `BACKLOG.md` and `projects/TODO.md` in
mnemcik/cg-extensions (`87c51e5`) and the owner's plugin marketplace repo finds nothing.

**Index files belong to the workspace after `cg init`.** They are written only when missing (`cmd/init.go:131-143`,
`writeFileIfNotExists` at `:669-681`); `cg sync` does not touch them, and `cg migrate` treats them as curated tables,
not items (`internal/migrate/migrate.go:24-27`). A change to the templates therefore reaches new workspaces only.

**How sessions land.** `cg worktree land` rebases the session branch onto the landing branch and stops on a conflict
with `ExitConflict` (`internal/worktree/land.go:60-71`, `:232-239`, `:473-480`). Only the `local` strategy serialises
lands with a lock in the shared git dir (`internal/worktree/landlock.go:14-23`); `direct-to-main` relies on the
remote's non-fast-forward rejection, and `pr` doesn't rebase at all: there the same insert conflict shows up when
the second PR is merged or updated from `main`, on the forge instead of at `cg worktree land`.

**"Highest + 1" can't be right even for a careful session.** A project created in a session that was then paused
exists only on its unlanded branch, not in `TODO.md` on the landing branch (`templates/workspace/CLAUDE.md:50`). A
session that reads "highest" from the landing branch cannot see it, nor rows on other sessions' branches.

**Experiment: where the conflict really comes from.** A scratch repo with a `TODO.md` shaped like the cg template;
branch A and `main` each insert one row after the same existing row; A is then rebased onto `main` (script kept
outside the repo; reproducible from this description):

| Case | Result |
|---|---|
| Both rows take `#` 2 | conflict |
| Neither row has a number (`—`) | **conflict** |
| `T.md merge=union` in `.gitattributes`, both take 2 | clean, both rows kept, **both numbered 2** |
| `merge=union`, both sides change the same row's status | clean, **the row duplicated** with both statuses |

So the rebase conflict comes from two insertions at the same place, **not from the number**. The number adds a
second, worse cost: after keeping both rows, one of them must be renumbered, and with rank numbering everything below
it too.

**One workspace's usage** (the reporter's; counts only). It has 92 project rows and 57 idea rows. Its project table
uses `#` as the **rank position** (its header note says to renumber after any move), so any insertion or reorder
rewrites every row below. Its idea table uses `#` as an **identifier**: idea numbers are cited 8 times in project
logs, decisions, a todo list and an area file ("idea #9"), and one number is held by two rows today. So the same
column means two different things in the two tables, and neither meaning comes from the framework. No citation of a
project number ("project #N", "rank #N") was found outside the index.

**Recon checked:**
- this repo (`cmd/`, `internal/`, `templates/`, `docs/`, `README.md`, `CONTRIBUTING.md`): as above;
- the owner's extension and plugin repos (mnemcik/cg-extensions and the owner's plugin marketplace): no index readers;
- the installed plugins and skills of this session: none allocates IDs or touches cg indexes;
- one real workspace (read-only, counts only): as above;
- external: git's built-in `union` merge driver (gitattributes(5)), tested above; ID schemes that need no
  coordination (slugs, date or random IDs) are considered under Options. Assumption (med): no established tool
  allocates sequential numbers across git worktrees without a shared lock or server; none was found, but no
  dedicated search was made.
- in-flight work: #259 / PR #303 (`gh-259-extension-commands`) changes `cmd/external.go`, `cmd/start.go`,
  `cmd/worktree.go`, `internal/session/context.go`, `internal/worktree/worktree.go`, `internal/workspace/workspace.go`
  and others, but not `cmd/match.go`, `cmd/init.go` or `templates/` (`git diff --stat origin/main...origin/gh-259-extension-commands`).
  #300 and #301 are analyses of extension skills and extension hook filters, with only `specs/` on their branches.
  None overlaps the recommendation; see Hidden constraints for the one alternative that would.
- related open issues (`gh issue list`, 2026-11-22): **#270** (a mis-formatted project index silently parses to zero
  projects; asks for documented required columns and a warning) touches the same parser; **#229** (shipped content
  names the default index paths although `.cg.json` `indexes` can change them) touches the same notes. Neither
  covers numbering; no open issue or PR duplicates #279.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes. Two sessions that each add a row take the same number, because nothing allocates it and no session can see unlanded rows. | `cmd/init.go:694-731`; no allocation rule anywhere; `templates/workspace/CLAUDE.md:50`; one number held twice in the reporter's workspace |
| Is the expected behaviour right? | Partly. The issue assumes the number must stay and be allocated safely. The framework never defined the number, nothing reads it, and rank is row order. | `cmd/match.go:215-226`; `templates/notes/selecting-next-project.md:15` |
| Is it still current? | Yes, on `main` at `61abdfd`; no open PR or issue covers it. | `gh` issue and PR lists, 2026-11-22 |
| Is it the right problem? | Partly. The number causes the renumbering and the duplicate IDs; the rebase conflict itself comes from parallel inserts and stays whatever is done with the number. | Experiment above |

**Verdict: partly valid.** The real problem: the index carries an undefined `#` column that sessions treat as an
allocated identifier (ideas) or a rank position (projects), neither of which can be kept consistent across parallel
sessions without coordination. The two tables are independent in code (nothing parses the idea backlog), so they can
get different answers. Parallel row inserts also conflict on rebase, but that conflict is mechanical (keep
both rows) once no number has to be fixed up.

## Options

The options answer two questions, one per table, because the evidence differs: in the project index `#` duplicates
row order (rank), while in the idea backlog it serves as a short identifier that records cite.

### Recommended — the slug is the identity; stop filling `#` (column kept for now)

- **What:**
  0. **Where it lives.** One every-session line in the `conventions` `cg:section` of `templates/workspace/CLAUDE.md`,
     next to the rank rule (`:306-318`, `:312`), because `/wrap` (`templates/skills/wrap/SKILL.md:47`, `:173`) and
     several notes edit index rows (`information-propagation.md:28`, `capturing-improvement-proposals.md:58-59`,
     `promoting-a-project-to-an-area.md:37`) without loading the two notes below. The line says: rows are identified
     by slug, new rows leave `#` empty, never renumber, unless the index's own note asks for numbering or
     renumbering: then follow the note, and unless the index carries the marker
     `<!-- cg: index numbering kept deliberately -->`, load `notes/project-structure.md` (which says to tell the
     owner). The marker only silences the notice. The
     detail (items 1 and 2) lives in that note, with a pointer from `notes/idea-to-project-workflow.md`, as
     CONTRIBUTING.md's impact-review rule asks (every-session rules short, trigger-only detail in a note).
  1. **Rule, in the framework notes** (`notes/project-structure.md`, `notes/idea-to-project-workflow.md`): an item is
     identified by its slug (project folder, idea file) and referred to by slug or link, never by a row number. Rank
     is row order. New rows leave `#` empty. Existing idea numbers are never renumbered (records cite them). Existing
     project numbers are rank positions; once rows move they go stale and could be misread as rank, so if nothing in
     the workspace's records cites them, the owner may clear them (decision 1a). A session doesn't edit an index's
     own header note (for example one asking for renumbering) by itself: it proposes the change to the owner, since
     indexes are curated. Until the owner changes it, the workspace's own note wins over the framework rule
     (extending the framework's precedent that a workspace's own content wins over framework content,
     `templates/commands/cg-sync.md:56`). The conflict is surfaced by what a session can see: a session about to add or move a
     row in an index whose own note (the text above its tables) asks for numbering or renumbering follows the note
     and tells the owner once, in that session, that it conflicts with the framework rule, unless the index carries the
     marker `<!-- cg: index numbering kept deliberately -->`. The owner either drops the note or adds that marker; either way the notice
     stops, with no state kept anywhere. This works whatever path brought the rule (`/cg-sync`, a plain
     `cg sync --apply`, a merged drifted note). So in a workspace whose index note asks for renumbering, the
     reporter's included, "no new numbers" takes effect for that index only once the owner drops that note
     (decision 4).
  2. **Conflict line**, same notes: when a rebase or PR merge conflicts on an index because both sides added rows,
     keep both rows (in rank order for projects). With no number to fix up, that resolution is always right.
  3. **Index templates, commands and parser unchanged.** `cg init` keeps the `#` header in both indexes; the parser keeps reading by
     position. Nothing breaks for any binary, old or new, and no index file changes on `cg sync`.
  4. Applies to **both** tables (decision 1a and 1b).
- **Trade-off:** a vestigial column in every index, including fresh ones, that the framework says to leave empty. Loses
  the short numeric handle for new ideas ("idea #9"); slugs are longer but unique by construction (two items can't
  share a file or folder), greppable, and the same in every checkout. The insert conflict remains, but resolves as
  keep-both with nothing to renumber.
- **Wrong choice when:** the owner wants short numbers for ideas (then the allocator for the idea backlog, 1b), or a
  clean header in new workspaces matters enough to take the parser change now (then the next option).
- **Rabbit holes:** don't migrate existing indexes (curated, user-owned files whose numbers records cite); don't fix
  the "priority sections" wording in `selecting-next-project.md:15`, which describes sections the template doesn't
  create (a separate, pre-existing mismatch); new note text should say "the project index" and "the idea backlog"
  rather than add hard-coded paths (#229).

### Alternative — drop the `#` column from new workspaces too (two releases)

- **What:** the recommended rule, plus: (release 1) `parseProjectIndex` finds columns by header name (Project, Status,
  Areas, Folder; case-insensitive, no aliases), falls back to today's positional reading when those names are absent
  and the row has 5+ cells, and **warns** instead of returning zero rows when neither works (the warning is #270's
  ask, so coordinate with it); (release 2, once release 1 is out) `cg init` creates both indexes without `#`, as
  `areas/INDEX.md` already is. Existing workspaces drop the column only after they run release 1.
- **Trade-off:** clean tables for new workspaces, at the cost of a parser change, tests for both shapes (and CRLF), two
  releases, and a window where a new-shape index read by an old binary silently shows zero projects. `cg`'s
  background auto-update (`internal/autoupdate`) narrows that window when enabled; it can be disabled
  (`CONSIGLIERE_AUTO_UPDATE=0`, `--no-auto-update`), so it is not a guarantee.
- **Wrong choice when:** the vestigial column costs nothing in practice; or #270's decision on warnings and a content
  check is still open (this option would pre-empt part of it).
- **Rabbit holes:** header aliases; making the parser lenient in other ways in the same change; dropping the column
  from a workspace before its binaries can read the new shape.
- **Wins instead if:** the owner wants new workspaces to start without a dead column. It can also follow the
  recommendation later, as a separate change.

### Alternative — a number allocator for the idea backlog, `cg index next` (runner-up for 1b)

- **What:** a built-in command that hands out the next idea number. Under an OS file lock in the shared git dir (the
  pattern of `internal/worktree/landlock.go`, with its Unix and Windows halves), it takes the maximum over the landing
  branch, every local worktree's copy of the backlog, and a reservation file next to the lock, then records the
  reservation. Sessions call it instead of reading "highest". The project index still follows the recommended rule
  (rank is row order; a number there would be renumbered on every move).
- **Trade-off:** keeps short idea numbers, at the cost of a new command, a reservation file and lock code on three
  platforms, plus a rule every session must follow (a session that forgets falls back to today's clash).
- **Wrong choice when:** sessions run on more than one machine against the same remote (the reservation file is local
  to one clone), or idea numbers are rarely used to refer to ideas.
- **Rabbit holes:** abandoned reservations (gaps are fine; don't reclaim them); seeing rows on remote-only branches;
  it doesn't fix the insert conflict, so the conflict line of the recommendation is needed anyway.
- **Wins instead if:** the owner wants to keep numeric handles for ideas, and parallel sessions run in worktrees of
  one clone.

### Alternative — number at land time

- **What:** new rows carry a placeholder; `cg worktree land` replaces it with the next free number after its rebase.
- **Trade-off:** collision-free for the `local` strategy (lands are locked) and, with care, `direct-to-main` (inside
  its retry loop). It rewrites the session's commits at land, can't work for the `pr` strategy (no rebase), and the
  insert conflict happens in that same rebase, before any number is assigned.
- **Wrong choice when:** any workspace lands by PR; or when the land step should stay a pure rebase-and-push.
- **Rabbit holes:** amending landed content; placeholders surviving in a paused branch.

### Not credible

- **`merge=union` on the index files.** Removes the conflict, but the experiment shows it silently keeps both
  versions of a row both sides edited and keeps duplicate numbers. A silent wrong merge is worse than a visible
  conflict.
- **Do nothing.** Every clash costs a hand renumber, and records that cite a number may now point at the wrong row.
  With parallel sessions the normal way of working, the cost grows with every session added.
- **Coordination-free IDs in the `#` column** (date or random short IDs). They avoid collisions but lose what a short
  number is for; at that point the slug is a better identifier that already exists.

### Possible follow-up (separate from this item)

`cg worktree land` could resolve an index conflict itself when every conflicted hunk is two pure insertions (empty
base on both sides): keep both, continue the rebase. Without numbers that resolution is always right; with numbers it
is not. It would not help the `pr` strategy. Worth doing only if keep-both conflicts still cost noticeably once
numbering is gone (decision 3).

## Hidden constraints

- **Reversibility:** the recommendation is text in two notes and one line of the template `CLAUDE.md`; fully reversible, and no index file changes. The
  column-drop alternative changes the shape of new workspaces, which is harder to take back.
- **Compatibility and old binaries:** the recommendation changes no format, so every cg version reads every index as
  before. The column-drop alternative does not have that property: see its trade-off. A parser change must keep
  every index that parses today parsing.
- **Existing workspaces and their records:** idea numbers already cited in logs and decisions stay meaningful only
  if existing idea rows are never renumbered or stripped; the rule says so. Existing project rank numbers may be
  cleared by the owner, and only where nothing in the workspace's records cites them (decision 1a). Nothing migrates
  either automatically.
- **Windows:** the recommendation is text only. A parser change is text parsing (`strings.TrimSpace` already drops a
  trailing `\r`); its tests must not depend on line endings (CONTRIBUTING.md, Supported platforms). The allocator
  would add lock code on both platforms.
- **Failure mid-operation, security, performance, data:** nothing new for the recommendation.
- **Dependencies on in-flight work:** none for the recommendation. The allocator would read the list of worktrees and
  session state, which #259 changes (session state records the worktree); it would have to build on #259's merged
  shape, not today's. Nothing here assumes any open #259, #300 or #301 decision.
- **Related issues:** the column-drop alternative overlaps #270 (the header names become the documented required
  columns; the zero-rows warning is #270's ask). Note edits should not add new hard-coded index paths (#229).
- **Who else is affected:** every workspace whose copies of the two notes and of the `conventions` section are unedited, through `cg sync --apply`;
  a workspace that edited them is reported as drifted and doesn't get the rule until it merges it
  (`cmd/sync.go:20-24`, `:37-40`). Where it arrives, Claude stops numbering new rows and stops renumbering, except
  in an index whose own note asks for numbering (Rule 1). No
  tooling that parses the index by position is affected by the recommendation; none was found in the
  owner's public extension repos for the alternative either.
- **Release:** the notes ship inside the binary (CONTRIBUTING.md, Updating templates), and release-please doesn't
  cut a release for `docs:` commits (CONTRIBUTING.md, Impact review), so the change must land as `feat:` (it
  changes how Claude maintains the indexes; a minor bump) to reach workspaces. It needs no major version and does not depend on the 2.0.0 that #259 brings.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1a | Project index: keep numbering rows? And what about existing rank numbers? | **No new numbers.** Rank is row order; a rank number must be rewritten on every move. Existing project numbers: the owner may clear them where nothing in the workspace's records cites them (none found in the reporter's workspace; stale ones could be misread as rank). Where a workspace's own index note asks for renumbering, as the reporter's does, "no new numbers" takes effect only once the owner drops that note (decision 4); the framework doesn't migrate them. | open |
| 1b | Idea backlog: keep numbering rows? | **No, refer to ideas by slug.** If **yes** (short handles matter), take the allocator for the backlog, and say whether parallel sessions ever run on more than one machine against one remote (the allocator covers one clone). | open |
| 2 | The column itself: keep it (empty for new rows) or drop it from new workspaces? | **Keep it for now.** Dropping it needs the header-aware parser first and a release in between, and overlaps #270; do it later, with #270, if at all. | open |
| 3 | Should `cg worktree land` auto-resolve index conflicts that are pure parallel insertions? | **Not in this change.** File it as a follow-up only if keep-both conflicts still cost after numbering is gone. | open |
| 4 | How does a workspace whose own index note asks for numbering or renumbering learn of the conflict? | **From the session that edits that index:** it follows the note and tells the owner once per session, until the owner drops the note or adds the marker `<!-- cg: index numbering kept deliberately -->`. Rejected alternative: a `/cg-sync` contradiction-pass check, which would run from the workspace's previous copy of the command at the sync that brings the rule and miss it. | open |

## Review

**Review 1** — `specs/gh-279-index-numbers/analysis.md` @ content hash `5ebcffe2` (sources at `origin/main` `61abdfd`) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis); the experiment script was re-run for the reviewer, same results
- Findings: 0 blocker, 3 should-fix, 3 nit → fixed: the parser's handling of a table without `#` (it yields zero rows, not misread ones) and what that means for old binaries; decision 1 split into projects and ideas; the "keep the column, stop filling it" option added (now the recommendation); nits fixed (`writeFileIfNotExists` lines, the allocator-tools claim tagged as an assumption, the `pr` strategy's conflict). Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 6 → answered by the fixes: split allowed (decision 1a/1b), keep-vs-drop the column (decision 2), old binaries (Hidden constraints); minor (my call): parser fallback and warning behaviour, case-insensitive headers without aliases (both only in the column-drop alternative), a workspace's own "renumber after any move" header note is dropped (recommended rule). Owner's: none beyond the listed decisions
- Verified true: index templates and line ranges; parser cells and callers; index files written only when missing; no numbering rule anywhere in templates; rank is row order; land lock is local-strategy only; experiment results; #303 touches none of the affected files; no private names
- Not verified: the private-workspace counts (reported observation); the owner's extension repos and the issue list (checked by the author, not by the reviewer)

**Review 2** — `specs/gh-279-index-numbers/analysis.md` @ content hash `db03d647` (delta from `5ebcffe2`) · 2026-11-22
- Reviewers: doc-reviewer (delta: builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 2 should-fix, 2 nit → fixed: the release type (must be `feat:` or `fix:`, since `docs:` cuts no release); "never renumber" now applies to idea numbers only, and existing project rank numbers may be cleared by the owner (decision 1a); a session proposes header-note changes rather than making them; "every workspace" narrowed to workspaces with unedited notes. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 2 → answered by the fixes (existing project numbers: decision 1a; header note: the owner, on a session's proposal)
- Regression check: all four round-1 findings resolved
- Verified true: `cmd/match.go:203-205` and `:243-251` (empty cells kept, so an empty `#` still parses); `cmd/init.go:669-681`; auto-update off switches; `cg sync` touches only unedited notes
- Not verified: the private-workspace counts; the exact `cg status` / `cg share` messages

**Review 3** — `specs/gh-279-index-numbers/analysis.md` @ content hash `5d731146` (delta from `db03d647`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 2 nit → fixed: Hidden constraints "Existing workspaces" narrowed to idea numbers, project numbers clearable by the owner; "nothing cites them" made a condition, with the reporter-workspace count of project-number citations (zero) added as evidence; release type pinned to `feat:`. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 1 minor (my call: until the owner changes a workspace's own header note, that note wins over the framework rule, as workspace-local instructions do)
- Regression check: round-2 findings resolved, apart from the Hidden-constraints contradiction fixed here
- Verified true: `cmd/sync.go:20-24`, `:37-40`; CONTRIBUTING.md on embedded templates, `docs:` not released, bump rules; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace counts

**Review 4** — `specs/gh-279-index-numbers/analysis.md` @ content hash `3ab9ec5d` (delta from `5d731146`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: Rule 1 and decision 1a now say that where a workspace's own index note asks for renumbering (the reporter's included), the project-index fix takes effect only once the owner drops that note, and that a session proposes it the first time it edits the index after the rule arrives; the "own note wins" rule cites `templates/commands/cg-sync.md:56` as the precedent it extends. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 1, answered by the fix (when the session proposes the note change)
- Regression check: all round-3 findings resolved
- Verified true: CONTRIBUTING.md bump rules; the cg-sync precedent; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace counts

**Review 5** — `specs/gh-279-index-numbers/analysis.md` @ content hash `acafe080` (delta from `3ab9ec5d`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 2 nit → fixed: the proposal trigger is now observable and one-shot (the `/cg-sync` contradiction pass also reads the index files' own notes; the owner decides once; no record, no repeat), listed as decision 4; decision 1a names what waits ("no new numbers") and its punctuation is repaired; Rule 1 and 1a use the same condition wording. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 1, answered by the fix (a declined change is not proposed again)
- Regression check: round-4 findings resolved
- Verified true: `templates/commands/cg-sync.md:55-56`; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations

**Review 6** — `specs/gh-279-index-numbers/analysis.md` @ content hash `4d7b5eed` (delta from `acafe080`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 1 blocker, 2 should-fix, 1 nit → fixed: the `/cg-sync` trigger is dropped (at the sync that brings the rule, Step 2 runs from the workspace's previous copy of the command, and later syncs don't review it again: `templates/commands/cg-sync.md:44-46`, `:68-69`); replaced by a stateless trigger any path reaches: the session editing an index with a conflicting note follows it and tells the owner once per session, until the note is dropped or marked deliberate; "Where it arrives" qualified to match Rule 1; drifted-note path covered by the same trigger. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 2 → answered: the `/cg-sync` change is dropped; "the note" is the text above the index's tables, and the wording is "numbering or renumbering" in both places
- Regression check: round-5 nits resolved; round-5 should-fix superseded by the new trigger
- Verified true: `templates/commands/cg-sync.md:42-58`, `:68-69`; `CONTRIBUTING.md:61-67`; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations

**Review 7** — `specs/gh-279-index-numbers/analysis.md` @ content hash `35a94150` (delta from `4d7b5eed`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: the rule now reaches every row-editing path through one every-session line in the template `CLAUDE.md` `conventions` section, with the detail in `notes/project-structure.md` (`/wrap` and several notes edit rows without loading the two notes); "kept deliberately" has an exact marker. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 2, answered by the fixes (which files carry the rule; the marker's exact form)
- Regression check: all round-6 findings resolved
- Verified true: `templates/commands/cg-sync.md:44-46`, `:68-69`; `/wrap` doesn't reference the two notes; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations

**Review 8** — `specs/gh-279-index-numbers/analysis.md` @ content hash `d7db3830` (delta from `35a94150`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: the every-session line now carries the exception and its trigger (follow a conflicting index note unless the marker is there, and load the note that says to tell the owner); item 3 says "no index file changes". Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 1, answered by the fix (the line itself says to follow the index note)
- Regression check: round-7 findings resolved
- Verified true: `templates/workspace/CLAUDE.md:306-318`, `:312`; `/wrap` row edits; `cmd/sync.go:23`, `:39`; CONTRIBUTING.md:160; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations

**Review 9** — `specs/gh-279-index-numbers/analysis.md` @ content hash `48ffb44f` (delta from `d7db3830`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: in the every-session line the marker now gates only the notice (an index note asking for numbering is always followed); wording aligned to "numbering or renumbering". Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 1, answered by the fix (with the marker, the session follows the note and stays silent)
- Regression check: round-8 findings resolved
- Verified true: item 3 consistent with "Who else is affected"; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations

**Review 10** — `specs/gh-279-index-numbers/analysis.md` @ content hash `bcfc6ad3` (delta from `48ffb44f`) · 2026-11-22
- Reviewers: doc-reviewer (delta: all five lenses)
- Tests: not applicable (analysis)
- Findings: none. **Clean.**
- Builder questions: none
- Regression check: round-9 findings resolved
- Verified true: the every-session line, Rule 1 and decision 4 agree on what the marker does; wording consistent; no private details; no reliance on open #259/#300/#301 decisions
- Not verified: the private-workspace observations (counts of rows and citations, the index note asking for renumbering)
- After this round: one wording change only (a second repo's name replaced by "the owner's plugin marketplace repo" in Current state and Recon, for the private-context rule); no re-review needed for a rewording

## Verdict

**proceed**, once the owner settles decisions 1a, 1b, 2, 3 and 4. The last review round (10) is clean, no review
finding is waiting on the owner, and no unverified assumption would flip the recommendation. The private-workspace
observations support decision 1b's framing (idea numbers are cited), not the recommendation's correctness.

## Q→A log

- (none yet)
