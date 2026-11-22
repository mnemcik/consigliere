# Spec — extension commands declared in manifests; the session carries its workspace (#259)

Implements the decisions in [analysis.md](analysis.md#decision). Each scenario below is one behaviour and maps to
at least one test; `plan.md` names the milestone that delivers it. Q1–Q10 at the end are the choices this spec makes
beyond the decision, for the owner's agreement.

## Terms

- **Command:** one or more words, each matching `[a-z0-9][a-z0-9-]*` (a letter or digit first, then letters, digits
  or `-`), separated by single spaces, for example `live run` or `coderabbit approve`. Its first word is its
  **namespace**.
- **Declared command:** a command an installed extension's manifest lists under `contributes.commands`.
- **Typed words:** the leading tokens after cg's own persistent flags, up to the first token that starts with `-`
  (or the end). `cg live run --until-pane x` has typed words `live run`.
- **Built-in:** a command cobra resolves in cg itself (every top-level command name and alias, plus `help`,
  `completion` and `__complete`).
- **Session root:** the absolute directory named by `CG_WORKSPACE`. **Cwd root:** the root `workspace.FindRoot`
  finds from the current directory.
- **Consulted roots:** the roots dispatch looks at, in the order S20–S24 give.
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
the namespace word (Q4), so `cg live run --until-pane x` runs `bin/cg-live run --until-pane x`, as today, with
`CG_COMMAND=live run` in its environment (S28).
`commands` is optional: an extension that contributes no command (tmux) omits it.

## Scenarios

### Manifest

- **S1** WHEN a manifest declares `commands` entries with valid command words and binaries THEN it validates.
- **S2** WHEN a `commands` entry has an empty command, a word outside `[a-z0-9][a-z0-9-]*` (a word starting with
  `-` could never be typed, since typed words end at the first `-` token, Q7), a double space, a leading or trailing
  space, or an invalid binary THEN validation fails, naming the entry's index and the rule.
- **S3** WHEN one manifest declares the same command twice THEN validation fails, naming both indexes.
- **S4** WHEN one manifest declares both `shop` and `shop add` THEN it validates: nesting inside one extension is
  allowed, and dispatch picks the longest match (S15).
- **S5** WHEN a manifest declares `commands` with `"manifest": 1` THEN validation fails, asking for `"manifest": 2`;
  a v1 manifest without `commands` still validates (Q1).

### Install, update, `cg init` and remove: conflicts refused before anything is applied

The conflict check compares the incoming manifest's commands with the **verified** commands of every other
extension installed in the workspace (from their pinned manifests, not their ledgers alone).

- **S6** WHEN an extension being installed declares a command another installed extension already declares THEN
  install fails, naming the command and both extensions, and the workspace (files, `.cg.json`, ledgers) is
  unchanged.
- **S7** WHEN it declares `pr` while another installed extension declares `pr merge` (or the reverse) THEN install
  fails the same way: a command inside another extension's command is a conflict in either direction.
- **S8** WHEN two extensions declare different commands in one namespace (`pr merge` and `pr watch`) THEN both
  install, and each command dispatches to its own extension.
- **S9** WHEN an extension declares a command whose namespace is a built-in (`start …`, `extension …`, `help …`)
  and the extension doesn't already own that command THEN install and update fail, naming the built-in. An
  extension owns a command when its installed version's manifest, read and validated at its recorded commit,
  declares it, never from the ledger alone; a missing binary doesn't end ownership, so `cg extension update` can
  still repair it (S17). An extension whose tree can't be read owns nothing, so `cg init` has to restore it first.
  Ownership is per command: owning
  `foo run` doesn't let an update add `foo watch` when `foo` is a built-in, and a command moved to another extension
  in one update isn't owned by the receiving extension. An owned command whose namespace a later cg made a built-in
  doesn't block update: cg prints a warning naming the command and the built-in, S14 flags it, and the built-in
  still wins at dispatch (U1). `cg init` restores the versions recorded in `.cg.json`, so every command it
  restores is owned: it only warns.
- **S10** WHEN `cg extension update` (one target or all) brings versions whose commands conflict, with each other or
  with an extension not being updated, THEN the update fails before any target is applied, and every installed
  version stays. Moving a command from one extension to another in a single `cg extension update` (both targets)
  succeeds whatever the order.
- **S11** WHEN `cg init` re-installs extensions THEN the check runs over the whole set (the re-installed ones and
  those already present) before the first one is applied; on any conflict it reports every conflict and applies
  none of them.
- **S12** WHEN an extension is updated and keeps a command it already owns THEN that is not a conflict with itself.
- **S13** WHEN an extension is removed THEN its commands are free: another extension declaring them installs.
- **S14** WHEN `cg extension list` runs (text and `--json`) THEN each extension's commands are shown, and a command
  whose namespace is a built-in is flagged (it can't run: the built-in wins, U1).

### Dispatch: which program runs

Roots are checked in the consulted order. The first root whose ledgers have **any** entry in the typed namespace (a
`commands` entry, verified or not) **decides** that namespace; later roots and the
`PATH` fallback are not consulted for it (Q6, Q9).

- **S15** WHEN the typed words start with one or more of the deciding root's `commands` entries THEN the longest
  such entry wins (chosen among all its entries, before verification) and, if it is verified, its binary runs;
  if it isn't, S17 applies. A shorter verified entry never stands in for an unverified longer one.
- **S16** WHEN the deciding root has no such entry (with only `pr merge` declared: `cg pr` alone, or `cg pr watch`)
  THEN cg exits with a
  usage error listing that root's declared commands in the namespace; a later root that declares the typed command
  is not consulted, and neither is the `PATH` fallback.
- **S17** WHEN a root's ledger entry for the typed command isn't verified (the command is missing from the pinned
  manifest, the binary differs, the manifest doesn't validate, the tree can't be read, or the binary is missing)
  THEN dispatch **stops** with an error naming the extension and `cg init` / `cg extension update <name>` as the
  fix. It does not fall through to a later root or the `PATH` fallback, and the namespace counts as declared for
  S16/S32.
- **S18** WHEN two ledgers in the deciding root declare the same command anywhere in the typed namespace (a
  hand-edited or pulled `.cg.json`), whether or not it is the command the typed words match, THEN dispatch stops
  with a usage error naming both, instead of running the first.

### Dispatch: which workspace serves

- **S19** WHEN `CG_WORKSPACE` is unset THEN the cwd root alone is consulted, then the `PATH` fallback (today's order,
  with declared commands).
- **S20** WHEN `CG_WORKSPACE` is set and the cwd root is the session root THEN only the session root is consulted.
- **S21** WHEN the cwd root is listed in the session repo's `git worktree list --porcelain` (after resolving symlinks
  on both sides) THEN only the cwd root is consulted, so a worktree's own extension changes count.
- **S22** WHEN the cwd root is another workspace (not listed) THEN the session root is consulted first, then the cwd
  root, then the `PATH` fallback.
- **S23** WHEN the worktree check can't run or can't be trusted (git missing or refusing; the session root is not
  the top level of its repo; it's gone) THEN the cwd root counts as another workspace (S22): it can't win by making
  the check fail.
- **S24** WHEN the current directory is not inside any workspace and `CG_WORKSPACE` is set THEN the session root is
  consulted, then the `PATH` fallback. *(The external-repo case of #248.)*
- **S25** WHEN `CG_WORKSPACE` is relative, names a directory that doesn't exist (for example a worktree the session
  started in and that was removed since), names one with no `.cg.json` entry, or names one whose `.cg.json` decodes
  to an object whose `type` is missing or isn't `consigliere` THEN it is ignored, dispatch behaves as S19, and the
  unknown-command hint says so. WHEN the `.cg.json` entry exists but can't be read or doesn't decode to an object (a
  directory, a dangling symlink, an empty file, invalid JSON, `null`, a non-object, or any field of the wrong type
  such as `"type": 5`; decoded the way cg reads `.cg.json` everywhere else, keys included) THEN dispatch stops with
  a usage error naming the file, and doesn't fall back to another
  workspace or the `PATH` fallback. This applies where the session root is consulted: under S20 it is also the
  current directory's root, which stops the same way (M2); under S22 and S24 as stated; inside one of the session's
  own worktrees (S21) only that worktree's `.cg.json` counts.
- **S26** WHEN an external checkout has a planted `.cg.json` and a `.git` file pointing at the session's repo THEN it
  still counts as another workspace (the list is read from the session root only).
- **S27** WHEN the caller's environment has `GIT_DIR`, `GIT_WORK_TREE`, `GIT_COMMON_DIR` or any other variable git
  lists in `git rev-parse --local-env-vars` THEN the worktree check ignores them.

### What the binary is told

- **S28** WHEN a binary runs from a workspace's declared command THEN `CG_EXTENSION_WORKSPACE` is set to the root
  that served it, `CG_COMMAND` is set to the declared command that matched (for example `live run`, or `live` for
  a bare-namespace entry), and `CG_WORKSPACE` is passed through unchanged.
- **S29** WHEN a binary runs through the `PATH` fallback THEN `CG_EXTENSION_WORKSPACE`, `CG_SESSION_WORKTREE` and
  `CG_COMMAND` are unset in its environment, even when the caller had them set.
- **S30** WHEN dispatch decides which workspace serves or which command matches THEN it never reads
  `CG_EXTENSION_WORKSPACE`, `CG_SESSION_WORKTREE` or `CG_COMMAND`.
- **S31** WHEN `cg start` (or bare `cg`) starts the agent or the launcher THEN `CG_WORKSPACE` is set to the absolute
  root it resolved (a worktree when started in one), in the agent's environment and the launcher's. It removes `CG_COMMAND`, `CG_EXTENSION_WORKSPACE` and `CG_SESSION_WORKTREE` from that
  environment: they describe one dispatched run and mustn't leak into a session started from inside a binary.

### `PATH` fallback

- **S32** WHEN no consulted root declares any command in the typed namespace and `cg-<namespace>` is on `PATH` THEN
  it runs with the arguments after the namespace (today's behaviour).
- **S33** WHEN a consulted root declares a command in that namespace THEN the `PATH` fallback is never used for it,
  whether or not the full command matched (Q3, Q6).
- **S34** WHEN nothing resolves THEN cg's unknown-command error carries a hint: whether `CG_WORKSPACE` is set (and
  why it was ignored, S25), and that a session started with `cg` carries its workspace to external directories.

### Session worktree

The session state file is `<badge root>/.claude/session-context/<id>.json`, where the badge root is
`gitx.CommonRoot` of the session root (as `sessionRoots` computes it today).

- **S35** WHEN `cg worktree create <slug>` runs with a session ID from `CG_SESSION_ID` or `CLAUDE_CODE_SESSION_ID`
  that passes `ValidSessionID`, and the new worktree belongs to the session's workspace repo (the repo of
  `CG_WORKSPACE`, or of the current workspace when `CG_WORKSPACE` is unset) THEN the session's state file records
  the worktree's absolute path (`worktree`), keeping its other fields and creating the file when absent. A worktree
  created in any other repo (an external repo the session works in) leaves the record unchanged.
- **S36** WHEN `cg session set-context --worktree <path>` runs THEN the path is recorded the same way, for
  workspaces that create worktrees with their own scripts (Q5). A path that isn't one of the repo's linked
  worktrees is refused: one not in `git worktree list`, and the main checkout (the list's first entry), so writers
  are never sent into the main checkout. Paths are compared after `filepath.EvalSymlinks` on both sides, as in S21;
  the same comparison applies in S38.
- **S37** WHEN `cg worktree remove` removes the recorded worktree THEN the record is cleared.
- **S38** WHEN the session root serves a command for a current directory outside the session root and its
  worktrees (S22/S24), the session ID is valid, and the recorded worktree is still one of the session repo's linked
  worktrees (listed, and not the main checkout) THEN `CG_SESSION_WORKTREE` is set to it (Q2).
- **S38b** WHEN the session root serves such a command, no valid worktree is recorded (none; no session ID, or one
  failing `ValidSessionID`; a state file that can't be read; or a recorded path that isn't one of the repo's linked worktrees, such as one no longer listed or the main checkout), and the session root is itself a linked worktree THEN `CG_SESSION_WORKTREE` is
  set to the session root (Q10). "Linked worktree" is decided in the session root with the cleaned environment:
  `git rev-parse --path-format=absolute --show-toplevel --git-dir --git-common-dir`; the top level must equal the
  session root and the git dir must differ from the common dir, both compared after `filepath.EvalSymlinks`. Any
  failure or mismatch means "not a linked worktree". In every other case than S38/S38b it is unset.
- **S39** WHEN the session ID fails `ValidSessionID` (for example `../../x`) THEN no state file is read or written.
- **S40** WHEN a state file has only `worktree` (no area or project) THEN the badge and `cg active` show the same as
  for a file without it.

### Unchanged

- **U1** Built-ins win over any declared command or `PATH` program.
- **U2** `cg live …` and `cg shop …` (subcommands, `--help`, bare namespace) keep their spelling, arguments and
  behaviour once the extensions ship `commands` as X2 says.
- **U3** Inside the workspace, or one of its worktrees, without `CG_WORKSPACE`: the cwd's extensions serve, as today.
- **U4** `cg start` with a launcher behaves as today; `findLauncher` uses the pinned-manifest helper it now shares
  with dispatch.
- **U5** Every other contribution type installs, updates and is removed as today.
- **U6** No extension `bin/` directory is added to `PATH`.

## Cross-repo acceptance (outside this PR, released together)

- **X1** mnemcik/cg-extensions-registry: the manifest schema accepts `manifest: 2` and `commands`; `subcommands` is
  removed once no indexed manifest uses it.
- **X2** mnemcik/cg-extensions `live-runs` and `shopping`: `"manifest": 2`; `commands` declares the bare namespace
  (`live`, `shop`, so `cg shop`, `cg shop --help` and `cg live --until-pane x run` reach the binary as today) and
  each subcommand the binary handles (`live run`, `live watch`; `shop add`, `shop list`, …), so permission entries
  can name them; versions bumped.
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

- **Q1 — manifest version.** Manifest **v2** for any manifest with `commands`; v1 manifests without `commands`
  keep validating, so only `live-runs` and `shopping` change. An older cg then refuses the new manifests with its
  existing "unsupported manifest schema version" error. *Alternative:* stay on v1;
  one version fewer in the code, but a cg 1.x installing the new manifests drops `commands` silently and `cg live`
  stops resolving with no explanation. *Also:* decision 8 says "`commands` is required"; this spec reads it as
  "required to contribute commands" (it replaces `subcommands`), so a manifest without commands, such as tmux's,
  omits it.
- **Q2 — how a writer learns the session's worktree.** Dispatch resolves the session ID from `CG_SESSION_ID`, then
  `CLAUDE_CODE_SESSION_ID` (as cg's session commands do, without a flag), reads the recorded worktree, re-checks it
  against the session repo's worktree list, and passes it per run as `CG_SESSION_WORKTREE` (S38). Binaries stay
  simple and agent-neutral. *Alternative:* a `cg session worktree` query each writer calls; no new variable, but
  every writer repeats the lookup and the check.
- **Q3 — which installs count for the fallback rule.** The consulted roots (session root, cwd root). cg keeps no
  record of workspaces across the machine, so "installed anywhere" isn't knowable.
- **Q4 — arguments the binary receives.** Everything after the namespace word, as git and gh do, so existing
  binaries keep parsing their own subcommands; and cg passes the declared command it matched as `CG_COMMAND`
  (S28), so a binary serving several commands, or one that wants to refuse words cg didn't match, uses what cg
  decided instead of re-parsing. *Alternatives:* everything after the full command plus a per-entry `args` prefix,
  like the launcher's (explicit, but every entry repeats its words); everything after the full command alone, as
  kubectl does (breaks `cg-live` and `cg-shop`, and a binary serving several commands can't tell which ran).
- **Q5 — recording the worktree.** `cg worktree create` records it, and `cg session set-context --worktree` records
  it for workspaces whose own scripts create worktrees. *Alternative:* record only in `cg worktree create`, and
  such workspaces switch their scripts to it.
- **Q6 — the fallback is closed for a whole declared namespace,** not only for a declared full command (S16, S33).
  Decision 7 says "a command no installed extension declares"; read per full command, `cg shop frobnicate` would
  reach any `cg-shop` on `PATH`, and a grant written `Bash(cg shop *)` would cover it. *Alternative:* per full
  command, as decision 7's wording literally reads.
- **Q7 — flags end the typed words.** Matching stops at the first token starting with `-`. `cg live --x run` matches
  only `live`, which under S16 is a usage error unless the extension also declares `live` (X2 does, for the two
  existing extensions). *Alternative:* skip flags while matching; but cg can't know which flags take a value, so
  `--until-pane x` would misparse.
- **Q8 — no special handling of the removed `subcommands` form.** cg 2.0 doesn't recognise it: like any unknown
  manifest or ledger field, it is ignored. An extension not yet updated contributes no commands, so `cg live run`
  is an unknown command until `cg extension update` runs. *Consequence:* in that window the session's workspace
  declares nothing in the namespace, so a cloned workspace that declares it, or a `cg-<namespace>` program on `PATH`,
  could serve it (Q6 and Q9 then have nothing to protect). *Alternative:* recognise the old form only to refuse it with an update hint, and let it claim its
  namespace.
- **Q9 — the first root that declares a namespace decides it.** Once the session root declares commands in `shop`,
  a cloned repo's workspace can't answer `cg shop frobnicate`, even when it declares that exact command; otherwise
  a typo or an undeclared subcommand would run the clone's code under a grant written for the session's namespace
  (decision 4: a clone mustn't choose the code behind the owner's grants). *Cost:* two workspaces can't split one
  namespace across them (session `pr merge`, clone `pr watch`); within one workspace they still can (S8).
  *Alternative:* decide per full command, as decision 4's "when it provides the command" literally reads.
- **Q10 — a session started in a linked worktree writes there without a record** (S38b). Decision 5 says writers
  find the worktree from the session's state file; S38b adds a second source that needs no state file and no
  session ID, because the session root (`CG_WORKSPACE`, set by `cg start`) is then already the worktree, and
  decision 5's aim (never write into the main checkout) holds. *Alternative:* refuse unless a worktree is recorded,
  as decision 5 literally reads; sessions started in a worktree by agents without a session ID then can't write
  from external directories.
