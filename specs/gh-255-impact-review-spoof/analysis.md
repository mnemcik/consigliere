---
title: "Analysis — impact-review can be satisfied without the maintainer (#255)"
created: 2026-11-27
item: "https://github.com/mnemcik/consigliere/issues/255"
size: full
verdict: needs clarification (owner decisions 1-10)
---

# Analysis — impact-review can be satisfied without the maintainer (#255)

- **Item:** https://github.com/mnemcik/consigliere/issues/255
- **Size:** full (several decisions; touches branch protection in three repos, two workflows, CONTRIBUTING and the
  maintainer-side tooling)
- **Status:** analysis only, waiting on the owner. No spec, plan or code.
- **Evidence base:** consigliere `origin/main` at `5c4aa50` (2026-11-27); cg-extensions
  `.github/workflows/impact-review.yml` on `main` (read 2026-11-27 via the contents API); branch protection, Actions
  fork-approval policy, repo settings and collaborators of all three repos read with `gh api` on 2026-11-27; the
  `pr-workflow` plugin 0.3.0 (`pr-intake` skill); GitHub docs read 2026-11-27 (the pages show no date; REST pages at
  API version 2026-03-10): branch protection REST
  (https://docs.github.com/en/rest/branches/branch-protection), about protected branches
  (https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches),
  commit statuses REST (https://docs.github.com/en/rest/commits/statuses).

## Question

`impact-review` is the required check that records the maintainer's impact review. The issue reports two ways to turn
it green without that review: (1) a check run of the same name from a workflow the PR adds, since the required check
accepts any source; (2) the bot exemption in `impact-review.yml`, which keys on who *opened* the PR, so a person's
push to a bot's branch also gets `success`. It also asks to analyse a separate identity for agent sessions, so a
status or agreement from the owner's account is provably the owner's.

This is a **"how"** item: both paths are real today (below), and the issue already names a direction (a dedicated
GitHub App as the pinned source). The questions are whether that direction is the right one, what it costs, what
happens to the bot exemption, and in what order.

## Current state (evidence)

**The gate.** `impact-review` is a commit status on the PR head (CONTRIBUTING.md "Impact review", lines 191-206 at
`5c4aa50`). The maintainer side posts it with `gh api -X POST repos/<repo>/statuses/<sha> -f context=impact-review`
under the owner's account; on #323's head it was created by `mnemcik`, on the release PR #312's head by
`github-actions[bot]` (the bot exemption) — both read with `gh api …/commits/<sha>/statuses`.

**Branch protection** (`gh api repos/mnemcik/<repo>/branches/main/protection`, 2026-11-27):

| Repo | `impact-review` source | Other required checks | Required approvals | Fork workflow approval | Auto-merge |
|---|---|---|---|---|---|
| consigliere | `app_id: null` (any source) | Lint, PR title, Test ×3, pinned to GitHub Actions (15368) | 1 | all external contributors | allowed |
| cg-extensions | `app_id: null` | `manifests` (Actions) | 1 | first-time contributors only | off |
| cg-extensions-registry | `app_id: null` | none | **0** | first-time contributors only | off |

All three: strict (up to date), admins not enforced, no rulesets. The only collaborator in each is the owner (admin).

**GitHub's matching rules** (docs, read 2026-11-27): "Required status checks can be checks or commit statuses"; "you
can select an app that has recently set this check as the expected source … If the status is set by any other person
or integration, merging won't be allowed"; with "any source" you "can still manually verify the author of each
status, listed in the merge box" (about protected branches). The REST field `checks[].app_id` is "The ID of the GitHub
App that must provide this check" (branch protection REST).

**Path 1 is real.** With `app_id: null`, a check run named `impact-review` (a workflow job of that name, from the PR's
own workflow file on a `pull_request` run) satisfies the required check. Pinning to GitHub Actions would not help:
the PR's own workflows also run as GitHub Actions (the same reason #234's "Known limit" gave for the previous check,
`Upstream proposal reviewed`). Today it is held off by review (CONTRIBUTING line 182 rejects such a job) and, in
consigliere only, by approval of every outside contributor's workflow runs. In cg-extensions and the registry a
returning contributor's workflows run without approval. In the registry the gate is the only required check and no
approval is required, so a spoofed `impact-review` leaves only the merge click (which needs write access).

**Path 2 is real.** `impact-review.yml` (consigliere, lines 15-46 at `5c4aa50`) runs on `pull_request_target`
(`opened`, `reopened`, `synchronize`) and posts `success` when `github.event.pull_request.user.login` is one of
`consigliere-release-bot[bot]`, `dependabot[bot]`, `renovate[bot]`, with `github.token`. It never looks at who pushed
(`github.event.sender`). Any push to a bot's branch by someone with write access gets `success`. cg-extensions has the
same workflow without the release bot (diffed 2026-11-27); the registry has no workflows. CONTRIBUTING line 204 asks
people not to push to bot branches; nothing enforces it.

**The workflow's trigger stops by default on 2026-12-02.** GitHub "will block `pull_request_target` events by
default in all public repositories" unless an Actions event policy allows them; enforcement moved from 2026-11-02 to
2026-12-02, after two brownouts on 2026-11-09 and 2026-11-16, and private repositories aren't affected
(https://github.blog/changelog/2026-11-02-pull_request_target-blocked-by-default-december-2, read 2026-11-27; the
page is labelled "Retired"; the date change is an editor's note on the execution-protections GA post,
https://github.blog/changelog/2026-09-17-workflow-execution-protections-in-github-actions-generally-available/).
Allowing it again means an event policy, scoped to specific workflow files. The workflow still ran on 2026-11-27
(a `success` run at 11:21 UTC, `gh run list --workflow impact-review.yml`). So, unless someone allows the event,
**path 2 closes by itself on 2026-12-02, and with it the bot exemption**: from then on no workflow posts
`impact-review` on bot PRs, including the release PR, and they wait for a status nobody posts until the maintainer
side posts it. Whether execution protections are available on repositories owned by a personal account is not
stated on either page (**unverified**).

**Admins.** `enforce_admins` is off in all three repos, so an admin can merge past a missing `impact-review`. Every
author and maintainer session acts as the owner's admin account, so the gate, however its source is pinned, doesn't
stop an author session from merging past it; only the rules do (CONTRIBUTING line 154).

**What the 2026-11-26 PR-flow changes changed.** #317 (CONTRIBUTING by role) wrote down that the author "never sets
`impact-review`, approves or merges its own PR, even under the maintainer's account" (line 154) and that "Any account
or app with write access to statuses technically can" set it (line 203). #315 turned CodeRabbit's automatic review off
(`.coderabbit.yaml` `auto_review.enabled: false`), so its approval now comes only on request. #313 retired the
upstream-proposal process. None of them changes either path: they make the rule explicit, not enforced. They do
sharpen the identity point: every author and maintainer session acts as the same admin account, so the status's
`creator` can't tell the maintainer side from an author session.

**Bot PR volume** (merged, all time, `gh pr list --state merged --limit 200`): consigliere 35 release-bot, 18
Dependabot, 1 Renovate; cg-extensions and the registry none. The maintainer side already acts on every bot PR it
merges: CodeRabbit skips bot PRs, so the approval is the maintainer side's `@coderabbitai approve` (`pr-intake`
skill, "Other bot PRs" and "Release PRs").

**Who reads the gate.** The `pr-intake` skill treats "the gate status is on that head" as a readiness condition and
sets it itself (SKILL.md lines 55, 69, 73). It doesn't say how it checks the status's source.

**Recon also checked:** existing analyses in `specs/` (shape only); `grep -ri forgejo` over the repo (nothing: no
Forgejo use in this repo); the release bot App (`consigliere-release-bot`, key in an Actions secret for
release-please) as a reusable source (rejected below: its key is readable by workflows).

## Validity

- **Does the problem exist?** Yes, both paths, traced above against today's workflow and protection.
- **Is the expected behaviour right?** Yes: CONTRIBUTING says only the maintainer (or someone with its
  authorisation) sets the status for non-exempt PRs; the gate should enforce what the text promises.
- **Still current?** Yes (path 2 until 2026-12-02, unless `pull_request_target` is allowed again). #234 left it as a known limit; #313/#315/#317 did not touch it. Not a duplicate.
- **Right problem?** Partly reframed: the cause is that the gate accepts **any source** and that the exemption trusts
  the PR author instead of the pusher. Fixing only the workflow's `if:` closes path 2 and leaves path 1. The identity
  question is a separate problem (who acts), which the App makes solvable but does not solve.
- **Verdict: valid.** Severity today is limited: the owner is the only collaborator, consigliere approves every
  outside workflow run, and merging needs write access. The gate is still not the control it is described as.

## Options

### Recommended — a dedicated GitHub App is the only source of `impact-review`; the workflow exemption goes

1. The owner creates a GitHub App (owned by the owner's account, no webhook, one repository permission: commit
   statuses read/write) and installs it on the three repos.
2. Its private key stays outside GitHub: in the owner's password manager, never in an Actions secret. The maintainer
   side mints a short-lived installation token from it and posts `impact-review` with that token instead of the owner's
   account.
3. Branch protection pins `impact-review` to the App's ID in all three repos.
4. The `impact-review.yml` workflow is removed from consigliere and cg-extensions, and the maintainer side sets the
   status on bot PRs too, after the check CONTRIBUTING already requires (the head adds nothing beyond `main` and the
   bot's own change). **Done by the interim change (decision 6)**, which lands first; if it hasn't landed when the
   App work starts, the App change takes it over.
5. CONTRIBUTING (and the cg-extensions README section it mirrors) say the status counts only from the App; the
   `pr-workflow` plugin's gate step posts through the App. (The bot-exemption wording goes in the interim change.)
6. cg-extensions and the registry require approval of workflow runs for all outside contributors, as consigliere
   already does (a settings change, independent of the rest).

- **Closes:** path 1 (a check run or status from any other source no longer satisfies the check — "If the status is
  set by any other person or integration, merging won't be allowed"); path 2 (no workflow posts the status at all).
  Any write-access account that can't reach the key (a future contributor, or an agent's own account on
  GitHub) can't set the gate either.
- **Trade-off:** the maintainer side does one more step per bot PR (about 50 so far in consigliere, mostly releases,
  which already need the owner's "merge it"); a key to keep and rotate; minting a token needs a small script (`gh` has
  no built-in for App tokens).
- **Wrong choice when:** bot PR volume grows to where a manual status per Dependabot PR is a real burden, or the
  maintainer side moves to a machine that can't reach the key.
- **Rabbit holes:** writing a general App-token tool (keep it to "mint, post one status"); trying to keep an automatic
  bot exemption with the App's key (see the alternative); per-repo Apps (one App installed on three repos is enough).
- **Does not close, against sessions under the owner's account:** today it adds nothing there. Such a session can
  merge past the gate as an admin (`enforce_admins` off), and, if it can read the key, mint a token. Either the
  long-running maintainer side reads the key without a prompt (unattended gating and merging keep working; any other
  session on that machine could read it too), or each read needs the owner's unlock (author sessions can't use it
  silently; unattended merges stall). Which one is decision 8. The App's value against the owner's own sessions comes
  with the identity question (decision 4): an author account without admin and without the key.

### Alternative — App pinned, bot exemption kept in a workflow (runner-up)

Same App and pinning, but a `pull_request_target` workflow keeps posting `success` for bot PRs, with the App's key in
an Actions environment restricted to `main`, and the check moves to `github.event.sender.login` in both the job's
`if:` and the step. A clean update from `main` keeps `success` only if the workflow can prove the new head is
`main` merged into the previous head (parents check plus an unchanged PR diff).

- **Trade-off:** no manual step for bot PRs; but the key becomes readable by any workflow that runs on `main` with
  that environment, and the clean-update proof is new logic in a privileged workflow.
- **Wrong choice when:** bot PRs stay at today's volume (the manual step is cheap). Since 2025-12-08, environment
  branch rules for `pull_request_target` are checked against the default branch
  (https://github.blog/changelog/2025-11-07-actions-pull_request_target-and-environment-branch-protections-changes/),
  so the restriction to `main` holds; but from 2026-12-02 this alternative also needs `pull_request_target` allowed
  for that workflow by an event policy (see Current state).
- **Rabbit holes:** the clean-update proof (tree equality needs a merge; approximations leak).
- **Wins instead if** Dependabot PRs become frequent enough that the maintainer side setting each status is a burden.

### Alternative — smallest change: fix path 2 only, tighten workflow approval

Keep any source. Change the workflow to require `github.event.sender.login` to be the bot (job `if:` and step);
a person's update from `main` gets no status and the maintainer side re-sets it. Require approval of all outside
contributors' workflow runs in cg-extensions and the registry.

- **Trade-off:** a few lines, no new infrastructure, can land soon. Path 1 stays open for anyone whose workflows run
  without review (any write-access account, including every session under the owner's account), and the
  registry's gate still has no second line.
- **Wrong choice when:** the gate is meant to hold against anything but an honest author. Also needs
  `pull_request_target` allowed from 2026-12-02 to keep working at all.

### Considered and rejected

- **CODEOWNERS review as the gate:** the owner is the only code owner, and GitHub doesn't let an author approve
  their own PR; every agent PR is opened under the owner's account, so it could never be satisfied.
- **Required deployments with environment reviewers:** no key to manage, but a deployment status can be created
  through the API by any write-access account, so whether it resists the same spoofing is unverified; more moving
  parts than a pinned status.
- **The App posts a check run instead of a commit status:** the same pinning effect; a status is what the flow
  already uses.
- **Rulesets instead of branch protection:** the same per-check app pinning (`integration_id`), nothing extra here.
- **Workflow execution protections (actor and event rules)** as a second line against path 1: they could limit who
  can trigger PR workflows, but availability on personal-account repositories is unverified, and pinning already
  closes path 1.

### Do nothing

The owner is the only collaborator and consigliere approves outside workflow runs. Credible only while the repos have
no other writers; it leaves cg-extensions and the registry open to returning contributors' workflows, and leaves
CONTRIBUTING describing a control that isn't one. Not recommended.

## Hidden constraints

- **Order of the cutover.** Pinning makes every existing owner-posted status stop counting. Open PRs at cutover
  need their status re-posted through the App. Create and install the App, post one status with it, then pin
  (the docs describe choosing "an app that has recently set this check"; whether the API accepts an App that has
  never posted is **unverified**, assumption med — post first and the question doesn't arise), passing the App's ID
  explicitly (leaving `app_id` out lets GitHub pick "the GitHub App that has recently provided this check"). The
  workflow is already gone by then (interim change, decision 6); if not, remove it with the pin, since its
  `success` no longer counts once pinned. One App, pinned in all three repos in one go;
  the token-minting script (below) must work before the pin.
- **2026-12-02, before any of this lands.** Unless `pull_request_target` is allowed, the workflow stops then. From
  that day the maintainer side posts `impact-review` on bot PRs by hand (under the owner's account until the App
  exists), after the check CONTRIBUTING line 204 already asks for; the release PR #312 needs this on any new head.
  No rule covers that yet: `pr-intake` 0.3.0 sets the gate only "when the agreement covers it" (SKILL line 55),
  covers release PRs through the owner's "merge it" (line 75), and for other bot PRs waits for a status "often by the
  repo's own workflow" (line 76); CONTRIBUTING lines 195 and 204 still say the workflow sets it. So, until an interim
  change lands, a Dependabot or Renovate PR needs the owner's go for its status, like a release PR. The interim
  change is small and separate from the App: delete the workflow in consigliere and cg-extensions; in CONTRIBUTING,
  rewrite every sentence that rests on the exemption (lines 193 "exempt", 195 "the workflow sets it … for the bots",
  203 "PRs that aren't exempt", 204 "exempt by who opened them") so the maintainer side sets it on bot PRs after the
  line-204 check, and the same in the cg-extensions README; and add a bot-PR rule to `pr-intake`. This is decision 6.
- **Release in flight.** cg 2.0.0 (#312) is held on #318, #324 and the step-4 / step-5 PRs. The cutover changes the
  gate under those PRs, and removing the workflow is a `ci:` change that `release-please` doesn't release (it leaves
  the release branch behind). The implementation waits until the 2.0.0 rollout is finished (step 5 merged). Open PRs
  read 2026-11-27 (#312, #318, #324) touch neither `.github/` nor CONTRIBUTING, so there is no file overlap.
- **Reversibility.** Fully reversible: re-set `app_id` to any source and restore the workflow from history. Failure
  mid-way (pinned, but the maintainer side can't mint a token): no PR can merge except by an admin, who isn't
  enforced; unpin to recover. Unpinning is an admin edit to branch protection, which `enforce_admins` doesn't
  affect, so the recovery stays either way. Turning `enforce_admins` on blocks only an admin's merge past a missing
  gate; an admin session can still change protection itself (decision 9).
- **Security of the key.** Not in a repo, not in Actions, not in CI. Rotation: generate a new key in the App settings,
  delete the old one. A leaked key lets its holder set the gate only (one permission); the merge still needs
  write access and, in two repos, an approval.
- **Where the token minting lives.** It is maintainer-side tooling, not cg core: the `pr-workflow` plugin's gate step
  (and later the cg `pr-workflow` extension, cg-extensions#45 and #46, which this issue softly blocks). The key's
  location is user configuration there, never a value in a repo.
- **Docs.** CONTRIBUTING lines 151-154, 182, 195-204 (the "Who sets it" and "Bot PRs are exempt" bullets are 203-204) and the cg-extensions README's equivalent change; the "Known
  limit" disappears.
- **Who else is affected.** Outside contributors: nothing changes for them (they never set the status). Bots: their
  PRs wait for the maintainer side's status, which they already wait on for the approval.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | What makes `impact-review` trustworthy? | A dedicated GitHub App, the only accepted source in all three repos, its key outside GitHub | open |
| 2 | What happens to the bot exemption? | Remove the workflow; the maintainer side sets the status on bot PRs after its existing check | open |
| 3 | Require approval of all outside contributors' workflow runs in cg-extensions and the registry? | Yes, now (a settings change, independent of the rest) | open |
| 4 | The agent-identity question (a separate account for agent sessions, on every forge) | Split into its own issue, analysed after the App lands; the App is its prerequisite | open |
| 5 | Scope of this issue | All three repos under this issue; the cg-extensions workflow and README change as a PR there referencing it | open |
| 6 | `pull_request_target` from 2026-12-02, and bot PRs until the App | Don't allow it; let the workflow stop (path 2 closes by itself). Until an interim change lands, a bot PR's status needs the owner's go, like a release PR. The interim change (workflow deleted in both repos, CONTRIBUTING's exemption wording rewritten at lines 193, 195, 203 and 204 and the same in the cg-extensions README, a bot-PR rule in `pr-intake`; see Hidden constraints, "2026-12-02") lands right after the 2.0.0 rollout, ahead of the App | open |
| 7 | Timing of the rest | After the cg 2.0.0 rollout is finished (step 5 merged); the token script ready before the pin | open |
| 8 | How the maintainer side reaches the App key | Readable by the long-running maintainer side without a prompt (unattended merges keep working); accepted that same-machine sessions could read it until decision 4 | open |
| 9 | `enforce_admins` | Leave off for now: on, it only stops a casual admin merge past the gate, while an admin session can still edit protection; the owner-account sessions are decision 4's problem | open |
| 10 | Size | Interim change S; the App change M (one milestone: App + pinning + docs; the plugin change is in its own repo) | open |

## Review

**Review 1** — `specs/gh-255-impact-review-spoof/analysis.md` @ content hash `1b596c9` (uncommitted) · 2026-11-27
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: not applicable (analysis)
- Findings: 1 blocker, 3 should-fix, 2 nit → fixed: `pull_request_target` blocked by default from 2026-12-02
  (verified at the changelog; new Current-state paragraph, cutover step, decision 6); admin bypass (Current state,
  decision 9); how the key is reached and what the App adds against owner-account sessions (decision 8); rejected
  options recorded with reasons; trigger types; explicit App ID in the cutover. Dropped in validation: none.
- Builder questions: 5 → all folded into decisions 6-9 and the cutover step (order: one App, all three repos pinned
  in one go; the token script ready before the pin).
- Verified true: workflow behaviour; CONTRIBUTING line references; `.coderabbit.yaml`; #234's known limit;
  pr-intake's gate handling; GitHub's required-check source semantics; environment branch rules for
  `pull_request_target` checked against the default branch.
- Not verified: live settings and bot counts (author's `gh api` reads); #313/#317 diffs; the App "commit statuses"
  permission name; pinning an App that never posted; execution protections on personal-account repos.

**Review 2** — same path @ content hash `65996e8` (uncommitted), delta from `1b596c9` · 2026-11-27
- Reviewers: doc-reviewer (delta: regression check of review 1, new problems)
- Findings: 0 blocker, 2 should-fix, 1 nit → fixed: decision 6 left bot PRs and CONTRIBUTING without a rule from
  2026-12-02 (interim change and owner's-go rule added); decision 9's reason was wrong (`enforce_admins` doesn't
  affect unpinning); Validity dates path 2. Dropped in validation: none.
- Builder questions: 1 → answered by the decision 6 fix.
- Verified true: review 1's six findings each resolved; the `pull_request_target` changelog facts; decision
  numbering and cross-references consistent.
- Not verified: the 2026-11-27 workflow run and live `enforce_admins` (author's `gh` reads); deployment statuses
  creatable by any write-access account.

**Review 3** — same path @ content hash `20f9894` (uncommitted), delta from `65996e8` · 2026-11-27
- Reviewers: doc-reviewer (delta)
- Findings: 0 blocker, 2 should-fix, 0 nit → fixed: Recommended steps 4-5, the cutover and decision 10 still owned
  the workflow removal that decision 6 moved to the interim change (now marked, with the fallback if the interim
  change hasn't landed); the interim CONTRIBUTING edit named only line 204 (now 193, 195, 203, 204 and the
  cg-extensions README).
- Builder questions: 1 → answered by the fallback in step 4.
- Verified true: review 2's findings resolved; SKILL lines 55/75/76 and CONTRIBUTING 195/204 citations; decision 6
  and 7 timing agree; nothing private, no exploit steps.
- Not verified: the cg-extensions README text.

**Review 4** — same path @ content hash `eb82f0d` (uncommitted), delta from `20f9894` · 2026-11-27
- Reviewers: doc-reviewer (delta)
- Findings: 0 blocker, 1 should-fix, 0 nit → fixed: decision 6's table row still named one bullet (now the four
  lines and the cg-extensions README, pointing to the cutover bullet). A one-cell wording fix that copies the body's
  already-reviewed list.
- Builder questions: none.
- Verified true: review 3's findings resolved; the four quoted CONTRIBUTING fragments match; no other CONTRIBUTING
  line rests on the exemption; the Review 3 record agrees with the body.
- Not verified: the cg-extensions README text.

**Review 5** — same path @ content hash `d650014` (uncommitted), delta from `eb82f0d` · 2026-11-27
- Reviewers: doc-reviewer (delta)
- Findings: none → **clean**. Review 4's finding resolved; the row matches the body and its cross-reference
  resolves; the Review 4 record agrees with the body.
- Builder questions: none.
- Not verified: as in review 4.

The later edits (this record and the Verdict) change no analysis content.

## Verdict

needs clarification: the recommendation is ready, but decisions 1-10 are the owner's, and decision 6 is time-bound
(GitHub stops the workflow's trigger on 2026-12-02). Nothing is implemented before the owner decides and agrees a
spec on the issue.

## Q→A log

- (none yet)
