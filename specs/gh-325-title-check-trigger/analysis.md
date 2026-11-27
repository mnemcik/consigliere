---
title: "Analysis — the required 'Validate PR title' check stops on 2026-12-02 (#325)"
created: 2026-11-27
item: "https://github.com/mnemcik/consigliere/issues/325"
size: full
verdict: needs clarification (owner decisions 1-5)
---

# Analysis — the required 'Validate PR title' check stops on 2026-12-02 (#325)

- **Item:** https://github.com/mnemcik/consigliere/issues/325
- **Size:** full (small change, but four decisions and a deadline)
- **Status:** analysis only, waiting on the owner. No spec, plan or code.
- **Scope:** the title check (`.github/workflows/pr-title-lint.yml`) only. The bot-exemption workflow
  (`impact-review.yml`) on the same trigger is #255's (its decision 6, amended:
  https://github.com/mnemcik/consigliere/issues/255#issuecomment-7222904576).
- **Evidence base:** consigliere `origin/main` at `5c4aa50` (2026-11-27); branch protection, Actions settings and the
  repository's Actions policies read with `gh api` on 2026-11-27; open PRs #312, #318, #324 read on 2026-11-27;
  cg-extensions and cg-extensions-registry workflows read via the contents API on 2026-11-27; GitHub pages read
  2026-11-27 (docs pages show no date; REST at API version 2026-03-10):
  - changelog, 2026-11-02: https://github.blog/changelog/2026-11-02-pull_request_target-blocked-by-default-december-2
  - Actions policies: https://docs.github.com/en/actions/concepts/about-actions-policies,
    https://docs.github.com/actions/how-tos/administer/control-workflow-execution,
    https://docs.github.com/en/rest/actions/policies
  - https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target
  - events: https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows
  - the action's README: https://github.com/amannn/action-semantic-pull-request

## Question

The required check `Validate PR title` comes from a workflow that runs only on `pull_request_target`. From 2026-12-02
GitHub blocks that event by default in public repositories, so the check stops reporting and no PR that gets a new
head after that date can merge, the 2.0.0 release PR included. It affects everyone who opens or merges a PR in this repository.

This is a **"how"** item: the problem is real and dated (below). The question is how to keep the check running
without undoing the security change, how long that takes, and in what order against the 2.0.0 release.

## Current state (evidence)

**The workflow.** `.github/workflows/pr-title-lint.yml` (at `5c4aa50`):

- line 7: `on: pull_request_target`, types `opened`, `edited`, `reopened`, `synchronize` (lines 8-12);
- lines 14-15: `permissions: pull-requests: read`;
- line 19: job `name: Validate PR title`; line 22: `uses: amannn/action-semantic-pull-request@v6` with
  `GITHUB_TOKEN` (lines 23-24). Nothing is checked out and no secret other than `GITHUB_TOKEN` is used.
