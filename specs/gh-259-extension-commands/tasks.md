# Tasks — #259

## Before code

- [x] Analysis, spec, plan on the branch
- [x] Independent review of spec + plan (L tier): rounds 1–6 (round 6 clean), rounds 7–8 after the owner's triage (round 8 clean)
- [x] Agreement comment on #259 (spec commit + hashes of `spec.md` and `plan.md`)

## M1 — manifest `commands` and install-time ownership

- [x] Failing tests for S1–S14
- [x] Manifest v2, ledger, `CheckCommands`, pinned-manifest helper, install/update/init wiring, `extension list`, interim dispatch
- [x] Mutation checks for S5–S11
- [x] Docs
- [x] `make check`; independent review (round 1: 2 fixed, 3 to the owner; round 2 clean)

## M2 — dispatch by command table

- [x] Failing tests for S15–S19, S28/S29 (`CG_COMMAND` parts), S30, S32–S34
- [x] Table, per-root matching, stop-on-unverified, errors, hint
- [x] Mutation checks for S16–S18, S29 (`CG_COMMAND` strip), S33
- [x] `make check`; independent review (5 rounds; round 5 clean)

## M3 — the session carries its workspace

- [ ] Failing tests for S20–S31
- [ ] `CG_WORKSPACE` at start; precedence; worktree test; `CG_EXTENSION_WORKSPACE`
- [ ] Mutation checks for S23, S25–S27, S29, S31 (strip), and the cross-root S16/S17 tests
- [ ] Docs; `make check`; independent review

## M4 — the session records its worktree

- [ ] Failing tests for S35–S40, S38b
- [ ] State key, `worktree create/remove`, `set-context --worktree`, `CG_SESSION_WORKTREE`
- [ ] Mutation checks for S36, S38, S38b, S39
- [ ] Docs; `make check`; independent review

## PR

- [ ] Final independent review over the whole PR
- [ ] PR opened, CI green on Linux, macOS, Windows; CodeRabbit handled
- [ ] Cross-repo PRs (registry schema, cg-extensions manifests, `cg-shop`, `cg-tmux`, note)
- [ ] Maintainer merges; release 2.0.0; X5 end to end
