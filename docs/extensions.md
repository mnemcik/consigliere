# `cg` extension system — manifest, registry, and `cg extension`

This document is the authoritative design for the Consigliere extension system:
the `cg-extension.json` manifest, the central registry, the `cg extension`
subcommand suite, the contribution points an extension can declare, and the
external-subcommand discovery mechanism.

It is the framework-side counterpart to the author-facing
[`EXTENSIONS.md`](../EXTENSIONS.md) guide. When the two disagree, this file is
the spec and `EXTENSIONS.md` is the tutorial; fix the tutorial.

## Why extensions exist

Consigliere is **domain-agnostic** framework scope: generic conventions
(session-start gate, DRY, information propagation, worktree mechanics) ship in
the framework; workspace- or domain-specific behaviour does not. But users still
want to opt into domain rules — 1Password as the credential store, VPaaS backlog
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
  the catalogue can grow without a binary release.
- **Extension repo**: holds `cg-extension.json` plus the contribution payloads.
- **Clone**: `cg extension install` clones the repo to
  `~/.config/consigliere/extensions/<name>/` (machine-shared source of truth).
- **Workspace**: contributions are *applied* per-workspace and tracked in a
  per-workspace ledger (`.cg/ext/<name>.json`) + `.cg.json` `extensions[]`.

## Install layout (DEC-002)

The cloned extension source is **per machine**, XDG-compliant:

```
~/.config/consigliere/extensions/<name>/      # the cloned extension repo (shared across this machine's workspaces)
  cg-extension.json
  fragments/<id>.md                  # CLAUDE.md section bodies
  notes/<file>.md                    # notes to copy into the workspace
  hooks/<file>.sh                    # hook wrappers (DEC-004 style: exec cg-<name> …)
  templates/<path>                   # templates to copy into the workspace
  bin/cg-<name>                      # optional external subcommand binary
```

`$XDG_CONFIG_HOME` is honoured when set; otherwise `~/.config`. This is the
**same `~/.config/consigliere` root** that `cg`'s auto-update state already uses
(`internal/autoupdate.StateDir`) — extensions deliberately do not introduce a
second config tree (project DEC-002, 2026-06-14).

What was *applied to a given workspace* is recorded **in that workspace**, not in
the shared clone, because one clone may back several workspaces:

```
<workspace>/.cg/ext/<name>.json      # install ledger: exactly what this workspace got
<workspace>/.cg.json                 # extensions[] entry (re-install on cg init)
```

This refines the project todo's original `~/.config/consigliere/extensions/<name>/.install-log.json`
placement: the ledger must be per-workspace so `cg extension remove` reverses the
right workspace, and so two workspaces sharing one clone don't fight over one
log. See DEC-002.

## Manifest schema v1 (`cg-extension.json`)

Lives at the root of every extension repo. v1 is intentionally minimal; the
`"manifest"` integer versions the schema itself for forward compatibility. v1
forbids cross-extension dependencies (declare none).

```json
{
  "manifest": 1,
  "name": "1password",
  "version": "0.1.0",
  "description": "1Password as authoritative credential store",
  "contributes": {
    "claude-md-sections": [
      { "id": "credentials-1password", "path": "fragments/credentials.md" }
    ],
    "notes": [
      { "src": "notes/credentials-policy.md", "dest": "notes/credentials-policy.md" }
    ],
    "hooks": [
      { "event": "SessionStart", "wrapper": "hooks/credentials-gate.sh", "command": "cg-1password gate" }
    ],
    "subcommands": [
      { "namespace": "secret", "binary": "cg-1password" }
    ],
    "templates": [
      { "src": "templates/credential-request.md", "dest": "templates/credential-request.md" }
    ]
  }
}
```

### Field reference

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `manifest` | int | yes | Manifest **schema** version. v1 = `1`. `cg` refuses a manifest whose `manifest` it doesn't understand. |
| `name` | string | yes | Short, lowercase, `[a-z0-9-]+`. The install name, the `.cg.json` key, the clone directory, the `ext:<name>:section` namespace, and the `cg-<name>` binary stem. Must be unique in the registry. |
| `version` | string | yes | Extension semver. Recorded in `.cg.json`; compared on `cg extension update`. |
| `description` | string | yes | One line, shown in `cg extension list` and the registry. |
| `contributes` | object | yes | The contribution points (all five keys below). Each is an array; an empty/absent array means "contributes nothing of this type". |

### Contribution points (all five supported at launch — project decision)

1. **`claude-md-sections`** — `[{ id, path }]`. Inserts the body of `path` into
   the workspace `CLAUDE.md` as an extension-owned section, delimited by
   `<!-- ext:<name>:section:start=<id> -->` / `<!-- ext:<name>:section:end=<id> -->`.
   This namespace is distinct from framework `cg:section` and user `user:section`
   blocks, so `cg sync` (which parses only `cg:section`) never touches extension
   fragments — and an extension can never clobber a framework section.
   Re-install/update replaces the section body in place; remove deletes the block.

