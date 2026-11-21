# Spec — extension commands declared in manifests; the session carries its workspace (#259)

Implements the decisions in [analysis.md](analysis.md#decision). Each scenario below is one behaviour and maps to
at least one test; `plan.md` names the milestone that delivers it. Q1–Q8 at the end are the choices this spec makes
beyond the decision, for the owner's agreement.

## Terms

- **Command:** one or more words, each matching `[a-z0-9-]+`, separated by single spaces, for example `live run`
  or `coderabbit approve`. Its first word is its **namespace**.
- **Declared command:** a command an installed extension's manifest lists under `contributes.commands`.
- **Typed words:** the leading tokens after cg's own persistent flags, up to the first token that starts with `-`
  (or the end). `cg live run --until-pane x` has typed words `live run`.
- **Built-in:** a command cobra resolves in cg itself (every top-level command name and alias, plus `help`,
  `completion` and `__complete`).
- **Session root:** the absolute directory named by `CG_WORKSPACE`. **Cwd root:** the root `workspace.FindRoot`
  finds from the current directory.
- **Consulted roots:** the roots dispatch looks at, in the order S22–S26 give.
- **Pinned tree:** the extension's source at the commit recorded in `.cg.json` (`extension.SourceRoot`).
- **Verified entry:** a ledger command entry whose extension's manifest in the pinned tree validates and declares
  the same command with the same binary, and whose binary exists in that tree's `bin/`.

## Manifest shape

```json
"manifest": 2,
"contributes": {
  "commands": [
    { "command": "live run",   "binary": "cg-live" },
    { "command": "live watch", "binary": "cg-live" }
  ]
}
```

`binary` names a file in the extension's `bin/` (`[a-z0-9-]+`, as today). The binary receives the arguments after
the namespace word (Q4), so `cg live run --until-pane x` runs `bin/cg-live run --until-pane x`, as today.
`commands` is optional: an extension that contributes no command (tmux) omits it.

## Scenarios

### Manifest

- **S1** WHEN a manifest declares `commands` entries with valid command words and binaries THEN it validates.
- **S2** WHEN a `commands` entry has an empty command, a word outside `[a-z0-9-]+`, a double space, a leading or
  trailing space, or an invalid binary THEN validation fails, naming the entry's index and the rule.
- **S3** WHEN one manifest declares the same command twice THEN validation fails, naming both indexes.
- **S4** WHEN a manifest contains `contributes.subcommands` (any schema version) THEN validation fails, saying the
  form was removed in cg 2.0 and naming `commands` as its replacement.
- **S5** WHEN one manifest declares both `shop` and `shop add` THEN it validates: nesting inside one extension is
  allowed, and dispatch picks the longest match (S16).
- **S6** WHEN a manifest declares `commands` with `"manifest": 1` THEN validation fails, asking for `"manifest": 2`;
  a v1 manifest without `subcommands` or `commands` still validates (Q1).

### Install, update, `cg init` and remove: conflicts refused before anything is applied

The conflict check compares the incoming manifest's commands with the **verified** commands of every other
extension installed in the workspace (from their pinned manifests, not their ledgers alone).

- **S7** WHEN an extension being installed declares a command another installed extension already declares THEN
  install fails, naming the command and both extensions, and the workspace (files, `.cg.json`, ledgers) is
  unchanged.
- **S8** WHEN it declares `pr` while another installed extension declares `pr merge` (or the reverse) THEN install
  fails the same way: a command inside another extension's command is a conflict in either direction.
- **S9** WHEN two extensions declare different commands in one namespace (`pr merge` and `pr watch`) THEN both
  install, and each command dispatches to its own extension.
- **S10** WHEN it declares a command whose namespace is a built-in (`start …`, `extension …`, `help …`) THEN install
  fails, naming the built-in.
- **S11** WHEN `cg extension update` (one target or all) brings versions whose commands conflict, with each other or
  with an extension not being updated, THEN the update fails before any target is applied, and every installed
  version stays. Moving a command from one extension to another in a single `cg extension update` (both targets)
  succeeds whatever the order.
- **S12** WHEN `cg init` re-installs extensions THEN the check runs over the whole set (the re-installed ones and
  those already present) before the first one is applied; on any conflict it reports every conflict and applies
  none of them.
- **S13** WHEN an extension is updated and keeps a command it already owns THEN that is not a conflict with itself.
- **S14** WHEN an extension is removed THEN its commands are free: another extension declaring them installs.
- **S15** WHEN `cg extension list` runs (text and `--json`) THEN each extension's commands are shown, and a command
  whose namespace is now a built-in (added in a later cg) is flagged.

### Dispatch: which program runs

Matching runs **per consulted root, in order**:

- **S16** WHEN the typed words start with a declared, verified command of the current root THEN the longest such
  command wins and its binary runs; later roots are not consulted.
- **S17** WHEN no root has a full match but some consulted root declares commands in the typed namespace (`cg shop`
  alone, or `cg shop frobnicate`) THEN, after every consulted root has been checked, cg exits with a usage error
  listing the declared commands in that namespace from the consulted roots, and the `PATH` fallback is not used
  (Q6).
- **S18** WHEN a root's ledger entry for the typed command isn't verified (the command is missing from the pinned
  manifest, the binary differs, the manifest doesn't validate, the tree can't be read, or the binary is missing)
  THEN dispatch **stops** with an error naming the extension and `cg init` / `cg extension update <name>` as the
  fix. It does not fall through to a later root or the `PATH` fallback, and the namespace counts as declared for
  S17/S34.
- **S19** WHEN two ledgers in one root declare the same command (a hand-edited or pulled `.cg.json`) THEN dispatch
  stops with a usage error naming both, instead of running the first.
- **S20** WHEN a root's ledger still records the removed `subcommands` form for the typed namespace THEN dispatch
  stops with an error naming `cg extension update <name>` (the upgrade path from cg 1.x).

### Dispatch: which workspace serves

- **S21** WHEN `CG_WORKSPACE` is unset THEN the cwd root alone is consulted, then the `PATH` fallback (today's order,
  with declared commands).
- **S22** WHEN `CG_WORKSPACE` is set and the cwd root is the session root THEN only the session root is consulted.
- **S23** WHEN the cwd root is listed in the session repo's `git worktree list --porcelain` (after resolving symlinks
  on both sides) THEN only the cwd root is consulted, so a worktree's own extension changes count.
