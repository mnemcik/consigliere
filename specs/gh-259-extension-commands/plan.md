# Plan — #259

One PR on `gh-259-extension-commands`, one commit (or a small group) per milestone, independent review after each
milestone and a final round over the whole PR. Title: `feat!: extension commands declared in manifests; sessions
carry their workspace`. The description closes #259 and #248. The squash body becomes the changelog entry, so its
`BREAKING CHANGE:` footer carries the migration: update cg, then `cg extension update` (updates every installed
extension, including `live-runs`, `shopping`, and `tmux` for the variable in tmux sessions).

Every new test must fail before its change. Guard tests (refusals: S4, S6–S12, S17–S20, S25, S27–S29, S31, S35, S38,
S40, S41) also get a mutation check: remove the guard, confirm the test fails, restore. `make check` passes at every
milestone commit. Windows rules from CONTRIBUTING apply: dispatch tests that exec binaries skip on Windows with a
reason (#239 leaves extension commands unsupported there); path comparisons use `filepath.EvalSymlinks` and don't
depend on case.

## M1 — manifest `commands` and install-time ownership

Scenarios: S1–S15, U4, U5, X6 (docs part).

- `internal/extension/manifest.go`: `SchemaVersion` 2, v1 still accepted without `commands`/`subcommands` (S6);
  `CommandContribution{Command, Binary}`; `Contributions.Commands`; remove `SubcommandContribution` and
  `Subcommands`. `Validate`: word syntax, duplicate within the manifest; refuse a raw `subcommands` key (S4), found
  by decoding `contributes` into a map once, since `json.Unmarshal` drops unknown fields.
- `internal/extension/ledger.go`: `Commands` replaces `Subcommands`; `apply.go:240` records them.
- New `internal/extension/commands.go`: a pure `CheckCommands(installed, incoming map[string][]CommandContribution,
  builtins []string) []Conflict` covering duplicate, nesting both ways, built-in namespace; an extension never
  conflicts with its own previous version.
- A shared `verifiedManifest(root, ref)` helper (carried over from #251's trust helper) that loads and validates the
  manifest in the pinned tree; `findLauncher` switches to it (U4). Installed extensions' commands for the check come
  from it.
- `cmd/extension.go`:
  - install: check before `reapply`;
  - update: resolve and load every target's new manifest first, check the whole new set, then apply and save
    (S11);
  - `cg init` (`reinstallMissingExtensions`): load the manifests of the extensions to re-install and of those
    present, check the whole set, then apply (S12).
  - Built-in list from `rootCmd.Commands()` (names and aliases, hidden ones included, since they resolve too) plus `help`, `completion`,
    `__complete`.
  - `cg extension list`: commands in text and `--json`; built-in shadowing flagged.
- `cmd/external.go`, interim: read `ledger.Commands` and match on the namespace word only (today's behaviour on the
  new field), so the tree builds and `make check` passes; M2 replaces it.
- Docs: `docs/extensions.md` (contribution point, manifest v2, the naming rule (X6), discovery section, the
  `cg-1password` contradiction at `:145`, `:405` and `:452`), `EXTENSIONS.md`.

## M2 — dispatch by command table

Scenarios: S16–S21, S34–S36, U1–U3, U6.

- `cmd/external.go`: typed words (Q7); for each consulted root (only the cwd root until M3), build its table from
  the ledgers, verifying each entry with the M1 helper; the first root declaring the typed namespace decides it (Q9),
  longest word-prefix match there; an unverified entry for the typed command stops dispatch (S18); duplicates stop it (S19); an old
  `subcommands` ledger entry for the namespace stops it with the update hint (S20, read from the raw ledger JSON);
  no match in the deciding root → usage error listing its commands (S17); an undeclared namespace
  → `PATH` fallback `cg-<namespace>`.
- The binary still receives the arguments after the namespace word (Q4).
- Unknown-command hint (S36), carried over from #251 and reworded.

## M3 — the session carries its workspace

Scenarios: S22–S33.

- `cmd/start.go` / `start_exec_*`: set `CG_WORKSPACE` to the absolute resolved root in the environment of the agent
  or launcher (S33).
- `cmd/external.go`: consulted roots per S22–S26; `CG_WORKSPACE` ignored when relative or invalid (S27).
- Worktree test: a new `gitx` helper that runs git with a **cleaned** environment (every variable from
  `git rev-parse --local-env-vars` removed; `RunEnv` can only add). In the session root, require
  `git rev-parse --show-toplevel` to equal the session root, then read `git worktree list --porcelain`; compare
  workspace roots after `filepath.EvalSymlinks`; skip unresolvable entries; any failure → another workspace (S25,
  S28, S29).
- `runExternal`: set `CG_EXTENSION_WORKSPACE` for a workspace-served binary; strip it and `CG_SESSION_WORKTREE`
  for a fallback binary (S30, S31).
- Docs: discovery section, the variables, the precedence.

## M4 — the session records its worktree

Scenarios: S37–S42.

- `internal/session/context.go`: a `worktree` key, written through the existing map-preserving writer;
  `ReadContext` and the writer refuse an invalid session ID themselves (S41), not only their callers.
- State file location: `gitx.CommonRoot` of the session root, as `sessionRoots` does.
- `cmd/worktree.go`: `create` records it when a session ID resolves; `remove` clears it when it matches.
- `cmd/session.go`: `set-context --worktree <path>`, refused unless the path is in the repo's worktree list.
- `cmd/external.go`: S40, re-checking the recorded path against the worktree list (same cleaned-env helper).
- Check `cg active` and the status line with a state file holding only `worktree` (S42); adjust if either lists it.
- Docs: session state and the writer contract.

## Cross-repo (after this PR is approved, before the release PR is merged)

1. mnemcik/cg-extensions-registry: schema accepts `manifest: 2` and `commands`, keeping `subcommands` for now, so
   the indexed v1 manifests still validate (X1, first half).
2. mnemcik/cg-extensions: `live-runs` and `shopping` on manifest v2 with `commands` (X2); `cg-shop` write root
   (X4); `cg-tmux` passes `CG_WORKSPACE` (X3); the `live-runs` note on external repos. Version bumps, then the
   registry index bump.
3. Registry: remove `subcommands` from the schema (X1, second half).
4. The maintainer merges this PR, then the release PR (2.0.0). Then `cg update`, `cg extension update` for the three
   extensions in a workspace, and X5.

## Risks

- Hidden or test-only cobra commands in the built-in list: covered by an M1 test.
- The extra git calls per dispatch outside the session root: measured once in M3 (expected a few ms).
- A workspace pinned to cg 1.x after the extensions move to v2: `cg extension update` there fails with the
  unsupported-version error until it updates cg (Q1).
