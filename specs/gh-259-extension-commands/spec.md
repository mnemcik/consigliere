# Spec — extension commands declared in manifests; the session carries its workspace (#259)

Implements the decisions in [analysis.md](analysis.md#decision). Each scenario below is one behaviour and maps to
at least one test; `plan.md` names the milestone that delivers it.

## Terms

- **Command:** one or more words, each matching `[a-z0-9-]+`, separated by single spaces, for example `live run`
  or `coderabbit approve`. Its first word is its **namespace**.
- **Declared command:** a command an installed extension's manifest lists under `contributes.commands`.
- **Built-in:** a command cobra resolves in cg itself (every top-level command name and alias, plus `help`,
  `completion` and `__complete`).
- **Session root:** the workspace root named by `CG_WORKSPACE`. **Cwd root:** the root `workspace.FindRoot` finds
  from the current directory.
- **Pinned tree:** the extension's source at the commit recorded in `.cg.json` (`extension.SourceRoot`).

## Manifest shape

```json
"contributes": {
  "commands": [
    { "command": "live run",   "binary": "cg-live" },
    { "command": "live watch", "binary": "cg-live" }
  ]
}
```

`binary` names a file in the extension's `bin/` (`[a-z0-9-]+`, as today). The binary receives the arguments after
the namespace word (see Q4), so `cg live run --until-pane x` runs `bin/cg-live run --until-pane x`, as today.

## Scenarios

### Manifest

- **S1** WHEN a manifest declares `commands` entries with valid command words and binaries THEN it validates.
- **S2** WHEN a `commands` entry has an empty command, a word outside `[a-z0-9-]+`, a double space, a leading or
  trailing space, or an invalid binary THEN validation fails, naming the entry's index and the rule.
- **S3** WHEN one manifest declares the same command twice THEN validation fails, naming both indexes.
- **S4** WHEN a manifest contains `contributes.subcommands` THEN validation fails with a message that the form was
  removed in cg 2.0 and naming `commands` as its replacement (Q1).
- **S5** WHEN one manifest declares both `shop` and `shop add` THEN it validates: nesting inside one extension is
  allowed, and dispatch picks the longest match (S14).

### Install, update and `cg init`: conflicts refused before anything is applied

- **S6** WHEN an extension being installed declares a command another installed extension already declares THEN
  install fails, naming the command and both extensions, and the workspace (files, `.cg.json`, ledgers) is
  unchanged.
- **S7** WHEN it declares `pr` while another installed extension declares `pr merge` (or the reverse) THEN install
  fails the same way: a command inside another extension's command is a conflict in either direction.
- **S8** WHEN two extensions declare different commands in one namespace (`pr merge` and `pr watch`) THEN both
  install, and each command dispatches to its own extension.
- **S9** WHEN it declares a command whose namespace is a built-in (`start …`, `extension …`, `help …`) THEN install
  fails, naming the built-in.
- **S10** WHEN `cg extension update` brings a version whose commands conflict with another installed extension THEN
  the update fails as in S6/S7 and the installed version stays.
- **S11** WHEN `cg init` re-installs several extensions and any two conflict THEN it reports every conflict and
  applies none of the conflicting extensions' contributions (the check runs over the whole set first).
- **S12** WHEN an extension is updated and keeps a command it already owns THEN that is not a conflict with itself.
- **S13** WHEN `cg extension list` runs THEN each extension's commands are shown, and a command shadowed by a
  built-in (a built-in added in a later cg) is flagged.

### Dispatch: which program runs

- **S14** WHEN `cg <words…> <args…>` is typed and no built-in resolves it THEN dispatch matches the longest declared
  command that is a word-prefix of the leading non-flag tokens, and runs that extension's binary.
- **S15** WHEN no declared command matches but declared commands exist in that namespace (`cg shop` alone, or
  `cg shop frobnicate`) THEN cg exits with a usage error listing the declared commands in that namespace, and does
  not use the `PATH` fallback.
- **S16** WHEN the ledger and the manifest in the pinned tree disagree (command missing there, a different binary,
  the manifest doesn't validate, or the tree can't be read) THEN dispatch refuses that entry with a message naming
  the extension and `cg init` / `cg extension update` as the fix, and does not run the binary.
- **S17** WHEN two ledgers in one workspace declare the same command (a hand-edited or pulled `.cg.json`) THEN
  dispatch refuses with a usage error naming both, instead of running the first.
- **S18** WHEN the declared binary is missing from the pinned tree's `bin/` THEN dispatch refuses, naming the file.

### Dispatch: which workspace serves

- **S19** WHEN `CG_WORKSPACE` is unset THEN the cwd root alone is consulted, then the `PATH` fallback (today's order,
  with declared commands).
- **S20** WHEN `CG_WORKSPACE` is set and the cwd root is the session root THEN only the session root is consulted.
- **S21** WHEN the cwd root is listed in the session repo's `git worktree list --porcelain` (after resolving symlinks
  on both sides) THEN only the cwd root is consulted, so a worktree's own extension changes count.
- **S22** WHEN the cwd root is another workspace (not listed) THEN the session root is consulted first, then the
  cwd root, then the `PATH` fallback.
- **S23** WHEN the worktree check can't run (git missing or refusing, session root gone) THEN the cwd root counts as
  another workspace (S22): it can't win by making the check fail.
