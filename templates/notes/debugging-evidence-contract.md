# Debugging — Evidence Contract Before Fixing

## Meta

- **Category:** process
- **Tags:** `debugging`, `evidence`, `workflow`
- **Framework note:** shipped by Consigliere and loaded on demand when a non-trivial bug is reported. Safe to edit for your workspace.

## Summary

Before proposing or applying a fix for a non-trivial bug, state three things in the same response: how you reproduced or traced it, the hypothesis with its supporting evidence, and one adjacent hypothesis you ruled out. This is the bug-diagnosis subset of **Evidence Over Inference** (which covers the broader precondition / state-vs-rate / vendor-fact axes). The headline trigger stays inline in `CLAUDE.md`; this note holds the contract.

## Why this exists

The recurring failure mode is a symptom-shaped hypothesis applied without code-path tracing → fix → regression or a second-round fix. It bites hardest on library-internal symptoms (UI / layout / scroll / resize / component-framework / test-harness / auth-client internals) where the cause is rarely the first plausible explanation. Single-hypothesis debugging is the trap: not "guessed wrong from no information" but "found *a* plausible cause and stopped."

## The contract

Before proposing or applying a fix, state in the same response:

1. **Reproduction or trace** — how you reproduced the bug, OR the specific code path you traced (`file:line`). For a library-internal symptom, name the library mechanism you read or grepped.
2. **Hypothesis with evidence** — the cause you believe is operative AND the specific evidence (code snippet, log line, repro outcome, library source line) that supports it. "It's probably X" without a citation is not evidence.
3. **One alternative ruled out** — an adjacent plausible hypothesis you considered and the reason you discarded it. This makes the search at least binary and surfaces cases where two hypotheses are both consistent with the symptom (which almost always means you don't have enough evidence yet).

Only after those three lines, propose the change. If any of the three is missing, say so and either gather it or ask the user before editing.

## Exceptions

- **Trivial bugs** — typo, obviously-wrong constant, a missing `await` on a single line — fix directly; the diff is its own evidence.
- **User explicitly says "just try X"** — you're executing the user's hypothesis, not your own; note that you're doing so without independent evidence.
- **User has already cited the cause** — don't re-derive it; restate the cause briefly and proceed.

## Related

- `CLAUDE.md` → **Evidence Over Inference** — the broader axis (precondition / state-vs-rate / vendor-fact). This note is the bug-diagnosis subset.
- Tool-gotcha notes in your own `notes/` are often where the real library-internal cause turns out to be documented — check them before tracing from scratch.
