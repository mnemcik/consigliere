# Plan — #259

One PR on `gh-259-extension-commands`, one commit (or a small group) per milestone, independent review after each
milestone and a final round over the whole PR. Title: `feat!: extension commands declared in manifests; sessions
carry their workspace`. The description closes #259 and #248. The squash body becomes the changelog entry, so its
`BREAKING CHANGE:` footer carries the migration: update cg, then, once the extensions' v2 versions are
published (cross-repo step 4), `cg extension update` (one run updates every installed extension, including
`tmux` for the variable in tmux sessions); an update run before step 4 fetches the old manifests, which then
contribute no commands (Q8).

Every new test must fail before its change. Guard tests (refusals: S5–S11, S16–S18, S23, S25–S27, S29, S31 (strip), S33, S36,
S38, S38b, S39) also get a mutation check: remove the guard, confirm the test fails, restore. `make check` passes at every
milestone commit. Windows rules from CONTRIBUTING apply: dispatch tests that exec binaries skip on Windows with a
reason (#239 leaves extension commands unsupported there); path comparisons use `filepath.EvalSymlinks` and don't
depend on case.

## M1 — manifest `commands` and install-time ownership

Scenarios: S1–S14, U4, U5, X6 (docs part).

- `internal/extension/manifest.go`: `SchemaVersion` 2, v1 still accepted without `commands` (S5);
  `CommandContribution{Command, Binary}`; `Contributions.Commands`; remove `SubcommandContribution` and
  `Subcommands`. `Validate`: word syntax, duplicate within the manifest. A `subcommands` key is ignored like any unknown field (Q8).
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
    (S10);
  - `cg init` (`reinstallMissingExtensions`): load the manifests of the extensions to re-install and of those
    present, check the whole set, then apply (S11).
  - Built-in list from `rootCmd.Commands()` (names and aliases, hidden ones included, since they resolve too) plus `help`, `completion`,
    `__complete`.
  - `cg extension list`: commands in text and `--json`; built-in shadowing flagged.
- `cmd/external.go`, interim: read `ledger.Commands` and match on the namespace word only (today's behaviour on the
  new field), so the tree builds and `make check` passes; M2 replaces it.
- Docs: `docs/extensions.md` (contribution point, manifest v2, the naming rule (X6), discovery section, the
  `cg-1password` contradiction at `:145`, `:405` and `:452`), `EXTENSIONS.md`.

## M2 — dispatch by command table

Scenarios: S15–S19, S28 and S29 (their `CG_COMMAND` parts), S30, S32–S34, U1–U3, U6.

- `cmd/external.go`: typed words (Q7); for each consulted root (only the cwd root until M3), build its table from
  the ledgers, verifying each entry with the M1 helper. The first root with any entry in the typed namespace (a
  `commands` entry, verified or not) decides it
  (Q9). In the deciding root, in this order: any duplicate
  in the namespace stops it (S18); the longest word-prefix match over all its `commands` entries is chosen, and stops dispatch if it
  isn't verified (S17); no match → usage error listing its commands (S16). An undeclared namespace → `PATH`
  fallback `cg-<namespace>`.
- The binary still receives the arguments after the namespace word, with `CG_COMMAND` set to the matched
  declared command (Q4, S28's `CG_COMMAND` part; `runExternal` sets it, the fallback path strips it, S29).
- Unknown-command hint (S34), carried over from #251 and reworded.

## M3 — the session carries its workspace

Scenarios: S20–S31.

- `cmd/start.go` / `start_exec_*`: set `CG_WORKSPACE` to the absolute resolved root in the environment of the agent
  or launcher, and remove the three per-run variables (S31).
- `cmd/external.go`: consulted roots per S20–S24; `CG_WORKSPACE` ignored or stopping per S25's two branches. Guard
  tests with mutation checks: each S25 stop case (directory, empty, unreadable, invalid JSON, non-object, `null`)
  stops from S22 and S24 and doesn't stop inside a listed worktree (S21); S9 (in M1's code, changed with this
  amendment): an owned built-in-namespace command passes update with the warning, a new command in the same
  built-in namespace is refused, a moved command is refused, `cg init` only warns.
- Cross-root tests: a session root whose `shop` entry is unverified (S17), or that declares only
  `shop add` (S16), blocks a clone's verified `shop …` typed from inside the clone (guard tests, with mutation
  checks).
