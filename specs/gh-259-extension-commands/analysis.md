# Analysis — extension commands: naming, resolution, and how a command finds its workspace (#259)

- **Item:** https://github.com/mnemcik/consigliere/issues/259
- **Size:** L (several milestones; new behaviour across the manifest, install, dispatch, `cg start` and session state)
- **Status:** decided; the decisions are posted on the issue
- **Evidence base:** `origin/main` at `61abdfd` (2026-11-21); mnemcik/cg-extensions at `87c51e5`;
  mnemcik/cg-extensions-registry at `37db607`

## Question

An extension declares `{ "namespace": "<ns>", "binary": "<name>" }`, and `cg <ns> …` runs that binary. The issue
names three consequences: a namespace is backed by one binary and a second extension declaring it is silently
ignored; the binary's name doesn't follow from the command; resolution depends on the current directory, and
outside a workspace only a `cg-<namespace>` on `PATH` resolves.

Two questions follow:

- **A.** How are extension commands named and resolved, so that a typed command maps to exactly one program?
- **B.** How does a command run outside the workspace (for example from an external repo an agent session works
  in) find the workspace it belongs to?

## Current state

**Dispatch** (`cmd/external.go`):

- Built-ins always win: `externalDispatch` returns early when cobra resolves the verb (`:23-51`).
- The workspace comes from the current directory only: `workspaceRootForDispatch` → `os.Getwd()` →
  `workspace.FindRoot` (`:111-118`).
- With a root, cg walks `cfg.Extensions` in `.cg.json` order and returns the **first** ledger whose namespace
  matches and whose `bin/<binary>` exists (`:58-83`). There is no conflict check and no warning, at dispatch or at
  install.
  - Two installed extensions declaring `shop`: the one listed first runs, the other is unreachable, and nothing
    says so.
  - A namespace equal to a built-in (`start` became one in 1.32.0) is shadowed without a warning.
- With no root or no match, cg falls back to `exec.LookPath("cg-" + namespace)` (`:85`).
- `runExternal` passes stdio only (`:94-108`): the binary is told nothing about the workspace.

**Manifest** (`internal/extension/manifest.go`): `binary` is free-form within `[a-z0-9-]+` (`:189-191`). The docs
contradict each other: the example maps `secret` to `cg-1password` (`docs/extensions.md:144-146`), while the
discovery section says the resolver looks for `cg-<verb>` (`docs/extensions.md:403-408`). The code uses the
declared binary.

**The launcher already does what dispatch doesn't**: `findLauncher` (`cmd/start.go:198-238`) does not trust the
committed ledger alone. It reads the manifest in the extension's pinned tree, requires it to validate and declare
the same entry, and refuses two launchers by name.

**Binaries find the workspace themselves**: `cg-shop` uses `git rev-parse --show-toplevel` of the current directory
(cg-extensions `shopping/bin/cg-shop:28-30`). Run from an external repo, it would write that repo's `shopping/`.

**What uses subcommands today**: of the 11 extensions in the public registry, two: `live-runs` (`live` → `cg-live`)
and `shopping` (`shop` → `cg-shop`).

