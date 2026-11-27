# Spec — the title check runs on `pull_request` (#325)

Implements the owner's decisions 1–5 on [analysis.md](analysis.md#open-decisions) (agreed on the issue:
https://github.com/mnemcik/consigliere/issues/325#issuecomment-7224607598). Size **S**: this spec only, plan: none.
Each criterion below is one observable result and says how it is shown. Q1–Q2 at the end are gaps the decisions don't
settle; each has a proposed answer for the owner's agreement.

Written against `main` at `5c4aa50`.

## Terms

- **The workflow:** `.github/workflows/pr-title-lint.yml` (name `PR Title Lint`).
- **The check:** the check run named `Validate PR title`, required on `main` and pinned to GitHub Actions.
- **The fix PR:** the PR that carries this change (`Fixes #325`).
- **Branch PR / fork PR:** a PR whose head branch is in `mnemcik/consigliere` / in a fork of it.

## The change

In the workflow, and nowhere else:

- **C1** The trigger `pull_request_target` becomes `pull_request`, with the same activity types: `opened`, `edited`,
  `reopened`, `synchronize` (decision 1). No `branches:` or `paths:` filter (decision 1).
- **C2** Everything else stays: `permissions: pull-requests: read`, the job id `validate`, the job name
  `Validate PR title`, `runs-on`, the action `amannn/action-semantic-pull-request@v6` and its `GITHUB_TOKEN` env.
  Nothing is checked out and no other secret is used.
- **C3** The header comment keeps what the check is for (squash merge, release-please, the single
  Conventional-Commits enforcement point) and adds, in a sentence or two, why the trigger is `pull_request`: GitHub
  blocks `pull_request_target` in public repositories from 2026-12-02, and the action needs only a read-only token,
  which fork PRs get on `pull_request`.

Not changed (decisions 1, 3): no Actions event policy; branch protection, required checks, `CONTRIBUTING.md`,
`impact-review.yml` and every other workflow stay as they are.

## Acceptance criteria

- **A1 — reports on a branch PR.** On the fix PR (a branch PR), the head commit has a `Validate PR title` check run
  with conclusion `success`, produced by a run of the workflow whose event is `pull_request`. Shown on the PR with the
  run's URL and event (`gh run list --workflow pr-title-lint.yml --event pull_request --commit <head>`).
- **A2 — still validates the title.** The `pull_request` run fails on a title that isn't a Conventional Commit and
  passes again on a valid one. Shown on the fix PR: change the title to an invalid one (for example `Title check on
  pull_request`), see the `edited` run fail, restore the `ci: …` title, see the next `edited` run pass. This also
  shows that an `edited` event starts a run (the analysis's one open assumption). Both run URLs go on the PR.
- **A3 — reports on a fork PR.** A fork PR gets the check from the `pull_request` run once the maintainer approves
  the fork's workflow run (fork approval is required for all outside contributors), with a read-only token that is
  enough for the action. How this is shown: Q1.
- **A4 — reports under the 2026-12-02 conditions.** The check comes from the `pull_request` event alone, so it
  reports when `pull_request_target` is blocked. How this is shown: Q2.
- **A5 — no other workflow changed.** The PR's diff against `main` touches only `.github/workflows/pr-title-lint.yml`
  and files under `specs/gh-325-title-check-trigger/`. Shown with `git diff --stat origin/main...<head>` in the PR
  description. Within `pr-title-lint.yml`, the diff is C1 and C3 only (C2 unchanged).
- **A6 — the file is valid.** The workflow parses as YAML and `actionlint` reports nothing for it (if actionlint
  isn't available, GitHub accepting the workflow and running it in A1 is the evidence). The repo's CI (`Lint`,
  `Test ×3`) is green on the fix PR's head.
- **A7 — the PR itself.** Title `ci: run the PR title check on pull_request` (or another `ci:` title); description
  with purpose, approach, impact (no change on `cg sync`: no template ships a workflow), the calls made within the
  agreement, `Fixes #325`, and the evidence for A1–A6.

## Out of scope

- `impact-review.yml` and its `pull_request_target` trigger (#255).
- Pinning the action to a commit SHA, or any other workflow hardening.
- The behaviour of open PRs after the merge: their next event runs the new workflow from the merge ref, as the
  analysis says; nothing to do for them.

## Questions for the owner (gaps, each with a proposed answer)

- **Q1 — how A3 (fork PR) is shown.** The decisions ask for a check that reports on fork PRs, but a fork PR can't be
  opened from the owner's own account (GitHub doesn't let an owner fork their own repository into the same account),
  and no second account is in scope. **Proposed:** A3 is shown by documented behaviour, not a live fork PR: GitHub's
  events reference (fork PRs run `pull_request` workflows with a read-only `GITHUB_TOKEN` and no secrets, after
  approval) and the action's README (`pull_request` "works for pull requests from forks too"; read-only is "all the
  action needs"). Both are cited in the PR description, and the first fork PR after the merge is the live
  confirmation; if it doesn't report, that is a new issue, not a reason to hold this one.
- **Q2 — how A4 (after 2026-12-02) is shown.** The block can't be switched on before the date, and the brownout
  windows are over. **Proposed:** A4 is shown by A1 (the passing check comes from a run whose event is
  `pull_request`) together with the 2026-11-16 brownout runs in the analysis, where `pull_request` runs were not
  blocked while `pull_request_target` ones were. Until the merge, the fix PR also gets the base branch's
  `pull_request_target` run with the same check name; the evidence names the `pull_request` run explicitly so the
  two aren't confused. Merged before 2026-12-02, the first PR after the date is the live confirmation.
