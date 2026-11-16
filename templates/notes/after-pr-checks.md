# After Creating a PR — Review-Resolution Loop

## Meta

- **Category:** process
- **Tags:** `pr-review-loop`, `pull-requests`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand after opening a pull request, and before delegating PR work to a subagent. Safe to edit for your workspace.

## Summary

After opening a PR, the work isn't done until CI is green and every review comment has been addressed (fixed or answered with reasoning). This note holds the fetch probes, the per-comment outcome taxonomy, the CI-failure handling, the contract to give a subagent that works a PR, and the guardrails. The "MUST enter the loop" rule + the bot/silent-dismissal headline stay inline in `CLAUDE.md`.

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

5. **If CI or review is still pending at session end** — offer the user a `/schedule` agent to resume the loop in an appropriate window (15–30 min for typical Go CI, longer for slow pipelines, ~60 min if waiting for CodeRabbit on a large diff). Name the concrete action in the offer ("resume the review loop on PR #<N>"), not "check on things".

## Delegating the loop to a subagent

A subagent that opens or works a PR doesn't know this note, and tends to stop at "PR opened" or at the first green run, leaving the review comments unanswered. Put this block, verbatim, into every PR-related subagent prompt; paraphrasing drops the exit clause it exists for:

```
PR-loop contract — you MUST NOT exit until ONE of the following is true:
  (a) the PR is ready to merge (step 6 holds) and you have reported that to the supervising session, OR
  (b) you are blocked and have reported the blocker to the supervising session. Waiting on the user
      (a merge, a decision, an approval) counts as blocked.

Each iteration:
  1. Push the current branch.
  2. Poll CI until every check has a terminal state (success, failure, or accepted skip).
  3. Fetch every new comment and review since the last iteration. Classify each: fix / reply with reasoning / escalate.
  4. Apply uncontroversial fixes (lint, type errors, validated suggested diffs) in a new commit. Push.
  5. Reply with reasoning to every finding you reject; never resolve a thread without a commit or a reply.
  6. Re-fetch; repeat from 2 until all CI is green AND every comment is addressed AND no review is
     still pending on the current head:
     - A person or team review that was requested (`gh pr view <N> --json reviewRequests`) and not given
       yet: you are blocked, (b). Report "awaiting review by <who>".
     - A review bot that hasn't posted on the current head (bots don't appear in reviewRequests): wait
       about 5 minutes between re-fetches; if it still hasn't posted 30 minutes after your last push, you
       are blocked, (b).
  7. Report ready-to-merge to the supervisor and stop. Never merge; merging is the user's call.

Never force-push or amend: every round is a new commit. If a comment disagrees with a recorded decision
(decisions.md, an area file), cite the decision in your reply.

After every CI poll, write a checkpoint line to your own output. It does not end your run; only (a) or
(b) does:
  PR #<N> | round <K> | CI: <green|pending|failing checks> | open comments: <count> | last commit: <sha> | next: <action>
```

Where the supervisor can read the subagent's output while it runs (a background subagent's transcript or log), the checkpoint line doubles as a stall signal. Because it comes with every poll, a slow pipeline still produces checkpoints; no checkpoint for several poll intervals means the loop has dropped and needs a nudge. A foreground subagent's output is visible only when it returns, so there is no stall signal then.

The subagent doesn't wait for the merge. The supervising session follows the PR from its report on: it watches the PR until it merges, offers the session-end `/schedule` step (5 above, naming the action: "check whether PR #<N> merged; if so, remove its branch and worktree") if the merge is still pending when the session ends, and removes the branch and worktree once the PR has merged.

## Guardrails

- **Never silently resolve a comment.** Either a commit addresses it or a reply explains the reasoning. A PR timeline without replies is a PR where decisions went unrecorded.
- **Never force-push or amend on a review branch.** Each round of review earns its own commit; the timeline is the review trail.
- **Bot comments (CodeRabbit, Copilot, dependabot) follow the same rule as human comments.** Bots are noisy but not dismissible by default — assess validity, fix or reply.
- **If a comment disagrees with an existing decision** (recorded in `decisions.md` or an area file), the reply should cite the decision. Do not reopen settled ground without user direction.
- **Scope creep gets a reply, not a silent fix.** A correct comment that belongs in another PR is answered as such, ideally with a link to the follow-up issue or todo entry that tracks it.
