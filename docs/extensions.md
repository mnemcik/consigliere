# `cg` extension system — manifest, registry, and `cg extension`

This document is the authoritative design for the Consigliere extension system:
the `cg-extension.json` manifest, the central registry, the `cg extension`
subcommand suite, the contribution points an extension can declare, and how
cg runs the commands extensions declare.

It is the framework-side counterpart to the author-facing
[`EXTENSIONS.md`](../EXTENSIONS.md) guide. When the two disagree, this file is
the spec and `EXTENSIONS.md` is the tutorial; fix the tutorial.

## Why extensions exist

Consigliere is **domain-agnostic** framework scope: generic conventions
(session-start gate, DRY, information propagation, worktree mechanics) ship in
the framework; workspace- or domain-specific behaviour does not. But users still
want to opt into domain rules — 1Password as the credential store, team backlog
standards, a Gmail-digest workflow, a voice/drafting persona — without forking
the framework or hand-editing their `CLAUDE.md` against the next `cg sync`.

Extensions are the pluggable layer for exactly that. Each extension is an
independent git repo declaring, via a manifest, a set of **contributions** it
applies to a workspace on install and reverses on removal. The framework never
inlines any of this; it ships the *mechanism*, not the content.

Decision lineage: `consigliere-gap-analysis` DEC-001 (framework vs. extension
boundary) and DEC-002 (separate repos + central registry + manifest
contributions); project decisions DEC-001 (manifest schema v1) and DEC-002
(install layout + per-workspace ledger).

## Model at a glance

```
  registry repo                  extension repo                 workspace
  (mnemcik/cg-                   (e.g. mnemcik/cg-              (the user's
   extensions-registry)           ext-1password)                knowledge base)
  ┌───────────────┐              ┌──────────────────┐          ┌──────────────────┐
  │ index.json     │  resolve    │ cg-extension.json │  apply   │ CLAUDE.md         │
  │  name → repo   │ ───────────▶│ contributes: {…}  │ ────────▶│  ext:<n>:section… │
  │  + manifestUrl │             │ fragments/        │          │ notes/…           │
  └───────────────┘              │ notes/ hooks/     │          │ .claude/hooks/…   │
                                 │ templates/ bin/   │          │ templates/…       │
                                 └──────────────────┘          │ .cg/ext/<n>.json  │ ◀ ledger
                                                                │ .cg.json          │ ◀ extensions[]
                                                                └──────────────────┘
```

