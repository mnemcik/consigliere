# After Creating a PR — Review-Resolution Loop

## Meta

- **Category:** process
- **Tags:** `pr-review-loop`, `pull-requests`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand after opening a pull request. Safe to edit for your workspace.

## Summary

After opening a PR, the work isn't done until CI is green and every review comment has been addressed (fixed or answered with reasoning). This note holds the fetch probes, the per-comment outcome taxonomy, the CI-failure handling, and the guardrails. The "MUST enter the loop" rule + the bot/silent-dismissal headline stay inline in `CLAUDE.md`.

## Procedure

Run the loop immediately after `gh pr create`, and again after each push of follow-up commits, until the exit condition is met.

1. **Fetch state** — three probes plus CI:
   - `gh pr checks <N> --repo <org>/<repo>`
   - Inline comments: `gh api repos/<org>/<repo>/pulls/<N>/comments`
   - Issue comments: `gh api repos/<org>/<repo>/issues/<N>/comments`
   - Review decisions: `gh api repos/<org>/<repo>/pulls/<N>/reviews`

2. **For each open comment, decide one of three outcomes:**
   - **Fix** — comment is valid and the fix is mechanical, local, and low-risk (gofmt, typo, missing null-check, misnamed variable, missing CHANGELOG entry, lint violations, obvious bug). Implement in a follow-up commit on the same branch. Never amend — each review round is its own commit so the PR timeline stays readable.
   - **Reply with reasoning** — comment is valid *as an observation* but Claude judges the fix unnecessary, incorrect, or out-of-scope for this PR. Post a reply explaining *why* — cite the relevant constraint, test, decision, or scope rule. Do not silently dismiss. Endpoint depends on comment type:
     - **Inline PR review comment** (pinned to a file/line) → `POST /repos/{owner}/{repo}/pulls/{N}/comments/{comment_id}/replies`.
     - **Issue-level / PR-thread comment** (the general discussion area) → `POST /repos/{owner}/{repo}/issues/{N}/comments`.
   - **Escalate to user** — reserved for genuinely ambiguous calls: comment touches project direction, requires domain knowledge Claude lacks, or the fix has significant blast radius (API rename, architecture shift, deleting substantial code). Surface it with the comment, Claude's read, and the tradeoff — then wait.

3. **For each failing CI check** — read the log (`gh run view <run-id> --log`), fix, push, re-check. Treat CI failures the same as valid review comments: fix or escalate, never ignore.

4. **Re-run the fetch step after every push** — new fixes often trigger new comments (CodeRabbit re-reviews on each commit). Loop until:
   - All CI checks are green or explicitly accepted skips, AND
   - Every inline and issue comment is either marked resolved, has a follow-up commit addressing it, or has a reasoning reply from Claude.

5. **If CI or review is still pending at session end** — offer the user a `/schedule` agent to resume the loop in an appropriate window (15–30 min for typical Go CI, longer for slow pipelines, ~60 min if waiting for CodeRabbit on a large diff).

## Guardrails

- **Never silently resolve a comment.** Either a commit addresses it or a reply explains the reasoning. A PR timeline without replies is a PR where decisions went unrecorded.
- **Never force-push or amend on a review branch.** Each round of review earns its own commit; the timeline is the review trail.
- **Bot comments (CodeRabbit, Copilot, dependabot) follow the same rule as human comments.** Bots are noisy but not dismissible by default — assess validity, fix or reply.
- **If a comment disagrees with an existing decision** (recorded in `decisions.md` or an area file), the reply should cite the decision. Do not reopen settled ground without user direction.