- Worktree test: a new `gitx` helper that runs git with a **cleaned** environment (every variable from
  `git rev-parse --local-env-vars` removed; `RunEnv` can only add). In the session root, require
  `git rev-parse --show-toplevel` to equal the session root, then read `git worktree list --porcelain`; compare
  workspace roots after `filepath.EvalSymlinks`; skip unresolvable entries; any failure → another workspace (S23,
  S26, S27).
- `runExternal`: set `CG_EXTENSION_WORKSPACE` for a workspace-served binary; strip it and `CG_SESSION_WORKTREE`
  for a fallback binary (S28, S29).
- Docs: discovery section, the variables, the precedence.

## M4 — the session records its worktree

Scenarios: S35–S40, S38b.

- `internal/session/context.go`: a `worktree` key, written through the existing map-preserving writer;
  `ReadContext` and the writer refuse an invalid session ID themselves (S39), not only their callers.
- State file location: `gitx.CommonRoot` of the session root, as `sessionRoots` does.
- `cmd/worktree.go`: `create` records it when a session ID resolves and the worktree belongs to the session repo
  (S35); `remove` clears it when it matches (S37). Guard tests with mutation checks: a worktree created or removed
  in another repo leaves the record unchanged; no session repo (no valid `CG_WORKSPACE`, cwd outside any workspace)
  records nothing; a symlinked session root still matches.
- Session commands root (Terms) and S38c: guard tests with mutation checks — session commands run in another
  workspace with an inherited `CG_WORKSPACE` use that workspace's state; inside one of W's linked worktrees they
  still use W's; dispatch from X still consults W first; a worktree created while in X doesn't enter W's record,
  and a writer served from W while in X gets no `CG_SESSION_WORKTREE` pointing into X. S6's unreadable-tree
  refusal (M1's code): guard tests with mutation checks, for install and update, an unreadable extension with no
  ledger commands, and an update-all whose own target's tree is missing (not refused).
- `cmd/session.go`: `set-context --worktree <path>`, refused unless the path is one of the repo's linked worktrees
  (S36: not the main checkout; symlinks resolved). Guard tests with mutation checks: the main checkout refused by
  S36 and ignored by S38 (and then S38b applies); a symlinked path to a linked worktree accepted.
- `cmd/external.go`: S38, re-checking the recorded path against the worktree list; S38b, the linked-worktree test
  exactly as the spec's S38b states it (one cleaned-env `git rev-parse --path-format=absolute --show-toplevel
  --git-dir --git-common-dir`; top level equals the session root; git dir differs from common dir; both compared
  after `filepath.EvalSymlinks`; any failure → not linked; no session ID needed). Tests: a main checkout → unset;
  a session root that is a subdirectory of its repo → unset; a symlinked linked-worktree root → set; a stale
  recorded worktree, an invalid session ID, or an unreadable state file, with a linked-worktree session root →
  set to the session root; a failing `rev-parse` → unset.
- Check `cg active` and the status line with a state file holding only `worktree` (S40); adjust if either lists it.
- Docs: session state and the writer contract.

## Cross-repo (after the agreement; each merge on the maintainer's go-ahead)

`cg extension install`/`update` read the extension repo's default branch, and the registry's `manifestUrl` points
at `main`, so v2 manifests must not reach cg-extensions `main` before cg 2.0.0 is released. Order:

1. mnemcik/cg-extensions-registry: schema accepts `manifest: 2` and `commands`, keeping `subcommands` for now, so
   the indexed v1 manifests still validate (X1, first half). Can merge any time.
2. mnemcik/cg-extensions PR A, compatible with cg 1.x: `cg-tmux` passes `CG_WORKSPACE` (X3); `cg-shop` write root
   (X4); the `live-runs` note on external repos. Version bumps. Can merge any time.
3. This PR, then the release PR → cg 2.0.0.
4. mnemcik/cg-extensions PR B: `live-runs` and `shopping` on manifest v2 with `commands` (X2), version bumps; then
   the registry index bump (versions, "cg 2.0+" in the descriptions). Prepared early, merged right after the
   release.
5. Registry: remove `subcommands` from the schema (X1, second half).
6. On each machine and workspace: `cg update`, then `cg extension update`; then X5.

## Risks

- Hidden or test-only cobra commands in the built-in list: covered by an M1 test.
- The extra git calls per dispatch outside the session root: measured once in M3 (expected a few ms).
- A workspace pinned to cg 1.x after the extensions move to v2: `cg extension update` there fails with the
  unsupported-version error until it updates cg (Q1).