2. **`notes`** — `[{ src, dest }]`. Copies `src` (relative to the extension repo)
   to `dest` (workspace-relative, conventionally under `notes/`). If a
   `notes/INDEX.md` exists, a pointer row is added under an
   `<!-- ext:<name> -->`-marked region so removal is exact.

3. **`hooks`** — `[{ event, wrapper, command }]`. Installs the bash wrapper at
   `wrapper` into `.claude/hooks/`, registers it for `event` in
   `.claude/settings.json` (additive merge — never clobbers existing hooks or
   user padding), and the wrapper `exec`s `command` (a `cg-<name>` subcommand).
   Mirrors framework hook style (DEC-004): user-readable, degrades to a no-op
   when the binary is absent.

4. **`subcommands`** — `[{ namespace, binary }]`. Declares that the extension
   ships an external binary `bin/<binary>` exposed as `cg <namespace> …` via the
   external-subcommand resolver (below). Install symlinks/records the binary;
   it is **not** copied into the workspace.

5. **`templates`** — `[{ src, dest }]`. Copies template files into the
   workspace `templates/` tree (same copy semantics as notes, without the
   INDEX pointer).

## `cg extension` subcommand suite

| Command | Behaviour |
|---------|-----------|
| `cg extension install <name>` | Resolve `<name>` in the registry → clone `repo` to `~/.config/consigliere/extensions/<name>/` (skip if present) → read manifest → apply all contributions to the current workspace → write ledger + `.cg.json` entry. |
| `cg extension install <repo-url>` | Same, but skip the registry: clone the URL directly, read its manifest, take `name` from the manifest. Source recorded as `direct`. |
| `cg extension install … --ref <tag\|branch>` | Pin the clone to a ref. Default: latest tag, else the default branch. |
| `cg extension install <repo-url> --path <subdir>` | Install a co-located extension whose manifest lives in `<subdir>` of a monorepo. Direct installs only; registry names carry their own `path`. See [Co-located extensions](#co-located-extensions-monorepo). |
| `cg extension list [--json]` | List installed extensions for the current workspace: name, version, source, installed-at. |
| `cg extension remove <name> [--purge]` | Reverse every contribution recorded in the workspace ledger (delete the `ext:<name>:section` block, copied notes/templates, hook wrapper + settings entry, INDEX rows), drop the `.cg.json` entry, delete the ledger. `--purge` also deletes the shared clone. |
| `cg extension update [<name>]` | Advance a **single-repo** extension's clone to the latest tag (or its default branch when untagged), then re-apply contributions (replace-in-place). A **co-located** (subdir) extension instead tracks the default branch and versions from its manifest — see [Co-located extensions](#co-located-extensions-monorepo). No `<name>` updates all installed extensions. |

### Re-install on fresh clone

`.cg.json` records each installed extension. `cg init` (and a future
`cg extension sync`) re-runs `cg extension install` for every `extensions[]`
entry, so cloning a workspace to a new machine restores its extensions.

## External-subcommand discovery

Modelled on git's `git foo` → `git-foo` pattern. When `cg <verb> …` is not a
known built-in subcommand, the resolver looks for an executable named
`cg-<verb>`:

1. in each installed extension's `bin/` (per the workspace's `.cg.json`), then
2. on `$PATH`.

If found, `cg` `exec`s it with the remaining arguments and forwards the exit
code. If not found, the normal "unknown command" cobra error is shown. Built-in
subcommands always win over external ones (no shadowing the framework).

## `.cg.json` v1.2 additions

Additive and optional; absence = no extensions. Existing fields unchanged.

```jsonc
{
  // … existing type / version / indexes / v1.1 blocks …
  "extensions": [
    {
      "name": "1password",
      "version": "0.1.0",
      "source": "registry",                       // "registry" | "direct"
      "repo": "https://github.com/mnemcik/cg-extensions",
      "path": "1password",                        // optional: subdir for a co-located extension; absent = repo root
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
  "subcommands": [{ "namespace": "secret", "binary": "cg-1password" }],
  "indexRows": [{ "file": "notes/INDEX.md", "marker": "ext:1password" }]
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

`cg extension install <name>` fetches `index.json` (anonymous HTTPS, like the
auto-update GitHub discovery), finds the entry, and clones `repo`.

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

**Isolation.** Each extension still gets its own name-keyed clone under
`~/.config/consigliere/extensions/<name>/` (the whole repo is cloned, the
manifest and payloads are read from `<clone>/<path>`). Co-located siblings
therefore install, remove, and update independently — `remove --purge` of one
never touches another's clone. The subdir is recorded in `.cg.json` (`path`) so
update and fresh-clone re-install know where each manifest lives.

**Versioning.** A single-repo extension's git tags are its releases, so `update`
checks out the latest tag (or the default branch when untagged). A whole-repo tag
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

- Whether `remove` of a clone shared by multiple workspaces should refuse
  without `--purge` when other workspaces still reference it (v1: `remove`
  leaves the clone, `--purge` deletes it unconditionally).
- Conflict handling if two extensions declare the same `ext:<name>:section` id
  — names are registry-unique so the namespace can't actually collide; revisit
  only if direct-install bypasses uniqueness.
