# Apply Uncontroversial Review Findings Without Asking

## Meta

- **Category:** process
- **Tags:** `pull-requests`, `code-review`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when a PR Claude opened receives CI / bot / human review findings. Safe to edit for your workspace.

## Summary

When automated or human review posts findings on a PR Claude just opened, the default is **validate first, then apply silently** — small, correct, localised fixes go straight in without a confirmation prompt; only ambiguous, architectural, or scope-expanding findings stop for the user. This note holds the validation checklist, the push-back rule, and the repo-config-wins rule. The headline ("fix automatically, merge only on explicit authorisation") stays inline in `CLAUDE.md`.

## The rule

When CI / CodeRabbit / Copilot / a human posts findings on a PR Claude just opened, **validate first, then apply silently**. For each finding ask:

1. Is it actually correct in this codebase?
2. Is the suggested fix appropriate?
3. Does it conflict with a repo convention?

For findings that pass validation **and** are small + localised + reversible (lint, type errors, obvious bugs with suggested diffs), the order is: **apply → push → re-check → summarise.** No intervening confirmation prompt.

For findings that are wrong, mis-applied, or violate repo conventions, **push back with a reasoning reply** per the PR review loop — don't apply a change just because a bot suggested it.

Stop and ask only for findings that are **ambiguous, require architectural judgment, or expand scope beyond the PR.**

This pairs with the shared-state-authorisation rule: **fix automatically, merge only on explicit authorisation.**

## When rejecting a finding on principle, verify the target repo's own review config first

Before replying *"this is out of scope for our style"*, read the repo's own review configuration — `.coderabbit.yaml` `path_instructions`, `CODEOWNERS`, and the contributor guide. If the finding matches an explicitly in-scope review class, the principled-rejection framing is wrong: the repo owner has codified that this class **is** reviewed.

This workspace's `CLAUDE.md` describes *Claude's* preferences; the target repo's config describes the contract future contributors read. **The target repo's config wins.**
