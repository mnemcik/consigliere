---
title: "Analysis — CI time per PR run and per update from main"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/284"
size: full
verdict: needs clarification
---

# Analysis — CI time per PR run and per update from main

## Question

Every PR run of CI takes about 4 minutes before the required checks are green, docs-only PRs included. Because
`main` requires up-to-date branches, each merge leaves the other approved PRs behind, and each of them pays a full run
again after its update from `main`. This slows every framework PR, and parallel sessions multiply the round trips. The
issue asks to measure first, then decide which changes to make and measure each one's effect.

This is a **how** item: the cost is real and measured below. The question is which levers to pull, in what order.

## Current state (evidence)

### Method

All numbers come from the GitHub API for this repository, read on 2026-11-22:

- `actions/workflows/ci.yml/runs` since 2026-10-01: 354 runs (278 `pull_request`, 76 `push`).
- `actions/runs/{id}/jobs?filter=all` for each run: 2,993 jobs and their steps, re-run attempts included.
- `pulls/{n}/commits` and `commits/{sha}/statuses` for the 101 PRs opened since 2026-09-25: 366 commits, 56 of them
  merges of `main` into the PR branch.
- The job logs of three `main` runs from 2026-11-17/18 (`44264161770`, `44397807426`, `44400549350`) on all three
  OSes, for per-package and per-test times (`go test -v` output).

"p50/p90" are medians and 90th percentiles over successful first attempts unless stated. Wall time is from the run's
creation to the end of the last required job.

### What CI runs today

- `.github/workflows/ci.yml`: one workflow on `pull_request` and on `push` to `main`, with no `paths` filter and no
  `concurrency` group. Jobs: `Lint` (two golangci-lint passes, the second with `GOOS=windows`), `Test` on
  `ubuntu-latest`, `macos-latest`, `windows-latest` (each `go test -v -race -coverprofile=coverage.out ./...`,
  `ci.yml:71`), and `Build` ×5, which `needs: [lint, test]` (`ci.yml:82`).
- Branch protection on `main` (`gh api repos/mnemcik/consigliere/branches/main/protection`): required checks `Lint`,
  `Validate PR title`, `Test (ubuntu-latest)`, `Test (macos-latest)`, `Test (windows-latest)`, `impact-review`;
  `strict: true` (branches must be up to date); one approving review; linear history. `Build` is not required.
- Repository settings: owner is a **User** account, public; auto-merge allowed; the "always suggest updating the
  branch" setting is off.