**What is coming**: the `pr-workflow` and `coderabbit` extensions (cg-extensions #45, #46, #49, #50) add commands
that read workspace configuration (role, repo lists, gate settings) and are run by agents from external repos'
checkouts. They wait on this issue for their command names.

**Command names are a permission boundary.** Agent tools grant commands by their typed text. In Claude Code, for
example, `Bash(cg coderabbit approve *)` allows any command starting with that text
(https://code.claude.com/docs/en/permissions). An entry like that is only as narrow as the mapping from a typed
command to one program: if another extension can answer a longer command under the same prefix, the grant reaches
code its author never reviewed.

**How a session carries its environment**: a variable set before the agent starts reaches the commands the agent
runs, including after it changes into an external repo. A new tmux session takes only `PATH` from the client and
the rest from the tmux server, unless `new-session -e VAR=value` passes it; `cg-tmux` passes no `-e` today
(cg-extensions `tmux/bin/cg-tmux:51`).

**Session state**: cg keeps a per-session state file, `.claude/session-context/<session-id>.json`
(`internal/session/context.go:44-50`), recording area, project, dirty and paused. It doesn't record a worktree. The
session ID comes from `--session-id`, `CG_SESSION_ID` or `CLAUDE_CODE_SESSION_ID` (`cmd/session.go:350-357`).

## Validity

| Question | Answer |
|---|---|
| Does the problem exist? | Yes, all three points, plus a fourth: binaries are never told their workspace, so a writer such as `cg shop add` uses the wrong repo from an external directory |
| Is the expected behaviour right? | Mostly. "One binary per namespace" is a normal model (gh); the defects are that conflicts are silent and that the name and the current directory decide what runs |
| Is it still current? | Yes; the code is unchanged on `origin/main`, and four cg-extensions issues wait on it |
| Is it the right problem? | Wider: nothing tells cg or a binary which workspace a session serves. Fixing names alone (putting extension `bin/` directories on `PATH`, #251) doesn't help commands that need the workspace |

## Options considered

**For A (naming and resolution):**

1. **One extension per namespace** (gh-style): install refuses a namespace already owned or built in; the binary
   is fixed to `cg-<namespace>` and parses its own commands. Simple, but two extensions can never add commands
   under a shared word such as `cg pr …`.
2. **kubectl-style `cg-<namespace>-<command>` binaries, longest match**: shared namespaces work, but it needs an
   escaping rule for dashes, one file per command, and a prefix-scanning resolver; and under longest match another
   extension can ship `cg-coderabbit-approve-force` and receive calls a `cg coderabbit approve *` grant allows.
3. **Commands declared in the manifest; cg owns the command tree** (chosen): each extension lists the commands it
   provides and which of its binaries serves each. cg refuses duplicate and nested commands across extensions and
   commands that shadow a built-in, at install time, and dispatch is a table lookup. Shared namespaces work, and a
   grant on a command only ever runs the code of the extension that declared it. Costs a manifest change and a
   larger change in cg.

**For B (workspace discovery):**

1. **Extension `bin/` directories on `PATH`** (#251): no new contract, but binaries still learn no workspace, writers
   reach the wrong repo, and every `bin/` helper becomes a bare command.
2. **The session carries its workspace** (chosen): `cg start` exports `CG_WORKSPACE`; dispatch prefers the
   session's workspace and tells each binary the root that served it.
3. **A `--workspace` flag only**: every call needs a path the agent doesn't reliably know. A fine later addition for
   plain terminals.
4. **An agent-specific variable** (Claude Code's project directory): not set in Bash commands, and tool-specific.

## Decision

Posted on the issue (2026-11-21):

1. **Commands are declared in the extension's manifest**, each mapped to one of the extension's binaries. cg owns
   the command tree. A command declared twice, a command declared inside another extension's command, or one that
   shadows a built-in is refused at install, update and `cg init`, before anything is applied. Several extensions
   may share a namespace. Dispatch is a table lookup.
2. **No fixed `cg-<namespace>` binary name, and no rule that the namespace equals the extension name.**
3. **The session carries its workspace:** `cg start` exports `CG_WORKSPACE`, never rewritten by dispatch. cg tells
   each extension binary which workspace root served it (`CG_EXTENSION_WORKSPACE`), and unsets that for a binary
   found through the `PATH` fallback.
4. **Precedence:** the current directory's workspace serves a command on its own only when it is the session's
   workspace or one of its worktrees (when that can't be shown, it counts as another workspace). Otherwise: the
   session's workspace, when it provides the command; then the current directory's other workspace; then the
   `PATH` fallback.
5. **Writers find the session's worktree** from the session's state file, which records the worktree once the
   session enters it. A writer started outside the workspace refuses when none is recorded.
6. **Naming rule:** a subject namespace is vendor-neutral (`cg pr merge`, `cg pr watch`); vendor-specific commands
   use the vendor's namespace (`cg coderabbit probe|review|approve`). `live` and `shop` are unchanged.
7. **The `cg-<namespace>` `PATH` fallback stays**, last in order, and only for a command no installed extension
   declares.
8. **`commands` is required and the old `subcommands` form is removed**, with no compatibility layer: cg 2.0.0,
   released with the registry schema and new `live-runs` and `shopping` manifests.
9. **Dispatch re-checks the manifest in the extension's pinned tree** before running a command, as `findLauncher`
   does.
10. **#251 is closed and replaced** by the PR for this issue, which carries over its unknown-command hint and trust
    helper and closes #248.

The spec ([spec.md](spec.md)) turns these into acceptance criteria; the plan ([plan.md](plan.md)) orders the work.

## Hidden constraints

- **Breaking change:** removing `subcommands` is a `BREAKING CHANGE` (CONTRIBUTING, Release process) → 2.0.0.
  Workspaces must run `cg extension update` for `live-runs` and `shopping` after updating cg.
- **Contract names:** `CG_WORKSPACE`, `CG_EXTENSION_WORKSPACE` and the manifest's `commands` shape become public on
  release; rename them before, never after.
- **No partial state:** the conflict check runs over every extension an operation applies (`cg init` re-applies
  several) before any of them is applied.
- **Cross-repo order:** the registry schema must accept `commands` before the new manifests are published; `cg-tmux`
  must pass `CG_WORKSPACE` into tmux sessions, or sessions started through it don't carry the workspace.
- **Windows:** extension subcommands don't run on Windows yet (#239); the dispatch changes must still build and their
  tests run or skip there with a reason.
