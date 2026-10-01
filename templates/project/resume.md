# Resume — {Project Title}

Pickup context for a paused session. Written by `/wrap pause`; **delete it once work has resumed**.

Paused: YYYY-MM-DD

## Cursor

- **Active todo item:** the in-flight line copied from `todo.md`, or `n/a`.
- **Files in flight:** paths currently being edited.
- **Step inside the step:** what was happening *inside* the todo item — "wrote the regex but haven't tested IPv6", "stub of `applyMigration` written, no tests yet".

## Mental context

Assumptions, half-formed hypotheses, and reasoning that will not survive in the codebase. *Why* the current approach was chosen, *what* alternative was rejected, *which* edge case was on your mind.

## Dirty state

- **Branch:** `session/<slug>`
- **WIP commit SHA:** filled in after the pause commit lands (`none — worktree clean before pause` if nothing was committed)
- **Pushed to origin:** yes | no
- **Uncommitted files at pause time:** paths, or `none`

## Next concrete action

A single specific step — not a goal. "Run `bun test integration/auth.test.ts` and read the failure for `accepts expired refresh tokens`", not "make the tests pass".

## Open blockers / questions

Anything stopping progress, or anything to think about away from the keyboard.

## Pause notes

Anything else that doesn't fit above. Keep brief.
