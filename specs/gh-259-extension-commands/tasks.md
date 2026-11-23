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

- [x] Failing tests for S20–S31
- [x] `CG_WORKSPACE` at start; precedence; worktree test; `CG_EXTENSION_WORKSPACE`
- [x] Mutation checks for S23, S25–S27, S29, S31 (strip), and the cross-root S16/S17 tests
- [x] Docs; `make check`; independent review (4 rounds; round 4 clean, its code-review nits fixed in M4's commit)

## M4 — the session records its worktree

- [x] Failing tests for S35–S40, S38b
- [x] State key, `worktree create/remove`, `set-context --worktree`, `CG_SESSION_WORKTREE`
- [x] Mutation checks for S36, S38, S38b, S39
- [x] Docs; `make check`; independent review (3 rounds; round 3 clean)

## Amendments A–F (after the new agreement)

- [x] S9: owned-command exemption with a warning (ownership from the installed version's manifest only, per command; `cg init` only warns), test-first, with the mutation checks plan.md lists
- [x] S2: command words match `[a-z0-9][a-z0-9-]*`, test-first
- [x] S25: stop branch for every decode case of the session `.cg.json` (directory, dangling symlink, empty, invalid JSON, `null`, non-object, any field of the wrong type, decoded as cg reads `.cg.json` elsewhere), from S20, S22 and S24, and the S21 exemption, test-first, with the mutation checks plan.md lists
- [x] S36/S38: the main checkout refused by `set-context --worktree` and ignored as a recorded worktree (then S38b applies), test-first, with mutation checks
- [x] S36/S38: paths compared after resolving symlinks on both sides; a symlinked path to a linked worktree accepted, test-first, with a mutation check
- [x] S38b: a recorded main checkout counts as "no valid worktree recorded", test-first
- [x] S14: list wording (a built-in-namespace command can't run)
- [x] S35: `cg worktree create` records only worktrees of the session's workspace repo (`CG_WORKSPACE`'s, else the current workspace's); test-first: create in another repo leaves the record unchanged
- [x] S35: the session repo is the session root's (`CG_WORKSPACE` unless S25 ignores it, else the cwd's workspace root); a worktree belongs to it when the session root's cleaned-env `git worktree list --porcelain` lists it and it isn't the main checkout, compared after resolving symlinks; test-first, with mutation checks: no session repo (no valid `CG_WORKSPACE`, cwd outside any workspace) records nothing; a symlinked session root still matches
- [x] S36: `set-context --worktree` refuses a path that isn't one of the session repo's linked worktrees, test-first
- [x] S37: `remove` clears only the same path (compared as in S35); removing a worktree of another repo leaves the record, test-first, with a mutation check

## Amendment H (after the new agreement)

- [ ] S6: install and update fail before anything is applied when another installed extension's tree at its recorded commit can't be read, naming that extension and `cg init` as the fix; guard test with a mutation check, for install and update
- [ ] Session repo root exception (Terms): the cwd root is the session repo root when it is another Consigliere workspace (not the session root and not one of its linked worktrees, decided as in S21–S23)
- [ ] S38c: session commands (`cg session` start gate, `set-context`, `pause`, `release`, `cg active`) in another workspace X with an inherited `CG_WORKSPACE` naming W use X's state files, gate, paused projects and badge root; guard tests with mutation checks: inside one of W's linked worktrees they still use W's; dispatch from X still consults W first (S22)
- [ ] Docs; `make check`; independent review until clean

## PR

- [x] Final independent review over the whole PR (4 rounds; round 4 clean; follow-ups in #307)
- [x] PR opened, CI green on Linux, macOS, Windows; CodeRabbit's comments answered (#303)
- [x] Intake review of #303 answered (should-fix 1 and the nits fixed; 2 and 3 await a spec amendment)

After merge, outside this PR (tracked on #259): the cross-repo PRs (registry schema, cg-extensions
manifests, `cg-shop`, `cg-tmux`, the `live-runs` note), the release, and X5 end to end.