- Unchanged since it was added in `844f03b` (#58).

**It is required.** `main`'s required status checks are `Lint`, `Validate PR title`, `Test (ubuntu|macos|windows-latest)`
(each pinned to `app_id` 15368, GitHub Actions) and `impact-review` (`app_id: null`), strict
(`gh api repos/mnemcik/consigliere/branches/main/protection`, 2026-11-27). Because the check is pinned to GitHub
Actions, a commit status posted through the API can't satisfy it; any Actions workflow job with that name can.

**It runs today.** The last five runs of the workflow were all `pull_request_target`, all `success`, the latest on
2026-11-27 15:57 UTC (`gh run list --workflow pr-title-lint.yml`).

**What changes on 2026-12-02.** "Starting December 2, 2026, GitHub Actions will block `pull_request_target` events
by default in all public repositories, unless an applicable Actions event policy explicitly allows them." Private
and internal repositories aren't affected (changelog, 2026-11-02). The repository is public and user-owned
(`gh api repos/mnemcik/consigliere`: `User`, `public`). The docs page on the default policy words the scope more
narrowly ("affected repositories that were using the default `pull_request_target` policy before general
availability"); this repository has no policy of its own (`GET /repos/mnemcik/consigliere/actions/policies` →
`{"total_count":0,"policies":[]}`, 2026-11-27), so it is in scope under either wording.

**This repository was blocked in the brownout.** GitHub ran enforcement windows on 2026-11-09 and 2026-11-16,
16:00-18:00 UTC, in which blocked attempts "create failed runs with an error annotation; the workflows won't execute"
(changelog). On 2026-11-16, for one PR's two heads (`gh run list`):

| Time (UTC) | Workflow | Event | Run | Result |
|---|---|---|---|---|
| 17:31:22 | PR Title Lint | `pull_request_target` | `43919248466` | `startup_failure` |
| 17:31:23 | Impact Review | `pull_request_target` | `43919251251` | `startup_failure` |
| 17:31:26 | CI | `pull_request` | `43919258287` | success |
| 17:32:55 | PR Title Lint | `pull_request_target` | `43919515190` | `startup_failure` |
| 17:35:00 | PR Title Lint | `pull_request_target` | `43919886395` | `startup_failure` |
| 17:35:00 | Impact Review | `pull_request_target` | `43919883743` | `startup_failure` |
| 17:35:01 | CI | `pull_request` | `43919889230` | success |
| 18:03:39 | PR Title Lint | `pull_request_target` | `43924834468` | success (after the window) |

A blocked run has no jobs (`…/runs/43919886395/jobs` → `total_count: 0`): the run fails, but the check never
appears, so the required `Validate PR title` stays expected rather than failing. That head's `success` came from the
18:03 run after the window. The brownout also shows by experiment that `pull_request` is not affected. That
enforcement from 2026-12-02 behaves like the brownout is an **assumption (high)**.

**GitHub's security reason.** "`pull_request_target` runs in the context of the base repository and can access its
secrets. If a workflow executes untrusted code from a fork, that code could compromise your pipeline or steal
secrets" (changelog). "Workflows triggered by `pull_request_target` run with elevated trust: the job receives the
base repository's `GITHUB_TOKEN` and access to repository and organization secrets" (securely-using page).

**GitHub's two ways forward.** Allow the event in an Actions event policy, which can target specific workflow files
(`conditions.workflow_path.include`; REST `POST /repos/{owner}/{repo}/actions/policies`), or, "if the workflow does
not need `pull_request_target`, update it to use a safer event where appropriate, such as `pull_request`"
(securely-using page). Policies exist at repository level (about Actions policies), and the read endpoint answers
for this repository (above). Creating one at repository level on this plan was not tried (it would change settings).

**The action supports `pull_request`, and recommends it.** Its README lists `pull_request` as "(recommended)", says
it "works for pull requests from forks too" because fork PRs "receive a `GITHUB_TOKEN` with read-only permissions,
which is all the action needs to validate the title", and already warns about the 2026-12-02 block. Its caveats:
first-time contributors' runs may need approval, and "since the workflow file comes from the pull request, a
contributor could change it to skip the check for their own PR". The repo uses none of the options that need
`pull_request_target` (`wip` with `statuses: write`, sticky comments).

**`pull_request` semantics that matter here** (events page): the workflow runs from the PR's merge ref
(`refs/pull/N/merge`), so an open PR picks up a changed workflow on its next event without rebasing; fork PRs get a
read-only `GITHUB_TOKEN` and no other secrets; "workflows will not run on `pull_request` activity if the pull request
has a merge conflict". `edited` is a supported activity type. That it fires on a title change is an
**assumption (high)**: the action's README example uses `edited` for exactly this, but the events page doesn't say;
verified on the fix PR itself (see Hidden constraints).

**Bots.** The release PR is opened with the `consigliere-release-bot` App token (`release-please.yml` lines 14-31),
so `pull_request` workflows run for it: #312's CI (`Lint`, `Test ×3`) ran on `pull_request` (`gh pr checks 312`,
2026-11-27). Dependabot PRs get `pull_request` runs with a read-only token, which is enough.

