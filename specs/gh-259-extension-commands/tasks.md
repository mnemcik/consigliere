# Tasks — #259

## Before code

- [x] Analysis, spec, plan on the branch
- [ ] Independent review of spec + plan (L tier), clean round
- [ ] Agreement comment on #259 (spec commit + hashes of `spec.md` and `plan.md`)

## M1 — manifest `commands` and install-time ownership

- [ ] Failing tests for S1–S13
- [ ] Manifest, ledger, `CheckCommands`, install/update/init wiring, `extension list`
- [ ] Mutation checks for S6–S11
- [ ] Docs
- [ ] `make check`; independent review

## M2 — dispatch by command table

- [ ] Failing tests for S14–S19, S31–S33
- [ ] Table, pinned-manifest helper (shared with `findLauncher`), errors, hint
- [ ] Mutation checks for S16–S18, S32
- [ ] `make check`; independent review

## M3 — the session carries its workspace

- [ ] Failing tests for S20–S30
- [ ] `CG_WORKSPACE` at start; precedence; worktree test; `CG_EXTENSION_WORKSPACE`
- [ ] Mutation checks for S23, S26, S28
- [ ] Docs; `make check`; independent review

## M4 — the session records its worktree

- [ ] Failing tests for S34–S37
- [ ] State key, `worktree create/remove`, `set-context --worktree`, `CG_SESSION_WORKTREE`
- [ ] Docs; `make check`; independent review

## PR

- [ ] Final independent review over the whole PR
- [ ] PR opened, CI green on Linux, macOS, Windows; CodeRabbit handled
- [ ] Cross-repo PRs (registry schema, cg-extensions manifests, `cg-shop`, `cg-tmux`, note)
- [ ] Maintainer merges; release 2.0.0; X5 end to end