- **S24** WHEN the current directory is not inside any workspace and `CG_WORKSPACE` is set THEN the session root is
  consulted, then the `PATH` fallback. *(The external-repo case of #248.)*
- **S25** WHEN `CG_WORKSPACE` names a directory with no valid `.cg.json` THEN it is ignored and the unknown-command
  hint says so.
- **S26** WHEN a `.cg.json` file is planted in an external checkout with a `.git` file pointing at the session's
  repo THEN the check still treats it as another workspace (the list is read from the session root only).

### What the binary is told

- **S27** WHEN a binary runs from a workspace's declared command THEN `CG_EXTENSION_WORKSPACE` is set to the root
  that served it, and `CG_WORKSPACE` is passed through unchanged.
- **S28** WHEN a binary runs through the `PATH` fallback THEN `CG_EXTENSION_WORKSPACE` is unset in its environment,
  even when the caller had it set.
- **S29** WHEN dispatch decides which workspace serves THEN it never reads `CG_EXTENSION_WORKSPACE`.
- **S30** WHEN `cg start` (or bare `cg`) starts the agent or the launcher THEN `CG_WORKSPACE` is set to the root it
  resolved, in the agent's environment and the launcher's.

### `PATH` fallback

- **S31** WHEN no consulted workspace declares any command in the namespace and `cg-<namespace>` is on `PATH` THEN
  it runs with the arguments after the namespace (today's behaviour).
- **S32** WHEN a consulted workspace declares a command in that namespace THEN the `PATH` fallback is never used for
  it, whether or not the full command matched (Q3).
- **S33** WHEN nothing resolves THEN cg's unknown-command error carries a hint: whether `CG_WORKSPACE` is set, and
  that a session started with `cg` carries its workspace to external directories.

### Session worktree

- **S34** WHEN `cg worktree create <slug>` runs with a resolvable session ID THEN the session's state file records
  the worktree's absolute path (`worktree`), keeping its other fields.
- **S35** WHEN `cg session set-context --worktree <path>` runs THEN the path is recorded the same way, for
  workspaces that create worktrees with their own scripts (Q5). A path that isn't a worktree of the session repo is
  refused.
- **S36** WHEN `cg worktree remove` removes the recorded worktree THEN the record is cleared.
- **S37** WHEN a binary runs from the session root while the current directory is outside it (S22/S24) and the
  session has a recorded worktree that still exists THEN `CG_SESSION_WORKTREE` is set to it (Q2); otherwise it is
  unset.

### Unchanged

- **U1** Built-ins win over any declared command or `PATH` program.
- **U2** `cg live run|watch …` and `cg shop …` keep their spelling and arguments once the extensions ship
  `commands`.
- **U3** Inside the workspace, or one of its worktrees, without `CG_WORKSPACE`: the cwd's extensions serve, as today.
- **U4** `findLauncher` and the `launcher` contribution are unchanged, apart from sharing the pinned-manifest helper.
- **U5** Every other contribution type installs, updates and is removed as today.

## Cross-repo acceptance (outside this PR, released together)

- **X1** mnemcik/cg-extensions-registry: the manifest schema accepts `commands` and rejects `subcommands`.
- **X2** mnemcik/cg-extensions `live-runs` and `shopping`: manifests declare their commands; versions bumped.
- **X3** `tmux`: `cg-tmux` passes `CG_WORKSPACE` into the tmux session (`new-session -e`) when it is set.
- **X4** `shopping`: `cg-shop` writes into `CG_SESSION_WORKTREE` when set, else `CG_EXTENSION_WORKSPACE` when the
  current directory is inside it, else the current work tree when no `CG_WORKSPACE` is set (today); and refuses
  when it was served from the session root for an outside directory with no recorded worktree.
- **X5** End to end: from a session started with `cg`, `cd` to an external repo and `cg live run …` runs, and
  `cg shop list` reads the workspace's list.

## Questions settled by this spec (for agreement)

- **Q1 — manifest version.** Keep `"manifest": 1` and refuse a `subcommands` key (S4). *Alternative:* a v2
  manifest, which would make all 11 registry extensions bump, though only two change. An older cg reading a new
  manifest ignores `commands` silently; acceptable because the two affected extensions have no other users.
- **Q2 — how a writer learns the session's worktree.** Dispatch resolves the session ID as cg already does
  (`--session-id`, `CG_SESSION_ID`, `CLAUDE_CODE_SESSION_ID`), reads the recorded worktree and passes it per run
  as `CG_SESSION_WORKTREE` (S37). Binaries stay simple and agent-neutral. *Alternative:* a `cg session worktree`
  query each writer calls.
- **Q3 — which installs count for the fallback rule.** The workspaces dispatch consults (session root, cwd root).
  cg keeps no record of workspaces across the machine, so "installed anywhere" isn't knowable.
- **Q4 — arguments the binary receives.** Everything after the namespace word, so existing binaries keep parsing
  their own subcommands. *Alternative:* everything after the full command, which would break `cg-live` and
  `cg-shop`.
- **Q5 — recording the worktree.** `cg worktree create` records it, and `cg session set-context --worktree` records
  it for workspaces whose own scripts create worktrees.