**Fork approval.** Workflow runs from forks need approval for all outside contributors
(`actions/permissions/fork-pr-contributor-approval` → `all_external_contributors`), so a fork's edited copy of the
workflow doesn't run until the maintainer approves it.

**Nothing else depends on the trigger.** Outside `specs/` and `CHANGELOG.md`, `pull_request_target` appears only in
`pr-title-lint.yml:7` and `impact-review.yml:16`; `CONTRIBUTING.md:213` names the check, not its trigger
(`git grep`, 2026-11-27). No workspace template ships a workflow, so `cg sync` is unaffected. cg-extensions has no
title check (`validate.yml` runs on `push`/`pull_request`; its `impact-review.yml` is #255's); cg-extensions-registry
has no workflows (contents API 404).

**Recon checked:** the workflow and its history; branch protection; Actions settings, fork approval and policies;
recent runs; #312's checks; the open PRs' files; #255's analysis (branch `gh-255-impact-review-spoof`, `32f97c7`) and
its decisions; CONTRIBUTING and CLAUDE.md; the two sibling repos' workflows; GitHub's changelog, policy, event and
security docs; the action's README. No existing fix or issue other than #325 and #255 found.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes: the only trigger of a required check is the blocked event | `pr-title-lint.yml:7`; protection; changelog |
| Is the expected behaviour right? | Yes: a required check must report on every PR, and the title must be checked because squash-merge makes it the commit release-please parses | `pr-title-lint.yml:3-5`; `CONTRIBUTING.md:212-214` |
| Is it still current? | Yes: still `pull_request_target` on `main`, no Actions policy, date unchanged since the changelog (enforcement moved from 2026-11-02 to 2026-12-02) | `5c4aa50`; policies API; changelog |
| Is it the right problem? | Yes: the cause is the trigger; the workflow doesn't need any of `pull_request_target`'s extra trust | permissions block; README options |

**Verdict:** valid.

## Options

### Recommended — switch the title check to `pull_request`

- **What:** in `pr-title-lint.yml`, replace `pull_request_target` with `pull_request` and keep everything else: the
  activity types (`opened`, `edited`, `reopened`, `synchronize`), `permissions: pull-requests: read`, the job name
  `Validate PR title` and the action. No `branches:` filter, as today (the required check only matters on PRs into
  `main`, and an unfiltered trigger matches the current behaviour). Update the header comment to say why
  `pull_request`. No change to branch protection, CONTRIBUTING or any Actions setting. A `ci:` PR, size S.
- **Why it's safe:** it moves to the event GitHub itself recommends, so GitHub's reason for the block doesn't come
  back: a fork PR's run gets a read-only token and no secrets, and the job checks nothing out.
- **Trade-off:** today the base branch's copy of the workflow runs, so a PR can't change its own title check. With
  `pull_request` the PR's copy runs, so a PR that edits `pr-title-lint.yml` could make its own check pass (the
  README's caveat). This adds no new hole: any PR can already add a workflow with a job named `Validate PR title`
  (the check is pinned to GitHub Actions, not to a workflow file), the same as for `Lint` and `Test`. Workflow changes
  already get the impact review's extra scrutiny, and a fork's run waits for the maintainer's approval
  (`CONTRIBUTING.md` "Safety"; fork approval above). The title is also in front of the maintainer at merge.
- **Wrong choice when:** the check had to be tamper-proof against PR authors (it isn't today either, see above), or
  the repo wanted the action's `wip` option or a sticky comment on forks, which need `pull_request_target`.
- **Rabbit holes:** pinning the action to a SHA, or reworking the other workflows: separate concerns, not this
  issue. Merging the title check into `ci.yml`: no gain, and `ci.yml` also runs on `push`, where there is no PR title.

### Alternative — allow `pull_request_target` for this one workflow (runner-up)

- **What:** an Actions event policy on the repository that allows `pull_request_target`, targeted at
  `.github/workflows/pr-title-lint.yml` only. No code change.
- **Safety case it would need:** the workflow checks out nothing, runs one action with a read-only token and no
  other secrets, so a run triggered by a fork's PR executes no fork code. That holds while the workflow stays like
  this; the action is pinned by tag (`@v6`), so its code runs with the base repository's trust.
- **Trade-off:** settings-only, minutes to apply, but it keeps the elevated-trust event GitHub is moving away from,
  and the state lives in a setting rather than in the repo, where reviews can't see it. It runs against the direction
  of #255's decision 6 ("don't allow `pull_request_target` again"), even if a workflow-scoped policy wouldn't revive
  `impact-review.yml`. Creating a repository-level policy on this plan is unverified (the REST page notes that
  `evaluate` enforcement needs Enterprise; `active` should not).
- **Wrong choice when:** the workflow change can merge before 2026-12-02, which it can (below).
- **Wins instead if:** the workflow change can't merge in time and PRs must keep merging; then it is a stopgap,
  removed when the change lands. Not needed in practice: the fix PR reports its own check (Hidden constraints), so a
  late fix only makes other PRs wait; and as a stopgap it is best-effort, since creating the policy is unverified.

### Do nothing

Every PR opened or updated from 2026-12-02 waits on a check that never passes. Admins aren't enforced on `main`, so
the owner could merge past it, but that is a bypass on every PR, not a fix. Removing `Validate PR title` from the
required checks drops the only Conventional-Commits enforcement point that release-please relies on
(`pr-title-lint.yml:3-5`). Not credible.

## Hidden constraints

- **The fix PR unblocks itself.** On the fix PR, `pull_request` runs the PR's own copy of the workflow, so it reports
  `Validate PR title` even after 2026-12-02. Before that date the base copy also runs on `pull_request_target`;
  both check the same title. No stopgap policy is needed for the fix to merge.
- **Verify on the fix PR:** the `pull_request` run reports `Validate PR title`, and editing the PR title starts a new
  run (`edited`), which settles the one assumption above.
- **Open PRs after it lands.** A PR's existing result stays on its head commit. Its next event (push, update from
  `main`, title edit) runs the new workflow from the merge ref. A PR with a merge conflict gets no run until the
  conflict is resolved; it can't merge before that anyway.
- **Deadline and duration.** The changelog gives a date, not a time; plan as if it starts at 2026-12-02 00:00 UTC, so
  the fix should be merged by the end of 2026-12-01 UTC. The work: owner's decisions on this analysis; `spec.md`
  (acceptance criteria; plan: none, size S) and the agreement comment; implementation and own review (under an hour);
  CI; the maintainer's impact and correctness review, approval and merge. About one working day of elapsed time
  once decisions are in, so the decisions are needed by 2026-11-29 (a Sunday) to keep two days of buffer.
- **If it is late.** Nothing breaks for the fix itself (above). From 2026-12-02 until it merges, any other PR that
  gets a new head waits on the expected check; heads that already have a result keep it. The fix merging is the
  remedy, so no stopgap is planned: the policy is best-effort (unverified), and merging past the check as an admin
  (admins aren't enforced on `main`) is possible but is a bypass the owner would have to choose per PR. Decision 5.
- **Release in flight.** No open PR touches `.github/` (#312: `.release-please-manifest.json`, `CHANGELOG.md`; #318
  and #324: Go code, docs, specs), so there is no file overlap and nothing to wait for to implement. The only
  interaction is strict up-to-date branches: whichever merges first leaves the others behind. A `ci:` commit is not
  released, so release-please does not regenerate #312 for it, and #312 then needs a clean update from `main`
  (`CONTRIBUTING.md` "Impact review", bot PRs). #312 already waits for #318 and #324, and each of those `feat` merges
  regenerates #312 anyway, so landing the fix before the last of them adds no extra cycle to the release. If #312 is
  still open and gets a new head after 2026-12-02, it needs this fix to pass the title check, and its `impact-review`
  follows #255's decision 6 (the owner's go).
- **Reversibility:** a one-line revert of a workflow trigger.
- **Security:** covered under the recommendation; no secrets, read-only token, nothing checked out.
- **Who else is affected:** contributors see the same check name and result. Fork contributors already wait for
  workflow approval for CI; with `pull_request` the title check waits too, including the re-run after a title edit,
  which is a small new approval chore for the maintainer on fork PRs (today the `pull_request_target` copy runs on
  fork PRs without approval). Workspaces are unaffected (`cg sync` ships no workflow). cg-extensions and the registry have
  nothing to change for this issue.
- **#255 compatibility:** the recommendation allows no `pull_request_target`, so it matches decision 6, and it
  touches neither `impact-review.yml` nor `impact-review`'s source, so #255's App plan is unaffected.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | How to keep `Validate PR title` running after 2026-12-02 | Switch `pr-title-lint.yml` to `pull_request`, same types, permissions, job name and action; no Actions policy allowing `pull_request_target` | open |
| 2 | Size and documents | S: `spec.md` with acceptance criteria, plan: none | open |
| 3 | The PR's own copy of the workflow now runs (a PR could edit its own title check) | Accept: no new hole (a same-named job can be added today), covered by the impact review's workflow scrutiny and fork-run approval; no CONTRIBUTING change | open |
| 4 | When it merges, relative to the 2.0.0 rollout | As soon as it is reviewed, independent of the rollout, ideally before the last of #318 / #324 merges; merged by the end of 2026-12-01 UTC at the latest | open |
| 5 | If it isn't merged by the end of 2026-12-01 UTC | No stopgap: other PRs with new heads wait until it merges (the fix PR reports its own check); no Actions policy, and no admin merge past the check unless the owner chooses one for a specific PR | open |

## Review

**Review 1** — `specs/gh-325-title-check-trigger/analysis.md` @ `92dbc41` (content `5f10237`) · 2026-11-27
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 2 should-fix, 2 nit → fixed in `8e68231`: no firm plan if the fix is late (new "If it is
  late" constraint and decision 5; policy stopgap marked best-effort; admin bypass named); brownout evidence unused
  (brownout runs checked, how a blocked run appears stated); fork title-check runs now need approval (stated); no
  `branches:` filter (stated). Owner's: none. Rejected: none.
- Builder questions: 2, both answered by the fixes above.
- Verified true: workflow lines and permissions; App token for release PRs; changelog quotes, scope and dates; the
  action's README statements; `pull_request` runs from the merge ref, no run on a merge conflict, read-only fork
  token; the fix PR reports its own check; release ordering vs `CONTRIBUTING.md` (bot-PR update from `main`); no
  conflict with #255's decision 6; size S with plan: none; no private data.
- Not verified: `gh` outputs (consistent and plausible only); the about-policies and REST policies pages; whether
  release-please regenerates its PR after a `ci:` commit (from CONTRIBUTING, not its source).

**Review 2** (delta) — @ `8e68231` (content `2733ef8`) · 2026-11-27
- Reviewers: doc-reviewer (same lenses, delta scope)
- Findings: 0 blocker, 0 should-fix, 3 nit → fixed in the next commit (text only, no further round): Question
  wording vs "heads that already have a result keep it"; brownout run IDs labelled (table) with the passing run after
  the window; run fails but the check never appears. Regression check: all four round-1 findings fixed.
- Builder questions: none.
- Verified true: brownout windows and the run times inside the second; blocked runs consistent with
  `startup_failure` and 0 jobs; 2026-11-29 is a Sunday; no contradiction with unchanged text.
- Not verified: `gh` outputs; that enforcement from 2026-12-02 behaves like the brownout (tagged assumption); that
  `pull_request_target` runs on fork PRs without approval today.

Outcome: clean at round 2.

## Verdict

needs clarification: the recommendation is ready and reviewed, but decisions 1-5 are the owner's, and they are
time-bound: needed by 2026-11-29 for the fix to merge by the end of 2026-12-01 UTC.

## Q→A log

- (none yet)
