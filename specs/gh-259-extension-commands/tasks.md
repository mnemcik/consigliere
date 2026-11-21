# Tasks — #259

## Before code

- [x] Analysis, spec, plan on the branch
- [ ] Independent review of spec + plan (L tier), clean round
- [ ] Agreement comment on #259 (spec commit + hashes of `spec.md` and `plan.md`)

## M1 — manifest `commands` and install-time ownership

- [ ] Failing tests for S1–S15
- [ ] Manifest v2, ledger, `CheckCommands`, pinned-manifest helper, install/update/init wiring, `extension list`, interim dispatch
- [ ] Mutation checks for S4, S6–S12
- [ ] Docs
- [ ] `make check`; independent review

## M2 — dispatch by command table

- [ ] Failing tests for S16–S21, S34–S36
- [ ] Table, per-root matching, stop-on-unverified, errors, hint
- [ ] Mutation checks for S18–S20, S35
- [ ] `make check`; independent review

## M3 — the session carries its workspace

- [ ] Failing tests for S22–S33
- [ ] `CG_WORKSPACE` at start; precedence; worktree test; `CG_EXTENSION_WORKSPACE`
- [ ] Mutation checks for S25, S27–S29, S31
- [ ] Docs; `make check`; independent review

## M4 — the session records its worktree

- [ ] Failing tests for S37–S42
- [ ] State key, `worktree create/remove`, `set-context --worktree`, `CG_SESSION_WORKTREE`
- [ ] Mutation checks for S38, S41
- [ ] Docs; `make check`; independent review

## PR

- [ ] Final independent review over the whole PR
- [ ] PR opened, CI green on Linux, macOS, Windows; CodeRabbit handled
- [ ] Cross-repo PRs (registry schema, cg-extensions manifests, `cg-shop`, `cg-tmux`, note)
- [ ] Maintainer merges; release 2.0.0; X5 end to end
