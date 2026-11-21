# Plan — #259

One PR on `gh-259-extension-commands`, one commit (or a small group) per milestone, independent review after each
milestone and a final round over the whole PR. Title: `feat!: extension commands declared in manifests; sessions
carry their workspace`, with a `BREAKING CHANGE:` footer (→ 2.0.0).

Every new test must fail before its change. Guard tests (refusals: S6–S11, S16–S18, S23, S26, S28, S32) also get
a mutation check: remove the guard, confirm the test fails, restore. `make check` passes before each milestone
commit; Windows rules from CONTRIBUTING apply (dispatch tests that exec binaries skip on Windows with a reason, as
#239 leaves them unsupported there).

## M1 — manifest `commands` and install-time ownership

Scenarios: S1–S13, U5.

- `internal/extension/manifest.go`: `CommandContribution{Command, Binary}`; `Contributions.Commands`; remove
  `SubcommandContribution` and `Subcommands`. `Validate`: word syntax, duplicate within the manifest, refuse a raw
  `subcommands` key (decode into a map once to detect it, since `json.Unmarshal` drops unknown fields).
- `internal/extension/ledger.go`: `Commands` replaces `Subcommands`; `apply.go:240` records them.
- New `internal/extension/commands.go`: a pure `CheckCommands(installed map[string][]string, incoming
  map[string][]string, builtins []string) []Conflict` covering duplicate, nesting both ways, built-in namespace;
  an extension never conflicts with its own previous version.
- `cmd/extension.go`: call it in install (`runExtInstall`) and `updateOne` before `reapply`; in
  `reinstallMissingExtensions` (`cg init`) over the whole set before the first apply. Built-in list from
  `rootCmd.Commands()` (names and aliases) plus `help`, `completion`, `__complete`.
- `cg extension list`: show commands; flag built-in shadowing.
- Docs: `docs/extensions.md` (contribution point, discovery section, the `cg-1password` contradiction),
  `EXTENSIONS.md`.

## M2 — dispatch by command table, with the pinned-manifest check

Scenarios: S14–S19, S31–S33, U1–U3.

- `cmd/external.go`: collect the leading non-flag words after the verb; for a root, build its table from the
  ledgers, verifying each entry against the manifest in the pinned tree (a helper shared with `findLauncher`,
  carried over from #251's trust helper); longest word-prefix match; duplicate across ledgers → usage error;
  namespace declared but no match → usage error listing the declared commands; namespace undeclared → `PATH`
  fallback `cg-<namespace>`.
- The binary still receives the arguments after the namespace word (Q4).
- Unknown-command hint (S33), carried over from #251 and reworded.

## M3 — the session carries its workspace

Scenarios: S20–S30.

- `cmd/start.go` / `start_exec_*`: set `CG_WORKSPACE` in the environment of the agent or launcher.
- `cmd/external.go`: the precedence of S20–S24. The worktree test reads `git worktree list --porcelain` in the
  session root only (`gitx` already parses it), compares workspace roots after `filepath.EvalSymlinks`, skips
  unresolvable entries, fails closed. Make sure an inherited `GIT_DIR` / `GIT_WORK_TREE` can't redirect it (clear
  them for that call).
- `runExternal`: set `CG_EXTENSION_WORKSPACE` for a workspace-served binary; strip it for a fallback binary.
- Docs: discovery section, the two variables and the precedence.

## M4 — the session records its worktree

Scenarios: S34–S37.

- `internal/session/context.go`: a `worktree` key in the state file, written through the existing map-preserving
  writer.
- `cmd/worktree.go`: `create` records it when a session ID resolves; `remove` clears it when it matches.
- `cmd/session.go`: `set-context --worktree <path>`, refused unless the path is listed in the repo's worktree list.
- `cmd/external.go`: when the session root serves an outside directory, pass `CG_SESSION_WORKTREE` if a recorded
  worktree exists.
- Docs: session state and the writer contract.

## Cross-repo (after this PR is approved, before the release PR is merged)

1. mnemcik/cg-extensions-registry: schema `commands` in, `subcommands` out (X1).
2. mnemcik/cg-extensions: `live-runs` and `shopping` manifests with `commands`; `cg-shop` write-root rule (X4);
   `cg-tmux` passes `CG_WORKSPACE` (X3); the `live-runs` note on external repos. Version bumps, then the registry
   index bump.
3. The maintainer merges this PR, then the release PR (2.0.0). Then `cg update`, `cg extension update` for the three
   extensions in a workspace, and X5.

## Risks

- `rootCmd.Commands()` called from install code: the built-in list must not include hidden test commands; verified
  in M1 tests.
- The extra `git worktree list` call per dispatch outside the session root: measured once in M3 (expected a few ms).
- A workspace pinned to an old cg: with the new manifests, its `cg live` stops resolving until it updates cg; noted in
  the release notes.