- No test calls `t.Parallel()` and none checks `testing.Short()` (grep over `*_test.go`, 0 hits each). Packages run in
  parallel (Go's default), tests within a package run one after another.

### Where a run spends its time

Job durations, November 2026 (1–22), successful jobs:

| Job | p50 | p90 | n |
|---|---|---|---|
| Lint | 54 s | 66 s | 157 |
| Test (ubuntu-latest) | 33 s | 61 s | 158 |
| Test (macos-latest) | 100 s | 131 s | 149 |
| **Test (windows-latest)** | **218 s** | **291 s** | 158 |
| Build (each of 5, not required) | 28 s | 33 s | 725 |

- **Wall time to green required checks**, PR runs with all four required CI jobs (so in practice since the Windows
  job was added on 2026-10-13): p50 223 s, p90 336 s (n=185). October p50 220 s,
  November p50 226 s. `push` runs on `main`: p50 233 s.
- **The Windows test job is the critical path** in 211 of 233 such runs; macOS in the other 22. Queueing is not a
  factor for Linux and Windows (p90 3 s); macOS queues p50 7 s with outliers (below).
- **Inside the Windows job** (November step p50): checkout 6 s, `setup-go` 31 s, `Run tests` 172 s (p90 249 s). On
  Ubuntu: `setup-go` 8 s, tests 17 s. On macOS: `setup-go` 23 s, tests 60 s.
- **Inside `Run tests` on Windows**, per package (three `main` runs): `cmd` 118–182 s, `internal/worktree` 74–116 s,
  `internal/share` 52–85 s, `internal/githooks` 19–31 s, everything else ≤ 16 s. On Ubuntu the same packages take
  13–15 s, 6–9 s, 4–6 s. `cmd` alone is the long pole of the step: the step is roughly `cmd` plus compile time.
- **Per test**, the same 614 top-level tests sum to 278–441 s on Windows, 107–131 s on macOS and 27–31 s on Ubuntu: a
  10–14× factor between Windows and Ubuntu. The cost is spread out: 107–156 Windows tests take ≥ 1 s each, together
  211–400 s. The slowest (`TestResolve` 10–15 s, `TestConcurrentEnsureKeepsForeignHook` 7–12 s,
  `TestSharePublish*` 5–10 s, `TestUpdateRespectsUserEdits` 6–10 s) are a few percent each, so speeding up a handful of
  tests won't move the total. These tests drive real `git` repositories in temp directories.
- **`-race` and coverage:** a local A/B on macOS (`go test -count=1 ./...` with and without `-race -coverprofile`) gave
  84–87 s with and 49–92 s without, on a machine running other work: **inconclusive**. The tests mostly wait on `git`
  processes, which the race detector doesn't slow, so its share on Windows is unknown until measured in CI.
  *Assumption (med):* dropping `-race` on Windows saves little.

### The macOS "timeouts"

The issue reports macOS test jobs hitting a 15-minute timeout. The data says something else:

- 9 macOS test jobs were `cancelled` after ~901 s, 8 of them on 2026-11-16 and one on 2026-11-18 (PRs and one `main`
  push alike). **None of them ever got a runner:** the job has no runner name and **no steps**. Re-run attempts of the
  same run that did get a runner finished in about a minute (run `43875955421` attempt 2: tests 63 s).
- `ci.yml` sets no `timeout-minutes`, and other macOS jobs started and finished inside the same 15-minute windows
  (3–8 of them), so this is neither a hanging test nor this repo's own macOS concurrency cap (5 concurrent macOS jobs on
  the Free plan, [Actions limits](https://docs.github.com/en/actions/reference/limits), read 2026-11-22). The
  documented discard for a queued GitHub-hosted run is 45 minutes
  ([GitHub-hosted runners](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners), "Workflow
  continuity"), so the 15-minute cut-off isn't explained by the docs. *Assumption (med):* a GitHub-side
  runner-assignment problem on those days. Couldn't verify against GitHub's status history.
- Since 2026-11-18 18:30, 21 macOS test jobs: 19 success, 2 failures (both from PR #303's own commits), no
  cancellations. Nothing in the repo would fix a job that never starts; a re-run is the workaround.

### Kinds of PR and the update-from-`main` round trips

48 PRs merged between 2026-10-13 (when the Windows job was added) and 2026-11-22, classified by changed files:

| Kind | PRs | CI runs | Runs per PR (median) | Merges of `main` into the branch |
|---|---|---|---|---|
| Code (any Go or other file) | 31 | 122 | 4 | 29 |
| Markdown, including `templates/` | 4 | 19 | 5 | 9 |
| Docs only (only `*.md` files, none under `templates/`) | 4 | 11 | 2 | 4 |
| Dependency bumps | 7 | 15 | 2 | 3 |
| CI only (`.github/`) | 2 | 3 | 1.5 | 1 |
| **Total** | **48** | **170** | | **46** |

- **Round trips:** 23 of the 48 PRs had at least one update from `main`; the 46 merge commits are about **27 % of all PR
  runs** (170). Each costs a full run, about 4 minutes, plus CodeRabbit's re-review, plus the maintainer's
  `impact-review` re-check of the new head (`CONTRIBUTING.md`, "Impact review").
- **CI is on the critical path after the last push**, not people: on the maintainer-reviewed PRs, `impact-review` was set
  a median 0.7 min after the head's CI ended (n=17), and the PR merged a median 2.6 min after CI ended (n=48).
- **Docs-only is a small share**: 4 PRs (#241, #242, #243, #252: `CONTRIBUTING.md`, `CLAUDE.md` and the `.github/`
  issue and PR templates), 11 runs, 6 % of runs. **Template Markdown is not docs-only:** everything
  under `templates/` is embedded in the binary (`templates/embed.go:14`) and tested (`templates/embed_test.go`;
  `TestClaudeFileHistoryCoversEmbed`, `CONTRIBUTING.md` "Updating templates"). PR #230, a `docs:` PR that changed two
  template files, failed its tests on all three OSes until `go generate` was run (run `43874261974`). No test reads
  `docs/` or the repository's root Markdown files (grep over `*_test.go`), so those can't break a test.

### Existing solutions checked

- **Merge queue** removes the need for authors to update their branch while keeping the up-to-date guarantee
  ([managing a merge queue](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue),
  read 2026-11-22). None of the merge-queue, merging-with-a-queue or ruleset pages states which repositories can use
  it. *Assumption (med):* merge queues need an organization-owned repository, and this one is user-owned. Couldn't
  verify; the owner can check whether "Require merge queue" is offered in `main`'s branch protection or rulesets.
- **Auto-merge** is enabled, but merges only "after all required reviews and status checks pass"
  ([auto-merge](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/incorporating-changes-from-a-pull-request/automatically-merging-a-pull-request));
  it doesn't update a branch that falls behind, so it doesn't remove a round trip.
- **Skipping work per PR kind:** a workflow skipped by a `paths` filter leaves its required checks `Pending` and blocks
  the merge; a *job* skipped by a job-level `if:` reports success and doesn't block
  ([troubleshooting required status checks](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks);
  [job conditions](https://docs.github.com/en/actions/using-jobs/using-conditions-to-control-job-execution)). So a
  docs-only skip has to be a job-level condition fed by a change-detection job, not `paths-ignore`.
- **Runner sizes for public repositories:** `ubuntu-latest` and `windows-latest` 4 vCPU / 16 GB, `macos-latest`
  3 (M1) / 7 GB ([choose the runner](https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/choose-the-runner-for-a-job),
  read 2026-11-22). Larger runners cost money and are not needed for the options below.

### In-flight work and how this relates

- **#259 / PR #303** (`gh-259-extension-commands`) adds or changes 16 test files, 10 of them in `cmd`, with about 80
  new `t.Setenv` calls. It doesn't touch `.github/` or the `Makefile`. Two consequences for this item: `cmd` on Windows
  will get slower when it lands, and `t.Setenv` can't be combined with `t.Parallel()`, so making `cmd`'s tests parallel
  gets harder, not easier. Nothing here depends on #303's open decisions; the options below change only the workflow
  and keep away from test files #303 touches.
- **#300** (`gh-300-extension-skills`) and **#301** (`gh-301-extension-hook-filters`) are analyses of extension
  features; their specs mention Windows only for runtime behaviour and test skips. No overlap with CI. All three are
  among the PRs that will pay the round trips this item reduces.

**Recon checked:** `ci.yml`, `impact-review.yml`, `pr-title-lint.yml`, `Makefile`, `.golangci.yml`,
`templates/embed.go`, `CONTRIBUTING.md`, `CLAUDE.md`; branch protection and repository settings via the API; CI runs,
jobs, steps, logs, PR commits and statuses (above); the test sources (grep for `t.Parallel`, `testing.Short`,
`t.Setenv`, `t.Chdir`, repo-doc reads); the specs on the three in-flight branches; GitHub docs on merge queue,
auto-merge, required checks and skips, runner sizes, limits. Installed plugins and the owner's extension repos: nothing
relevant to CI time found. Not checked: GitHub status history for 2026-11-16; Windows Defender or disk settings on the
runner image.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes. Required checks take p50 223 s per PR run, the same for docs-only PRs, and 27 % of runs follow an update from `main`. | Run/job data above |
| Is the expected behaviour right? | Yes, within the repo's rules: Windows is a supported platform whose test check is required (`CONTRIBUTING.md` "Supported platforms"), so it has to stay required; the aim is a faster run, not fewer platforms. | `CONTRIBUTING.md`, branch protection |
| Is it still current? | Yes for run time (November p50 226 s, slightly above October). The macOS cancellations stopped after 2026-11-18. | Above |
| Is it the right problem? | Partly. The issue frames run time and docs-only runs; the measurements show the cost is **one job** (Windows tests, the critical path in 91 % of runs), that docs-only runs are 6 % of runs, and that the macOS "timeouts" were jobs that never got a runner, not slow tests. | Above |

**Verdict:** partly valid. Real problem: the Windows test job, mostly `cmd`, `internal/worktree` and
`internal/share` running `git`-heavy tests one at a time, sets the run time for every PR; the round-trip count
multiplies it. The macOS cancellations are a GitHub-side runner problem with no repo fix.

## Options

Run time and round trips multiply, so options are judged by what they take off the ~223 s critical path and off the
number of runs.

### Recommended — Shard the Windows tests, keep the required check name, measure in one experiment PR

- **What:**
  1. Split the Windows test job into N parallel shards (N between 4 and 6, picked by the experiment). The heavy
     packages (`cmd`, `internal/worktree`, `internal/share`) are each split by test name across shards; the light
     packages go into one shard. Each shard builds the test binary of each heavy package once **on its Windows
     runner** (`go test -c` with the run's flags), lists its tests with that binary (`-test.list`, filtered to `^Test`),
     takes its own slice, and runs the same binary with `-test.run` (and `-test.coverprofile` if coverage is kept), one
     package at a time so a test name can't match in another package. Run directly, the binary loses what `go test`
     adds, so the shard passes `-test.timeout=10m` and `-test.v` explicitly and runs each binary from its package
     directory. Building once avoids a second link per package;
     no separate list-computing job runs before the shards. Listing on Windows means
     `*_windows_test.go` tests are included and new tests (#303 adds several) land in a shard automatically; the split is
     deterministic, not random. Linux and macOS keep one job each.
  2. Keep the required check `Test (windows-latest)` as a small job that `needs` the shards and fails unless every one
     of them succeeded (`if: always()` with an explicit result check, as the required-checks troubleshooting page
     advises for dependent jobs). Branch protection then needs no change. `Build` then `needs` that job. This job is
     named like a required check, which `CONTRIBUTING.md` rejects as written (decision 7).
  3. In the same experiment PR, and only as measured A/B variants, try the cheap Windows-only knobs: no `-race`, no
     `-coverprofile`, `setup-go` caching variants (its 31 s is paid once per shard), and the Go and temp directories on
     a faster disk if the runner has one. Adopt a knob only if it shows a clear win over several runs. Coverage, if
     kept on Windows, is uploaded per shard (`coverage-windows-<n>`); nothing in the repo reads these artifacts
     (grep over `.github/`, `Makefile`, `*.md`).
  4. Add a `concurrency` group per PR with `cancel-in-progress` for `pull_request` runs, so a superseded push frees its
     jobs. It doesn't shorten a run, but it offsets the extra shard jobs against the 20-job cap.
  5. Measure: the same API method as above, over at least 10 runs before and after, per step and per shard; record it
     in the PR.
- **Expected effect** (*assumption, med*, until the experiment measures it). A shard costs setup (~40 s: checkout,
  `setup-go`) plus compile (~20–70 s: the `Run tests` step minus `cmd` was 20–71 s in the three logged runs) plus its
  slowest slice of tests. A naive split that leaves `internal/worktree` (74–116 s) or `internal/share` (52–85 s) whole
  would land at ~130–200 s, hardly better than today; that is why the heavy packages are split too. With them split,
  a shard is roughly 40 + 20–70 + 40–60 s = 100–170 s, so the Windows job goes from p50 ~218 s to roughly that, macOS (p50
  100 s, p90 131 s) becomes the critical path more often, and required-check wall time (plus a few seconds for the
  aggregate job) drops from ~223 s to roughly 110–175 s. That saving applies to every run, round trips included.
- **Trade-off:** N Windows jobs per run instead of 1 against the Free plan's 20 concurrent jobs; a run today has up to
  5 jobs at once (lint, 3 tests, title lint; then 5 builds), with shards 8–10, so two or three PRs pushing at once
  would start to queue (item 4 eases this). More YAML, and a test-list split that must not drop tests (the aggregate
  job and a count check guard that). The fixed ~40 s setup per shard limits how far more shards help.
- **Wrong choice when:** parallel sessions regularly push 3+ PRs at the same minute; then the concurrency cap eats the
  gain and the round-trip options matter more.
- **Rabbit holes:** making `cmd`'s tests `t.Parallel()` (177 test functions, 37 `Setenv` calls, 17 chdirs, 31 uses of
  the global `rootCmd`, and #303 adds ~80 more `t.Setenv`): a large refactor of shared state, not a CI change. Chasing
  single slow tests: the cost is spread over 100+ tests. Speeding up `git` on Windows (Defender, `core.fscache`):
  unmeasured, image-dependent.

### Alternative — Skip the tests and build for docs-only PRs (runner-up as a companion, not a replacement)

- **What:** on `pull_request` runs only, a step right after `actions/checkout` in each `Lint`, test and shard job
  checks whether every changed file is a `*.md` file outside `templates/` (the class measured above, `.github/` issue
  and PR templates included), with `git diff --name-only HEAD^1 HEAD` on the PR's merge commit (checkout with
  `fetch-depth: 2`). That needs no permission beyond today's `contents: read` (`ci.yml:9-10`), unlike the PR-files API.
  If the check itself fails, the job runs everything. A docs-only PR then passes without testing the merged tree,
  which is safe only while `main` requires up-to-date branches (decision 4 keeps `strict`; relaxing it should revisit
  this skip); `push` runs on `main` always test everything. A separate detector job is avoided: every
  test job would have to `needs:` it, adding its start-up to the critical path of every code PR. The skip is done
  **at step level** (the job starts, skips `setup-go`, lint or the tests, and succeeds), not with a job-level `if:` on
  the matrix job: *assumption (med), not verified:* a matrix job
  skipped by a job-level `if:` reports one check under its unexpanded name (`Test`), which would leave the three
  required `Test (…)` checks expected and block the PR. Everything under `templates/` counts as code. `Lint` skips
  the same way: with only Markdown changed it has no Go code to check.
- **Trade-off:** a docs-only PR's required checks finish in roughly 20–30 s instead of ~4 minutes (about 55–65 s if
  `Lint` were kept), on the 6 % of runs that are docs-only; one extra step of a few seconds in each job of every other
  PR. A wrong detector (a new
  embedded path outside `templates/`, a test that starts reading a root `*.md`) would let a breaking PR through
  untested; the `push` run on `main` catches it afterwards.
- **Wrong choice when:** used alone: it doesn't touch the 94 % of runs that carry code or templates.
- **Rabbit holes:** extending the skip to "Markdown under `templates/`" (PR #230 shows those break tests); a
  third-party paths-filter action (prefer `git diff --name-only` against the base in a shell step).
- **Wins instead if:** the owner's main pain is docs PRs specifically, or the shard experiment saves less than ~40 s
  per run (then drop the shards and keep only this and the `concurrency` group).

### Alternative — Fewer round trips: relax "up to date before merging"

- **What:** set `strict: false` on `main`'s required checks, so a PR that is behind but conflict-free can merge on its
  last green run. Merge queue would be the safer form, but is (*assumption, med*, see above) not available to a
  user-owned repository.
- **Trade-off:** removes most of the 27 % of runs that follow an update from `main`, and their CodeRabbit and
  `impact-review` re-checks. The cost: two PRs that each pass alone can break `main` together (a semantic conflict);
  the `push` run on `main` catches it only after the merge, and `main` stays red until a fix lands. It also changes the
  merge process: `CONTRIBUTING.md` ("Impact review", "New commits need a new review") ties the maintainer's re-check
  of an updated head to "branch protection requires up-to-date branches", and that check that "nothing which landed on
  `main` since … contradicts it" would no longer happen at merge time unless the maintainer does it without an update.
  That section and step 3 of "How a PR gets to merge" (the author updates the branch from `main`) would need
  rewriting.
- **Wrong choice when:** PRs often touch the same packages or templates (they do: `cmd` and `templates/` are shared by
  most PRs), or when a red `main` would block parallel sessions more than the round trips do.
- **Rabbit holes:** moving the repository to an organization for merge queue (changes URLs, install script and release
  links); bots that update branches automatically.
- **Wins instead if:** shards and the docs-only skip still leave the round trips as the main delay, and the owner
  accepts an occasional red `main` and the process change.

### Do nothing

Credible only as "re-run the stuck macOS jobs by hand", which is today's workaround for the one part with no repo fix.
For run time it leaves ~4 minutes per run and per round trip, which the issue's priority says is not acceptable.

### Rejected

- **Make Windows tests non-required or post-merge only:** contradicts `CONTRIBUTING.md` ("the Windows test check is
  required to merge") and the platform rule in `CLAUDE.md`.
- **Larger or paid runners:** a cost the Free plan doesn't need for these gains.

## Hidden constraints

- **Reversibility:** workflow-only changes; reverting the PR restores today's CI. The required-check name stays the same,
  so branch protection doesn't change, unless the owner picks the `strict: false` option (a settings change, also
  reversible).
- **Failure mid-operation:** an aggregate job that reads shard results wrongly could report success with a shard
  missing; it must fail on any non-`success` result, including `skipped` and `cancelled`, and the split must cover every
  test (compare the union of shard lists with `go test -list`).
- **Security and governing rules:** `CONTRIBUTING.md` gives workflow changes extra scrutiny and says "A workflow job
  named like a required check (`impact-review` above all) is rejected, since its result would satisfy that check
  without a review." The aggregate `Test (windows-latest)` is such a job by design, so as written the rule rejects it.
  Either the rule is clarified in the same PR (an aggregate of the real test jobs that fails unless all of them pass is
  allowed; `impact-review` stays forbidden), or branch protection requires the shard checks by name instead, and must
  be edited whenever N changes (decision 7). No new third-party action is needed.
- **Performance:** more concurrent jobs per run against the 20-job Free cap; check queue times in the experiment.
- **Dependencies:** #303 adds `cmd` tests; shard lists must be computed at run time, not hard-coded, so new tests land in
  a shard automatically.
- **Operations:** `CONTRIBUTING.md` describes CI ("CI (lint, including a Windows build, tests on Linux, macOS and
  Windows, cross-platform build) must be green"); the wording stays true, but a note on sharding belongs next to `ci.yml`'s comments.
- **Who else is affected:** every open PR re-runs CI after the change lands; dependency bots' PRs are unaffected beyond
  that. No effect on workspaces (`cg sync` ships nothing from `.github/`).

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Which lever(s) to pursue | Shard the Windows tests behind an unchanged `Test (windows-latest)` check, measured in one experiment PR | open |
| 2 | Add the docs-only skip as well | Yes, as a separate small PR after #1, with everything under `templates/` treated as code, done as a step after checkout in each job (Lint included), with `git diff` on the merge commit, `pull_request` only | open |
| 3 | Try the Windows-only knobs (no `-race`, no coverage, `setup-go` cache variants, faster disk) in the experiment, adopting only measured wins | Yes; keep `-race` and coverage on Linux and macOS regardless | open |
| 4 | Relax "up to date before merging" (`strict: false`) to cut round trips | No for now; revisit with the post-shard numbers | open |
| 5 | Treat the 2026-11-16/18 macOS cancellations as out of scope (GitHub-side, no runner assigned; re-run is the workaround) | Yes; say so on the issue when the fix PR lands | open |
| 6 | Success criterion for the experiment | Required-check wall time p50 ≤ 170 s over ≥ 10 PR runs, no test dropped, queue p90 for Windows shards ≤ 10 s; ≤ 170 s is the target, not the gate: shards that save ~40 s or more per run are kept even above it; if they save less, drop them and keep only the docs-only skip and the `concurrency` group | open |
| 7 | How the required `Test (windows-latest)` check survives sharding, given `CONTRIBUTING.md`'s rule against jobs named like a required check | Keep it as an aggregate job and clarify the rule in the same PR (aggregates of the real test jobs allowed, `impact-review` never) | open |
| 8 | Add a per-PR `concurrency` group with `cancel-in-progress` for `pull_request` runs | Yes, in the experiment PR | open |

## Review

**Review 1** — `specs/gh-284-ci-time/analysis.md` @ content hash d6341bb (commit cec5ea7) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: n/a (analysis)
- Findings: 0 blocker, 6 should-fix, 3 nit → fixed: shard estimate ignored the non-`cmd` packages (heavy packages now
  split, per-shard estimate); docs-only skip on a matrix job (now step level, tagged assumption); aggregate job vs the
  `CONTRIBUTING.md` rule (quoted literally, decision 7); `strict: false` process knock-on stated; `concurrency` group
  added (decision 8); `setup-go` cache variants added; nits (line numbers, measurement window, docs-only class) fixed;
  owner's: none; rejected: none; dropped in validation: none
- Builder questions: 5 answered in the text (lists computed on the Windows runner, coverage per shard, skip on
  `pull_request` only, `Build` needs the aggregate, fallback in decision 6), 0 owner's
- Verified true: branch protection and settings; no `t.Parallel`/`testing.Short`; 614 tests per log and the
  per-package ranges; the 9 macOS cancellations without runner or steps; PR-kind table and ratios; #303 diffstat and
  no overlap with #300/#301; GitHub limits, runner sizes, 45-min discard, skip semantics
- Not verified: naming of a skipped matrix check; #303's `t.Setenv` count (diff not given); job p50/p90 and
  critical-path counts (scripts not run); the local `-race` A/B; GitHub status history; merge-queue availability

**Review 2** — @ fddf302 (uncommitted, delta from d6341bb) · 2026-11-22
- Reviewers: doc-reviewer (same lenses, delta)
- Findings: 0 blocker, 3 should-fix, 1 nit → fixed: detector as a separate job would sit on the critical path (now a
  step in each job); list computation could add a serial job (each shard lists itself); docs-only figure vs `Lint`
  (Lint skips too); arithmetic 100–170 s; owner's: none; rejected: none
- Builder questions: 3, all answered in the text (decision 6 target vs gate, detector as step, Lint skips)
- Regression check: all round-1 findings resolved
- Not verified: "`Run tests` minus `cmd` = 20–71 s" not recomputed; skipped-matrix naming

**Review 3** — @ 56462c0 (uncommitted, delta from fddf302) · 2026-11-22
- Reviewers: doc-reviewer (same lenses, delta)
- Findings: 0 blocker, 2 should-fix, 1 nit → fixed: double link with `-list` then `-run` (build once with
  `go test -c`); `gh pr diff` needs a permission the workflow lacks (now `git diff` on the merge commit,
  `fetch-depth: 2`); filter lists to `^Test` and run per package; owner's: none; rejected: none
- Regression check: all round-2 findings resolved
- Not verified: whether `go test` caches linked test binaries; PR-files API with `contents: read`

**Review 4** — @ c4d42e9 (uncommitted, delta from 56462c0) · 2026-11-22 — **clean**
- Reviewers: doc-reviewer (same lenses, delta)
- Findings: 0 blocker, 0 should-fix, 2 nit → both fixed after the round (text only): explicit `-test.timeout` and
  `-test.v` and package working directory for a directly-run binary; the docs-only skip relies on `strict` (linked to
  decision 4)
- Regression check: all round-3 findings resolved
- Not verified: `go test -c -race -cover` timing on Windows (for the experiment); merge-ref parent order from the
  reviewer's knowledge, not docs

## Verdict

**Needs clarification:** the analysis is ready, but the owner decides decisions 1–8. Decision 7 (the
`CONTRIBUTING.md` rule against a job named like a required check) blocks the recommended design as written, and the
expected saving (110–175 s wall time) is an assumption that the experiment in decision 1 is there to measure; decision 6
says what happens if it falls short.

## Q→A log

- (none yet)