- **Registry** (`mnemcik/cg-extensions-registry`): a JSON index mapping a short
  name to a repo URL + manifest URL. Decoupled from the `cg` release cadence so
  the catalogue can grow without a binary release. A workspace can name several
  registries (public + private) and installs are fully qualified —
  see [Multiple registries](#multiple-registries-fully-qualified-names).
- **Extension repo**: holds `cg-extension.json` plus the contribution payloads.
- **Store**: `cg extension install` fetches the repo into a machine-shared
  store, keyed by repo and commit, and records the installed commit in the
  workspace. See [Install layout](#install-layout-dec-002).
- **Workspace**: contributions are *applied* per-workspace and tracked in a
  per-workspace ledger (`.cg/ext/<name>.json`) + `.cg.json` `extensions[]`.

## Install layout (DEC-002)

The extension source is **per machine**, XDG-compliant, and keyed by **repo and
commit**, so no workspace's install or update can change what another workspace
reads:

```
~/.config/consigliere/extensions/
  mirrors/<key>.git                  # one bare mirror per repo URL: fetches, ref resolution
  trees/<key>@<commit>/              # one exported tree per commit, never modified after it is written
    <path>/                          # the extension's manifest dir (repo root when path is empty)
```

`<key>` is the repo URL's last path segment plus 12 hex characters of the URL's
SHA-256, which keeps it readable, collision-safe and free of path traversal. A
local repo is recorded and keyed by its absolute, symlink-resolved path. The
mirror fetches only branches and tags, and follows the remote's default branch.
Only a commit that is on a branch or tag, as of the store's last fetch, is installed or restored.
A tree is checked out through a private index into a staging directory and
renamed into place, so a reader never sees a partial one. Each workspace records
the commit it installed in `.cg.json` (`extensions[].commit`): install, update,
`cg init`'s re-install, git-hook approval and command dispatch all
read the tree at that commit. Co-located extensions at one commit share a tree,
and same-named extensions from different repos never meet. Trees are kept when
an extension is removed, since other workspaces may use them; a prune step does
not exist yet.

An entry recorded before the store (no `commit`) reads the legacy name-keyed
clone at `~/.config/consigliere/extensions/<name>/` until its next install or
update records a commit.

A manifest dir looks like this:

```
<tree>/<path>/
  cg-extension.json
  fragments/<id>.md                  # CLAUDE.md section bodies
  notes/<file>.md                    # notes to copy into the workspace
  hooks/<file>.sh                    # hook wrappers (DEC-004 style: exec a binary …)
  templates/<path>                   # templates to copy into the workspace
  bin/<binary>                       # binaries serving the extension's commands
```

`$XDG_CONFIG_HOME` is honoured when set; otherwise `~/.config`. This is the
**same `~/.config/consigliere` root** that `cg`'s auto-update state already uses
(`internal/autoupdate.StateDir`) — extensions deliberately do not introduce a
second config tree (project DEC-002, 2026-06-14).

What was *applied to a given workspace* is recorded **in that workspace**, not in
the store, because one tree may back several workspaces:

```
<workspace>/.cg/ext/<name>.json      # install ledger: exactly what this workspace got
<workspace>/.cg.json                 # extensions[] entry (re-install on cg init)
```

This refines the project todo's original `~/.config/consigliere/extensions/<name>/.install-log.json`
placement: the ledger must be per-workspace so `cg extension remove` reverses the
right workspace, and so two workspaces sharing one tree don't fight over one
log. See DEC-002.

## Manifest schema v2 (`cg-extension.json`)

Lives at the root of every extension repo. The schema is intentionally minimal;
the `"manifest"` integer versions the schema itself for forward compatibility.
It forbids cross-extension dependencies (declare none).

Version 2 adds [`commands`](#extension-commands). A manifest that declares
commands must say `"manifest": 2`; a version 1 manifest without commands still
installs, so an extension that contributes no command needn't change. cg 1.x
refuses a version 2 manifest with "unsupported manifest schema version".
Version 1's `subcommands` key is gone: cg ignores it like any unknown key, so
an extension that still uses it contributes no command until it moves to
`commands`.

```json
{
  "manifest": 2,
  "name": "1password",
  "version": "0.1.0",
  "description": "1Password as authoritative credential store",
  "contributes": {
    "claude-md-sections": [
      { "id": "credentials-1password", "path": "fragments/credentials.md" }
    ],
    "notes": [
      { "src": "notes/credentials-policy.md", "dest": "notes/credentials-policy.md",
        "index": "how to handle credentials through 1Password" }
    ],
    "hooks": [
      { "event": "SessionStart", "wrapper": "hooks/credentials-gate.sh", "command": "cg-1password gate" }
    ],
    "commands": [
      { "command": "secret get", "binary": "cg-1password" },
      { "command": "secret put", "binary": "cg-1password" }
    ],
    "templates": [
      { "src": "templates/credential-request.md", "dest": "templates/credential-request.md" }
    ],
    "git-hooks": [
      { "hook": "post-commit", "script": "git-hooks/check.sh" }
    ],
    "gitignore": [
      { "id": "1password-cache", "path": "fragments/gitignore" }
    ]
  }
}
```

### Field reference

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `manifest` | int | yes | Manifest **schema** version: `2`, or `1` for a manifest without `commands`. `cg` refuses a manifest whose `manifest` it doesn't understand. |
| `name` | string | yes | Short, lowercase, `[a-z0-9-]+`. The install name, the `.cg.json` key, the legacy clone directory and the `ext:<name>:section` namespace. Must be unique in the registry. It needn't match a command or a binary. |
| `version` | string | yes | Extension semver. Recorded in `.cg.json`; compared on `cg extension update`. |
| `description` | string | yes | One line, shown in `cg extension list` and the registry. |
| `contributes` | object | yes | The contribution points (all eight keys below). Each is an array; an empty/absent array means "contributes nothing of this type". |

### Contribution points

1. **`claude-md-sections`** — `[{ id, path }]`. Inserts the body of `path` into
   the workspace `CLAUDE.md` as an extension-owned section, delimited by
   `<!-- ext:<name>:section:start=<id> -->` / `<!-- ext:<name>:section:end=<id> -->`.
   This namespace is distinct from framework `cg:section` and user `user:section`
   blocks, so `cg sync` (which parses only `cg:section`) never touches extension
   fragments — and an extension can never clobber a framework section.
   Re-install/update replaces the section body in place; remove deletes the block.

2. **`notes`** — `[{ src, dest, index? }]`. Copies `src` (relative to the
   extension repo) to `dest` (workspace-relative, conventionally under
   `notes/`). A copied note is the user's to edit: see
   [Files you change](#files-you-change). The notes are also listed in the
   workspace notes index: see [Notes index rows](#notes-index-rows).

3. **`hooks`** — `[{ event, wrapper, command }]`. Installs the bash wrapper at
   `wrapper` into `.claude/hooks/`, registers it for `event` in
   `.claude/settings.json` (additive merge — never clobbers existing hooks or
   user padding), and the wrapper `exec`s `command` (typically one of the
   extension's binaries).
   Mirrors framework hook style (DEC-004): user-readable, degrades to a no-op
   when the binary is absent.

4. **`commands`** — `[{ command, binary }]` (manifest v2). Each entry declares
   that `cg <command> …` runs the extension's `bin/<binary>`. A command is one
   or more words, each `[a-z0-9][a-z0-9-]*` (a letter or digit first: a word
   starting with `-` could never be typed), separated by single spaces (`live run`,
   `coderabbit approve`); its first word is its **namespace**. `binary` is any
   `[a-z0-9-]+` file in `bin/`, and one binary may serve several commands. The
   binary is recorded in the ledger, **not** copied into the workspace. cg owns
   the command tree: see [Extension commands](#extension-commands) for the
   rules install enforces and how a typed command finds its program. Not
   supported on Windows yet
   ([#239](https://github.com/mnemcik/consigliere/issues/239)).

5. **`templates`** — `[{ src, dest }]`. Copies template files into the
   workspace `templates/` tree (same copy semantics as notes, including
   [Files you change](#files-you-change), without the index rows).

6. **`git-hooks`** — `[{ hook, script }]`. Copies the executable `script` to
   `.cg/git-hooks/<hook>.d/<name>-<base>` in the workspace, where it is
   versioned like any other file. `hook` is a client-side git hook:
   `pre-commit`, `prepare-commit-msg`, `commit-msg`, `post-commit`,
   `post-checkout`, `post-merge`, `post-rewrite`, `pre-push` or `pre-rebase`.
   What runs the scripts is a cg dispatcher at `.git/hooks/<hook>`, which
   gives each script the hook's arguments and stdin (a script without the
   execute bit runs through `sh`) and exits non-zero if any script did,
   which only matters for `pre-*` hooks. The dispatcher lives in the shared
   git dir, so commits in every worktree run it, and it reads the scripts from
   the main worktree: a script added in a session worktree takes effect once
   it has landed.
   - **Trust.** Git never versions hooks, so that a pull or a checkout cannot
     make code run. Versioned scripts would undo that, so a script runs only
     when it is *approved*: its content must match the same script in the
     store's tree at the commit `.cg.json` records for that extension. That is
     a commit of the recorded repo that is on one of its branches or tags (the
     store fetches nothing else, so fork refs such as `refs/pull/*` never
     qualify). A recorded commit can still arrive by pull, so `cg init`
     restores code the `.cg.json` names, not only code installed by hand on
     this machine. cg writes the approved scripts (path and git
     blob id) to `cg-hooks.allow` in the git dir, which is not versioned. The
     dispatcher runs only listed scripts whose content still matches, and
     warns about any other; cg reports unapproved scripts too. A script that
     arrives by pull or checkout therefore does not run until `cg extension
     install` or `update` approves it, unless the tree for that exact commit is
     already on this machine (below), and on a new machine scripts start running
     once `cg init` or `cg extension install` has fetched it.
     Approval follows `.cg.json`, and trees are shared by every workspace on the
     machine. So a `.cg.json` entry, including one that arrives by pull,
     approves the scripts of a commit already in the store for another
     workspace, without an install in this one.
   - **Reconciling.** `.git/hooks` is not versioned, so cg installs or removes
     dispatchers to match the approved scripts on `cg extension
     install|update|remove`, `cg init`, `cg sync --apply` and every session
     start (`cg session pull-latest`, which reports problems as a session
     message).
   - A hook file that existed before cg is moved to `<hook>.pre-cg`, runs
     first, and is restored when the last approved script for that hook goes.
   - When `core.hooksPath` is set, cg installs nothing and warns, rather than
     write into a directory that may be versioned or shared.
   - Requires git 2.31 or later (`rev-parse --path-format`).

7. **`gitignore`** — `[{ id, path }]`. Inserts the lines in `path` into the
   workspace `.gitignore` as an extension-owned block, delimited by
   `# ext:<name>:gitignore:start=<id>` / `# ext:<name>:gitignore:end=<id>`
   (comments, so git ignores the markers). It works like `claude-md-sections`:
   `.gitignore` is created if there is none, re-install/update replaces the
   block in place, and remove deletes it. Remove also deletes a `.gitignore`
   left holding nothing but whitespace, so an install that created the file
   leaves no trace (this also removes a `.gitignore` the user had left empty
   before the install). Lines outside the block are kept; the only edits outside it are a final newline added to a file that lacks one, and the one blank line put before the block, which remove takes away again. Markers are
   matched as whole lines, so an id that is a prefix of another (`cache`,
   `cache-v2`) never matches the longer one.
   A cg older than this contribution point ignores the key: it doesn't write the
   block, and it doesn't know about a block a newer cg wrote, so its `remove`
   leaves that block behind.

8. **`launcher`** — `[{ binary, args? }]`, at most one entry. Makes
   `cg start` (and bare `cg` in a workspace) run `bin/<binary> <args…> --
   <agent argv…> <user args…>` instead of the agent (`session.agent`) directly;
   the binary is expected to exec the agent itself, e.g. inside a terminal
   multiplexer:

   ```json
   "launcher": [{ "binary": "cg-tmux", "args": ["launch"] }]
   ```

   Like `commands` it touches no workspace files and is recorded in the
   ledger. Because the ledger is committed in the workspace, `cg start` doesn't
   trust it alone: it runs a launcher only when the extension's own manifest,
   in its tree at the recorded commit, validates and declares the same entry,
   and the binary exists there. Otherwise it warns, naming the extension, and
   starts the agent directly; `cg init` restores a missing tree. When two
   installed extensions contribute a launcher, `cg start` refuses and names
   them (bare `cg` prints help instead), and a launcher that fails to start is
   named in the error. Not supported on Windows yet
   ([#239](https://github.com/mnemcik/consigliere/issues/239)).

   A cg older than this contribution point ignores the key, so the extension
   still installs and `cg` keeps its older behaviour; it records no launcher in
   the ledger either. After upgrading cg, run `cg extension update <name>` to
   get the launcher. That also moves the extension to its latest version.

## `cg extension` subcommand suite

| Command | Behaviour |
|---------|-----------|
| `cg extension install <registry>/<name>` | Resolve `<name>` in the **named** registry `<registry>` (from `.cg.json` `registries`; the built-in `cg` alias is the public catalogue) → fetch `repo` into the store and export the tree for the resolved commit → read manifest → apply all contributions → write ledger + `.cg.json` entry, including the commit. **Names are always fully qualified** — a bare name is rejected, so the source is unambiguous (no first-match search, no [dependency confusion](#multiple-registries-fully-qualified-names)). |
| `cg extension install … --force` | Overwrite existing files the extension ships that no ledger records as its own. Without it, install refuses, before changing anything, when such a file differs from the extension's version. See [Files you change](#files-you-change). |
| `cg extension install <repo-url>` | Skip the registry: fetch the URL directly, read its manifest, take `name` from the manifest. Source recorded as `direct`. |
| `cg extension install … --ref <tag\|branch\|commit>` | Install at that ref. Default: the default branch. |
| `cg extension install <repo-url> --path <subdir>` | Install a co-located extension whose manifest lives in `<subdir>` of a monorepo. Direct installs only; registry entries carry their own `path`. See [Co-located extensions](#co-located-extensions-monorepo). |
| `cg extension list [--json]` | List installed extensions for the current workspace: name, version, source, repo, and the commands each declares. A command whose namespace is a built-in is flagged: it can't run, because the built-in wins. |
| `cg extension remove <name> [--purge]` | Reverse every contribution recorded in the workspace ledger (delete the `ext:<name>:section` block, copied notes/templates that are still as cg wrote them, hook wrapper + settings entry, git-hook scripts and any dispatcher left with nothing to run, the notes index block), drop the `.cg.json` entry, delete the ledger. The store's trees are shared, so they stay; `--purge` deletes only a legacy name-keyed clone. |
| `cg extension update [<name>]` | Move a **single-repo** extension to its latest tag (or its default branch when untagged), record the new commit, then re-apply contributions (replace-in-place). A **co-located** (subdir) extension instead tracks the default branch and versions from its manifest — see [Co-located extensions](#co-located-extensions-monorepo). No `<name>` updates all installed extensions. Copied notes and templates you changed are kept: see [Files you change](#files-you-change). Every target's new version is fetched and its [commands checked](#extension-commands), with the extensions not being updated, before any target is applied. |
| `cg extension pending [--json]` | List copied files that both you and the extension changed, which update left as yours. |
| `cg extension diff <name> <file>` | For a pending file, show the extension's change and yours, each against what cg installed, and whether a three-way merge is clean. |
| `cg extension resolve <name> <file> --merge\|--keep\|--take` | Settle a pending file: merge the extension's change into yours (refused unless the merge is clean), keep yours, or take the extension's. Each makes the extension's version the new baseline. |

**Every workspace file is written atomically.** Install, update, remove and rollback write each file (copied notes, templates, hook wrappers, git-hook scripts, `CLAUDE.md`, `.gitignore`, `settings.json`, the ledger, `.cg.json`) to a temp file in the same directory and rename it into place. A hook or shim run by a parallel session therefore sees the old file or the new one, never a half-written one. A failed write leaves the previous file intact. Copied files get a fixed mode (`0644`, or `0755` for hook and git-hook scripts); `CLAUDE.md`, `.gitignore`, `settings.json`, the ledger and `.cg.json` keep the mode they have, and a new one gets `0644` regardless of the umask. A file without its owner write bit is refused rather than replaced. This checks the mode bits, not access rights, so it also refuses root, and it doesn't catch a writable-looking file owned by someone else. A symlink is followed, so its target is replaced and the link survives. Hard links to a replaced file keep the old content.

### Notes index rows

When the workspace has a notes index, install lists the extension's notes in
it. The index is the file `.cg.json` names under `indexes.notes`, or
`notes/INDEX.md` when it names none. With no index file, nothing is written
and none is created.

The rows go in one block per extension, appended at the end of the index under
their own heading, so removal is exact and extensions never share a block:

```markdown
<!-- ext:1password:index:start -->
## From the 1password extension

- [Credentials & Secrets — 1Password in the Loop](credentials-policy.md) — how to handle credentials through 1Password
<!-- ext:1password:index:end -->
```

- **Link text** is the note's frontmatter `title`, else its first `# ` heading,
  else its file name.
- **Description**, after the dash, is the optional one-line `index` field of
  the note's manifest entry. Without it the row is the link alone.
- **A note the index already links to**, anywhere outside this block, gets no
  row, so a row you placed by hand stays the only one. With every note linked,
  the block is removed. A link counts when it is a markdown inline link
  (`[text](path)`) or a reference definition (`[label]: path`) whose path,
  resolved from the index's directory, is the note. Wikilinks don't count.
- **Broken markers** (a start with no end, or a block that appears twice) make
  install, update and remove stop with an error naming the index, rather than
  guess where the block ends. Fix or delete the markers and run it again.
- **Line endings:** an index whose every line ends in CRLF is written back as
  CRLF. Any other index is edited as it is.
- **A note you changed** is listed as it is on disk, and one you deleted gets
  no row. `cg extension resolve` doesn't touch the index, so after it the row
  catches up on the next update or reinstall.
- The block is cg's: update rewrites it and remove deletes it, so put your own
  rows outside it. The ledger records it under `indexRows`.

Templates get no rows.

### Files you change

Copied notes and templates are yours to edit once installed. The ledger records
the git blob id of the version cg last wrote for each one (`files`), and install,
update and remove compare three versions: that one (the base), what is on disk,
and what the extension ships now.

| On disk vs base | Extension vs base | What cg does |
|---|---|---|
| unchanged | changed | updates the file |
| changed | unchanged | leaves your file alone, silently |
| changed | changed | leaves your file alone and records it as **pending** |
| (no base: not installed by cg) | differs from disk | install refuses without `--force` |

Deleting a copied file is a change too: cg doesn't recreate it, and raises it as
pending if the extension changes it. `remove` deletes a copied file only when it
is still as cg wrote it, and says which ones it kept. A file a new version no
longer ships is treated the same.

Content is compared with line endings normalised to LF, so a CRLF checkout of
the workspace (Git for Windows' default `core.autocrlf`) matches the repo's LF
version, and for a repo that stores LF the ids are its own blob ids on every
machine. If an
extension's repo stores a file with CRLF, `diff`, `--merge` and `--take` can't
read its versions and say so; `--keep` still works.

A ledger from before cg recorded these ids uses the previous version's own files
as the base. That base is exact when the entry records a commit (the store's tree
for it). It is **uncertain** when it comes from a legacy name-keyed clone, which
may have moved since the install: a file that matches neither it nor the new
version is then raised as pending, never kept silently. When there is no base at
all (a legacy entry whose clone is gone), a file that differs from the new
version is raised too, and only `--keep` or `--take` apply.

**Pending files are raised at the next session start.** `cg session pull-latest`
(the framework `SessionStart` hook) adds a one-line notice for the user and
context for Claude, which tells Claude to go through each file with the user:
run `cg extension diff`, recommend merge, keep or take with a reason, and run
`cg extension resolve` with the user's choice. The record is in the workspace
ledger, so it travels with the workspace: an update nobody watched, such as
`cg init` on a new machine, is raised in the next session wherever it runs. The
notice repeats every session until the file is resolved.

Hook wrappers and git-hook scripts are not covered: they are code that must
match the extension's source for the trust model, so cg always owns them.
`claude-md-sections` and `gitignore` blocks are delimited and replaced as a
whole.

### Re-install on fresh clone

`.cg.json` records each installed extension and its commit. For every
`extensions[]` entry whose tree is missing on this machine, `cg init` fetches
**that commit** and re-applies its contributions, so every machine applies the
same content no matter what upstream holds by then. An entry without a commit
is installed from the default branch, and the commit is recorded. Every missing
tree is fetched and the [commands](#extension-commands) of the whole set (the
re-installed extensions and those already present) checked before the first is
applied: on a conflict `cg init` reports every conflict and re-installs none.

## Extension commands

An extension declares the commands it provides in its manifest's
[`commands`](#contribution-points), each served by one of its binaries. cg
owns the command tree, so a typed command maps to exactly one program, and a
permission grant written for a command (`Bash(cg coderabbit approve *)`) only
ever reaches the code of the extension that declared it.

### Naming

- A **subject namespace** is vendor-neutral and may be shared:
  `cg pr merge` and `cg pr watch` can come from different extensions.
- **Vendor-specific commands** use the vendor's namespace:
  `cg coderabbit probe`, `cg coderabbit approve`.
- Neither the namespace nor the binary has to match the extension's name.

### Ownership, checked before anything is applied

`cg extension install`, `cg extension update` and `cg init`'s re-install compare
the incoming manifests' commands with the commands of every other extension
installed in the workspace, and refuse, naming the commands and extensions,
before changing anything, when:

- another extension already declares the same command;
- a command is inside another extension's command, or the reverse: with `pr`
  installed, `pr merge` is refused, and with `pr merge` installed, `pr` is;
- its namespace is a built-in cg command or alias (`start`, `extension`,
  `ext`, `help`, `completion`, …) and the extension doesn't already own it:
  built-ins always win, so it could never run.

Two extensions may declare different commands in one namespace (`pr merge`
and `pr watch`). One extension may nest its own commands (`shop` and
`shop add`). An update that keeps an extension's own commands, or moves a
command from one extension to another in a single `cg extension update`, is
not a conflict. A removed extension's commands are free again.

An installed extension's commands are read from its ledger and counted only
when its manifest, in its tree at the recorded commit, declares the same
command with the same binary and that binary exists. A ledger that can't be
read might declare anything, so install, update and `cg init` refuse incoming
commands until it is restored; and an extension being installed, updated or
restored needs its own ledger readable, so nothing is applied when one isn't.
The ledger is committed in the workspace, so it isn't trusted on its own, just
as for the [launcher](#contribution-points).

A built-in added by a later cg can shadow a command an extension declared
earlier. The extension still **owns** that command: its installed version's
manifest, at the recorded commit, declares it (the ledger alone doesn't count;
a missing binary doesn't end ownership, an unreadable tree owns nothing). An
update that keeps an owned command goes ahead with a warning; one that adds a
new command in the built-in's namespace, or moves the command to another
extension, is refused. `cg init` restores the recorded versions, so it only
warns. The built-in still wins at dispatch, and `cg extension list` flags the
command: it can't run.

### Dispatch

When `cg <words> …` is not a built-in, cg matches the typed words (the leading
arguments after cg's own flags, up to the first one starting with `-`) against
the declared commands of the workspaces it consults, in order (see
[Which workspace serves](#which-workspace-serves)):

1. The first workspace whose ledgers have any command in the typed namespace
   (whether or not it checks out, see 3) decides it; later workspaces and
   `$PATH` aren't consulted for it. The longest declared command the typed words start
   with wins, and its binary runs with every argument after the namespace word,
   as git and gh do: `cg live run --until-pane x` runs `bin/cg-live run
   --until-pane x`. The binary finds the command cg matched in `CG_COMMAND`
   (`live run`, or `live` for a bare-namespace entry).
2. A typed command that matches none of the namespace's declared commands is a
   usage error that lists them. Because matching stops at the first flag,
   `cg live --x run` matches only `live`: an extension that wants a bare
   namespace to reach its binary declares the namespace itself too.
3. Before running a binary, cg re-checks the entry against the extension's
   manifest in its tree at the recorded commit, as above. An entry that fails
   stops dispatch with an error naming the extension and the fix, to run in the
   workspace that served it. For a damaged tree (its manifest can't be read, or
   the binary is gone): remove the extension's store tree if it is still there
   and run `cg init --force` (which restores only a missing tree), or
   `cg extension update <name>`. For a ledger that disagrees with an intact
   manifest: `cg extension update <name>`, which also moves it to its latest
   version (the tree is shared by other workspaces, so it stays). For a legacy
   entry without a recorded commit, remove its clone if it is still there;
   `cg init --force` then reinstalls it from its default branch. For an invalid `path` in `.cg.json`: correct the entry. cg never falls back to another program for it. Two
   ledgers declaring the same command in the namespace (a hand-edited or pulled
   `.cg.json`) stop dispatch too. So does a `.cg.json` or ledger that can't be
   read (it might declare the command; the error names the file to restore),
   and a current directory cg can't determine.
4. When no consulted workspace declares any command in the namespace, cg runs a
   `cg-<namespace>` executable on `$PATH`, if there is one, with the arguments
   after the namespace (a plain name: one with a path separator or a colon is
   never looked up). A spelling the lookup would still match to a declared
   namespace is refused too: another case on macOS and Windows, and on
   Windows any extension (so `cg shop.v2` is refused when `shop` is
   declared). A namespace an extension declares never reaches `$PATH`, whether or not
   the full command matched.

Otherwise cobra's "unknown command" error is shown, with a hint that an
extension's command runs inside the workspace that installed it, and whether
`CG_WORKSPACE` is set. Built-in commands always win over extension commands
and `$PATH` programs. No extension `bin/` directory is added to `$PATH`.

### Which workspace serves

`cg start` (and bare `cg`) starts the session with `CG_WORKSPACE` set to the
absolute workspace root it resolved, a worktree's root when started in one. A
launcher must pass it on into the session it creates (a terminal multiplexer
doesn't by default: `tmux new-session` needs `-e CG_WORKSPACE=…`). Dispatch
reads it (and never rewrites it) to pick the workspaces it consults:

| Where the command runs | Consulted, in order |
|---|---|
| `CG_WORKSPACE` unset | the current directory's workspace, then `$PATH` |
| inside the session's workspace | that workspace |
| inside one of its worktrees (listed by `git worktree list` in the session's workspace) | that worktree, so its own extension changes count |
| inside another workspace, e.g. a cloned repo's | the session's workspace, then the other one, then `$PATH` |
| outside any workspace, e.g. an external repo | the session's workspace, then `$PATH` |

The worktree list is read from the session's workspace only, with git's
repository variables (`GIT_DIR`, `GIT_WORK_TREE`, …) removed, and paths are
compared as the directories themselves (symlinks and, on a case-insensitive
filesystem, letter case don't matter). A listed path counts only while it is
still that worktree: its `.git` must point into the session's repository, so a
directory put where a deleted worktree was doesn't. When the list can't be read
or trusted (no git, the session's workspace isn't the top level of its
repository), the current directory's workspace counts as another workspace. A
`.cg.json` above the current directory that can't be read stops a command only
when the session's workspace doesn't declare its namespace (and always when it
is the session's workspace's or one of its worktrees').

A `CG_WORKSPACE` that is relative, names a directory that doesn't exist (a worktree removed since the
session started), one without a `.cg.json`, or one whose `.cg.json` isn't a
Consigliere workspace's is ignored, and the unknown-command hint says so. One
whose `.cg.json` exists but can't be read or decoded (a directory, a dangling
symlink, an empty file, invalid JSON, `null`, a non-object, a field of the wrong
type) stops dispatch, naming the file, with no fallback to another workspace or
`$PATH`; only inside one of the session's worktrees does that worktree's own
`.cg.json` serve instead.

Because the first workspace that declares a namespace decides it, a cloned
repository's workspace can't answer a command in a namespace the session's
workspace declares, even inside the clone: a grant written for the session's
commands never reaches the clone's code. Two workspaces can't split one
namespace between them; within one workspace, extensions can (`pr merge` and
`pr watch`).

### What the binary is told

| Variable | Set to |
|---|---|
| `CG_COMMAND` | the declared command that matched, e.g. `live run`, or `live` for a bare-namespace entry |
| `CG_EXTENSION_WORKSPACE` | the workspace root that served the command |
| `CG_WORKSPACE` | passed through unchanged: the session's workspace, when set |
| `CG_SESSION_WORKTREE` | the session's worktree, only when the session's workspace serves the command for a directory outside it and its worktrees (see below) |

A program found through the `$PATH` fallback gets none of `CG_COMMAND`,
`CG_EXTENSION_WORKSPACE` and `CG_SESSION_WORKTREE`, even when the caller had
them set. Dispatch never reads these per-run variables.

### Writers: where to write

A binary that writes into the workspace (a list, a note) must not write into
an external repository the session happens to be in, nor, when the session's
workspace serves it for such a directory, into the main checkout. cg tells it
where:

1. `CG_SESSION_WORKTREE`, when set: the session's worktree. Dispatch takes it
   from the session's state file (`worktree`, recorded by `cg worktree create`
   or `cg session set-context --worktree`; the session ID comes from
   `CG_SESSION_ID`, else `CLAUDE_CODE_SESSION_ID`), and only while it is still
   one of the session repo's linked worktrees (`git worktree list` in the
   session's workspace lists it, and it isn't the main checkout). Without such
   a record, a session started in a linked worktree gets that worktree (the
   session's workspace itself); one started in the main checkout gets none.
   That fallback (the linked-worktree test) needs git 2.31 or later (`rev-parse
   --path-format`); with an older git only a recorded worktree is passed on.
2. Else `CG_EXTENSION_WORKSPACE`, when the current directory is inside it.
3. Else, when `CG_EXTENSION_WORKSPACE` is set (the session's workspace served a
   command for an outside directory, with no worktree known), refuse, naming
   `cg worktree create` and `cg session set-context --worktree`.
4. Else (run directly, or through the `$PATH` fallback) the current work tree.

Inside the workspace or one of its worktrees, the current one serves the
command (rule 2): a session that works in a worktree should run its commands
from there.

## `.cg.json` v1.2 additions

Additive and optional; absence = no extensions. Existing fields unchanged.

```jsonc
{
  // … existing type / version / indexes / v1.1 blocks …
  "registries": {                                 // v1.3: named registries (alias → source)
    "cg": "https://raw.githubusercontent.com/mnemcik/cg-extensions-registry/main/index.json"
  },
  "extensions": [
    {
      "name": "1password",
      "version": "0.1.0",
      "source": "registry",                       // "registry" | "direct"
      "registry": "cg",                           // alias resolved through (registry installs); absent for direct
      "repo": "https://github.com/mnemcik/cg-extensions",
      "path": "1password",                        // optional: subdir for a co-located extension; absent = repo root
      "commit": "51559d0…",                       // full commit installed; absent for entries recorded before the store
      "installed": "2026-06-14T10:00:00Z"
    }
  ]
}
```

## Per-workspace install ledger (`.cg/ext/<name>.json`)

Records exactly what `install` applied so `remove` is exact and `update` can
diff. Not user-edited.

```jsonc
{
  "name": "1password",
  "version": "0.1.0",
  "claudeMdSections": ["credentials-1password"],
  "notes": ["notes/credentials-policy.md"],
  "hooks": [{ "event": "SessionStart", "wrapper": ".claude/hooks/credentials-gate.sh" }],
  "templates": ["templates/credential-request.md"],
  "commands": [{ "command": "secret get", "binary": "cg-1password" }],
  "indexRows": [{ "file": "notes/INDEX.md", "marker": "ext:1password:index" }],
  "gitHooks": [".cg/git-hooks/post-commit.d/1password-check.sh"],
  "gitignore": ["1password-cache"],
  // an extension that declares a launcher also gets:
  //   "launcher": [{ "binary": "cg-tmux", "args": ["launch"] }],
  // the git blob id of each copied file as cg last wrote it (see Files you change)
  "files": {
    "notes/credentials-policy.md": "3b18e512dba79e4c8300dd08aeb37f8e728b8dad",
    "templates/credential-request.md": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
  }
  // "pending": [...] appears only while a file is pending
}
```

## Registry repo (`mnemcik/cg-extensions-registry`)

A standalone public repo, deliberately decoupled from `cg`'s release cadence.

```
index.json                       # the catalogue
schema/index.schema.json         # JSON Schema for index.json
schema/cg-extension.schema.json  # JSON Schema for cg-extension.json (manifest v1)
README.md                        # how to add an extension (PR the index)
```

`index.json`:

```json
{
  "$schema": "https://raw.githubusercontent.com/mnemcik/cg-extensions-registry/main/schema/index.schema.json",
  "version": 1,
  "extensions": [
    {
      "name": "1password",
      "description": "1Password as authoritative credential store",
      "repo": "https://github.com/mnemcik/cg-ext-1password",
      "latestVersion": "0.0.0",
      "manifestUrl": "https://raw.githubusercontent.com/mnemcik/cg-ext-1password/main/cg-extension.json"
    }
  ]
}
```

`cg extension install <registry>/<name>` resolves `<registry>` to a source (see
below), fetches that one `index.json`, finds the entry, and clones `repo`.

## Multiple registries, fully-qualified names

A workspace can consult more than one registry — e.g. the public catalogue plus
a private, org-internal one. Registries are **named** in `.cg.json` under
`registries` (a map of alias → source), and installs are **always fully
qualified**:

```jsonc
// .cg.json
"registries": {
  "cg":    "https://raw.githubusercontent.com/mnemcik/cg-extensions-registry/main/index.json",
  "work":  "git@github-work:acme/cg-extensions-registry.git"
}
```

```console
cg extension install cg/1password         # public catalogue
cg extension install work/team-backlog    # private catalogue
```

- **Fully-qualified, never bare.** The alias names exactly one registry, so the
  source is unambiguous. A bare name (`cg extension install 1password`) is a
  hard error. This is deliberate: resolving a bare name across an ordered list
  of registries (first-match-wins) is a **dependency-confusion / name-shadowing**
  vector — a registry earlier in the list could silently supply a different
  extension than intended. Fully-qualified names remove that class of bug and
  any precedence rules along with it.
- **Transport is auto-detected from the source.** A raw HTTPS URL (e.g. a
  `…/index.json`, or any `http(s)://` endpoint) is fetched anonymously. A git
  source (`git@…`, `ssh://…`, or a URL ending in `.git`) is **cloned** and its
  root `index.json` read — over git's own auth, so a **private** registry repo
  works over SSH (e.g. the 1Password SSH agent) exactly like private extension
  *content* does.
- **The built-in `cg` alias** always resolves to the public catalogue and is
  **immutable**: `CONSIGLIERE_EXTENSIONS_REGISTRY` overrides it (a test / fork
  hook), otherwise it is `DefaultRegistryURL` — it is **not** overridable via
  `.cg.json`, so a committed or tampered config can't silently repoint `cg/<name>`
  to another registry. It resolves even in a workspace whose `.cg.json` predates
  the `registries` map; `cg init` also seeds it for visibility. It is a *named*
  registry addressed as `cg/<name>` — not a nameless default that bare names fall
  through to.
- **Unknown alias** is a hard error listing the configured registries. The
  installed-extension record (`.cg.json` `extensions[]`) stores the registry
  alias so `update` / `remove` / re-install re-resolve the same source.

## Co-located extensions (monorepo)

Several extensions can share one git repo, each in its own subdirectory — the
Claude-marketplace shape — so a maintainer with a handful of first-party
extensions doesn't run a repo apiece. The single-repo layout is still fully
supported; co-location is purely additive (gap-analysis DEC-010).

A registry entry addresses a subdir with an optional `path`:

```json
{
  "name": "1password",
  "description": "1Password as authoritative credential store",
  "repo": "https://github.com/mnemcik/cg-extensions",
  "path": "1password",
  "latestVersion": "0.1.0",
  "manifestUrl": "https://raw.githubusercontent.com/mnemcik/cg-extensions/main/1password/cg-extension.json"
}
```

`path` absent ⇒ the manifest is at the repo root (the single-extension default).
For a direct install, the user supplies the subdir explicitly:

```console
cg extension install https://github.com/mnemcik/cg-extensions --path 1password
```

`--path` applies only to direct repo-url installs; a registry name carries its
own `path`. The subdir is validated (relative, no `..` escape) before use.

**Isolation.** Co-located extensions share the repo's mirror and, at the same
commit, one tree; each is read from `<tree>/<path>`. They still install, remove
and update independently, because each `.cg.json` entry records its own commit
and removing one never deletes a tree. The subdir is recorded in `.cg.json`
(`path`) so update and fresh-clone re-install know where each manifest lives.

**Versioning.** A single-repo extension's git tags are its releases, so `update`
moves to the latest tag (or the default branch when untagged). A whole-repo tag
can't map to a single co-located extension's version, so a subdir extension's
`update` instead tracks the repo's **default branch** and takes the version from
the manifest's own `version` field. Bump the manifest `version` in the subdir to
publish a new version of a co-located extension.

## Reuse of existing machinery

The extension system deliberately reuses what `cg sync` already ships, rather
than reinventing it:

- **Section insert/replace/remove** — `internal/manifest` already implements
  `ParseSections` / `ReplaceSection` / `AppendSection` against `cg:section`
  markers. M4 generalizes these to a parameterized marker namespace so the same
  code drives `ext:<name>:section`. The hash helper (`HashContent`) and note
  walker (`NotesFromFS`) carry over for copy + change-detection.
- **Hook wrapper + settings merge** — the `cg init` hook-wiring logic (additive
  `settings.json` merge, wrapper install) is the template for the `hooks`
  contribution point.
- **Anonymous GitHub discovery** — `internal/autoupdate`'s anonymous release
  discovery is the pattern for fetching `index.json` and resolving latest tags.

## Scope boundaries (v1)

Explicitly **out** for v1 (documented here so they aren't silently assumed):

- Cross-extension dependency resolution — manifest declares zero deps.
- Signing / verification of extensions — v1 trusts the user; the registry is
  curated by PR. Noted in `SECURITY.md`.
- `cg extension update --auto` background updates — design only, deferred.
- Marketplace web UI — the JSON registry is the catalogue.

## Open questions deferred to implementation

- How to prune store trees and mirrors no workspace uses any more. Trees are
  shared and `remove` keeps them, so the store only grows (each tree is small:
  a few hundred KB for the `cg-extensions` monorepo).
- Conflict handling if two extensions declare the same `ext:<name>:section` id
  — names are registry-unique so the namespace can't actually collide; revisit
  only if direct-install bypasses uniqueness.