- **S24** WHEN the cwd root is another workspace (not listed) THEN the session root is consulted first, then the cwd
  root, then the `PATH` fallback.
- **S25** WHEN the worktree check can't run or can't be trusted (git missing or refusing; the session root is not
  the top level of its repo; it's gone) THEN the cwd root counts as another workspace (S24): it can't win by making
  the check fail.
- **S26** WHEN the current directory is not inside any workspace and `CG_WORKSPACE` is set THEN the session root is
  consulted, then the `PATH` fallback. *(The external-repo case of #248.)*
- **S27** WHEN `CG_WORKSPACE` is relative, or names a directory with no valid `.cg.json` (for example a worktree the
  session started in and that was removed since) THEN it is ignored, dispatch behaves as S21, and the
  unknown-command hint says so.
- **S28** WHEN an external checkout has a planted `.cg.json` and a `.git` file pointing at the session's repo THEN it
  still counts as another workspace (the list is read from the session root only).
- **S29** WHEN the caller's environment has `GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR` or any other variable git
  lists in `git rev-parse --local-env-vars` THEN the worktree check ignores them.

### What the binary is told

- **S30** WHEN a binary runs from a workspace's declared command THEN `CG_EXTENSION_WORKSPACE` is set to the root
  that served it, and `CG_WORKSPACE` is passed through unchanged.
- **S31** WHEN a binary runs through the `PATH` fallback THEN `CG_EXTENSION_WORKSPACE` and `CG_SESSION_WORKTREE` are
  unset in its environment, even when the caller had them set.
- **S32** WHEN dispatch decides which workspace serves THEN it never reads `CG_EXTENSION_WORKSPACE` or
  `CG_SESSION_WORKTREE`.
- **S33** WHEN `cg start` (or bare `cg`) starts the agent or the launcher THEN `CG_WORKSPACE` is set to the absolute
  root it resolved (a worktree when started in one), in the agent's environment and the launcher's.

### `PATH` fallback

- **S34** WHEN no consulted root declares any command in the typed namespace and `cg-<namespace>` is on `PATH` THEN
  it runs with the arguments after the namespace (today's behaviour).
- **S35** WHEN a consulted root declares a command in that namespace THEN the `PATH` fallback is never used for it,
  whether or not the full command matched (Q3, Q6).
- **S36** WHEN nothing resolves THEN cg's unknown-command error carries a hint: whether `CG_WORKSPACE` is set (and
  why it was ignored, S27), and that a session started with `cg` carries its workspace to external directories.

### Session worktree

The session state file is `<badge root>/.claude/session-context/<id>.json`, where the badge root is
`gitx.CommonRoot` of the session root (as `sessionRoots` computes it today).

- **S37** WHEN `cg worktree create <slug>` runs with a session ID from `CG_SESSION_ID` or `CLAUDE_CODE_SESSION_ID`
  that passes `ValidSessionID` THEN the session's state file records the worktree's absolute path (`worktree`),
  keeping its other fields and creating the file when absent.
- **S38** WHEN `cg session set-context --worktree <path>` runs THEN the path is recorded the same way, for
  workspaces that create worktrees with their own scripts (Q5). A path that isn't listed in the repo's worktree
  list is refused.
- **S39** WHEN `cg worktree remove` removes the recorded worktree THEN the record is cleared.
- **S40** WHEN the session root serves a command for a current directory outside the session root and its
  worktrees (S24/S26), the session ID is valid, and the recorded worktree is still listed in the session repo's
  worktree list THEN `CG_SESSION_WORKTREE` is set to it (Q2); in every other case it is unset.
- **S41** WHEN the session ID fails `ValidSessionID` (for example `../../x`) THEN no state file is read or written.
- **S42** WHEN a state file has only `worktree` (no area or project) THEN the badge and `cg active` show the same as
  for a file without it.

### Unchanged

- **U1** Built-ins win over any declared command or `PATH` program.
- **U2** `cg live run|watch …` and `cg shop <subcommand> …` keep their spelling and arguments once the extensions
  ship `commands`.
- **U3** Inside the workspace, or one of its worktrees, without `CG_WORKSPACE`: the cwd's extensions serve, as today.
- **U4** `cg start` with a launcher behaves as today; `findLauncher` uses the pinned-manifest helper it now shares
  with dispatch.
- **U5** Every other contribution type installs, updates and is removed as today.
- **U6** No extension `bin/` directory is added to `PATH`.

## Cross-repo acceptance (outside this PR, released together)

- **X1** mnemcik/cg-extensions-registry: the manifest schema accepts `manifest: 2` and `commands`; `subcommands` is
  removed once no indexed manifest uses it.
- **X2** mnemcik/cg-extensions `live-runs` and `shopping`: `"manifest": 2` and `commands` for each subcommand their
  binary handles (`live run`, `live watch`; `shop add`, `shop list`, … and `shop help`); versions bumped. Bare
  `cg shop` then lists the declared commands (S17).
- **X3** `tmux`: `cg-tmux` passes `CG_WORKSPACE` into the tmux session (`new-session -e`) when it is set.
- **X4** `shopping`: `cg-shop` picks where it writes, in this order: `CG_SESSION_WORKTREE` when set; else
  `CG_EXTENSION_WORKSPACE` when the current directory is inside it; else, when `CG_EXTENSION_WORKSPACE` is set (it
  was served from the session root for an outside directory, with no recorded worktree), it refuses, naming
  `cg worktree create` / `cg session set-context --worktree`; else (run directly or through the `PATH` fallback)
  the current work tree, as today.
- **X5** End to end: from a session started with `cg`, `cd` to an external repo and `cg live run …` runs, and
  `cg shop list` reads the workspace's list.
- **X6** The naming rule for new namespaces (vendor-neutral subject namespaces such as `cg pr …`; vendor namespaces
  such as `cg coderabbit …` for vendor-specific commands) is written in `docs/extensions.md` and `EXTENSIONS.md`.

## Choices this spec makes (for agreement)

- **Q1 — manifest version.** Manifest **v2** for any manifest with `commands`; v1 manifests without `subcommands`
  keep validating, so only `live-runs` and `shopping` change. An older cg then refuses the new manifests with its
  existing "unsupported manifest schema version" error. *Alternative:* stay on v1 and only refuse `subcommands`;
  one version fewer in the code, but a cg 1.x installing the new manifests drops `commands` silently and `cg live`
  stops resolving with no explanation.
- **Q2 — how a writer learns the session's worktree.** Dispatch resolves the session ID from `CG_SESSION_ID`, then
  `CLAUDE_CODE_SESSION_ID` (as cg's session commands do, without a flag), reads the recorded worktree, re-checks it
  against the session repo's worktree list, and passes it per run as `CG_SESSION_WORKTREE` (S40). Binaries stay
  simple and agent-neutral. *Alternative:* a `cg session worktree` query each writer calls; no new variable, but
  every writer repeats the lookup and the check.
- **Q3 — which installs count for the fallback rule.** The consulted roots (session root, cwd root). cg keeps no
  record of workspaces across the machine, so "installed anywhere" isn't knowable.
- **Q4 — arguments the binary receives.** Everything after the namespace word, so existing binaries keep parsing
  their own subcommands. *Alternative:* everything after the full command; cleaner for one-binary-per-command
  extensions, but it breaks `cg-live` and `cg-shop`, and a binary serving several commands couldn't tell which ran.
- **Q5 — recording the worktree.** `cg worktree create` records it, and `cg session set-context --worktree` records
  it for workspaces whose own scripts create worktrees. *Alternative:* record only in `cg worktree create`, and
  such workspaces switch their scripts to it.
- **Q6 — the fallback is closed for a whole declared namespace,** not only for a declared full command (S17, S35).
  Decision 7 says "a command no installed extension declares"; read per full command, `cg shop frobnicate` would
  reach any `cg-shop` on `PATH`, and a grant written `Bash(cg shop *)` would cover it. *Alternative:* per full
  command, as decision 7's wording literally reads.
- **Q7 — flags end the typed words.** Matching stops at the first token starting with `-`. `cg live --x run` matches
  only `live`, which under S17 is a usage error unless the extension also declares `live`. *Alternative:* skip
  flags while matching; but cg can't know which flags take a value, so `--until-pane x` would misparse.
- **Q8 — an extension still on `subcommands` after upgrading cg** gets a clear refusal naming
  `cg extension update <name>` at dispatch (S20) and at `cg init` / install (S4), but no compatibility reading.
