# Authoring a Consigliere extension

This guide shows how to write an extension for the `cg` framework. It is the
tutorial; the authoritative spec is [`docs/extensions.md`](docs/extensions.md).

An **extension** is an independent git repo that adds workspace- or
domain-specific behaviour to a Consigliere workspace — credential rules, backlog
standards, a digest workflow, a drafting persona — without forking the framework
or hand-editing `CLAUDE.md`. The framework ships the mechanism; your extension
ships the content.

## TL;DR

1. Create a repo with a `cg-extension.json` at its root.
2. Put your contribution payloads in `fragments/`, `notes/`, `hooks/`,
   `templates/`, and/or `bin/`.
3. Tag a release (`v0.1.0`).
4. Install it: `cg extension install https://github.com/you/cg-ext-foo`.
5. (Optional) PR your entry into `mnemcik/cg-extensions-registry` so users can
   `cg extension install foo` by name.

## Repository layout

```
cg-extension.json          # manifest (required, repo root)
fragments/                 # CLAUDE.md section bodies (claude-md-sections)
  <id>.md
notes/                     # notes copied into the workspace (notes)
  <file>.md
hooks/                     # bash hook wrappers (hooks)
  <file>.sh
templates/                 # templates copied into the workspace (templates)
  <path>
bin/                       # external subcommand binaries (subcommands)
  cg-<name>
README.md
LICENSE
```

You only need the directories your manifest actually references.

## The manifest

`cg-extension.json` (manifest **schema** version 1):

```json
{
  "manifest": 1,
  "name": "foo",
  "version": "0.1.0",
  "description": "One-line summary shown in cg extension list and the registry",
  "contributes": {
    "claude-md-sections": [
      { "id": "foo-rules", "path": "fragments/foo-rules.md" }
    ],
    "notes": [
      { "src": "notes/foo-workflow.md", "dest": "notes/foo-workflow.md" }
    ],
    "hooks": [
      { "event": "SessionStart", "wrapper": "hooks/foo-gate.sh", "command": "cg-foo gate" }
    ],
    "subcommands": [
      { "namespace": "foo", "binary": "cg-foo" }
    ],
    "templates": [
      { "src": "templates/foo.md", "dest": "templates/foo.md" }
    ]
  }
}
```

### Rules for `name`

`name` is load-bearing — it's the install name, the `.cg.json` key, the clone
directory, the `ext:<name>:section` marker namespace, and the `cg-<name>` binary
stem. It must be lowercase `[a-z0-9-]+` and unique in the registry. Pick it once;
changing it later is a breaking change for installed users.

### Every contribution key is optional

Declare only what your extension actually provides. A pure-rules extension might
only set `claude-md-sections` and `notes`; a tool extension might only set
`subcommands`.

## Contribution points

### `claude-md-sections` — add rules to CLAUDE.md

Each `{ id, path }` inserts the body of `path` into the user's `CLAUDE.md`,
wrapped in extension-owned sentinels:

```
<!-- ext:foo:section:start=foo-rules -->
…contents of fragments/foo-rules.md…
<!-- ext:foo:section:end=foo-rules -->
```

This namespace is separate from the framework's `cg:section` and the user's
`user:section` blocks, so `cg sync` never touches your section and you can never
clobber a framework rule. On `cg extension update` the body is replaced in
place; on `remove` the whole block is deleted.

**Keep fragments small and pointer-style.** The always-loaded `CLAUDE.md` costs
tokens on every session. Put the trigger + one-line headline in the fragment and
the detail in a note (see below) — the same load-on-demand discipline the
framework uses.

### `notes` — ship reference material

Each `{ src, dest }` copies a file from your repo into the workspace. By
convention `dest` lives under `notes/`. If the workspace has a `notes/INDEX.md`,
`cg` adds a pointer row inside an `ext:foo`-marked region so `remove` is exact.

### `hooks` — wire Claude Code hooks

Each `{ event, wrapper, command }` installs your bash `wrapper` into
`.claude/hooks/` and registers it for `event` (e.g. `SessionStart`,
`UserPromptSubmit`) in `.claude/settings.json`. The registration is an additive
merge — existing hooks and user customizations survive. Your wrapper should
`exec` a `cg-<name>` subcommand and degrade to a no-op when the binary is
missing, matching framework hook style:

```bash
#!/usr/bin/env bash
command -v cg-foo >/dev/null 2>&1 || exit 0
exec cg-foo gate "$@"
```

### `subcommands` — ship a `cg foo` command

Each `{ namespace, binary }` declares that `bin/<binary>` is exposed as
`cg <namespace> …`. When a user runs `cg foo bar`, `cg` finds `cg-foo` (in your
extension's `bin/` or on `$PATH`) and execs it with `bar`. Built-in `cg`
subcommands always take precedence — you cannot shadow the framework.

Build your binary for the platforms you support and commit it to `bin/`, or ship
it via your release assets (a future manifest version may fetch release
binaries; v1 expects `bin/`).

### `templates` — ship workspace templates

Each `{ src, dest }` copies a template into the workspace `templates/` tree.
Same copy semantics as notes, without the INDEX pointer.

## Versioning

Use semver for `version`. `cg extension update` pulls the latest tag (or `main`
if you publish no tags) and re-applies contributions. Treat a `name` change, a
removed contribution, or a renamed section `id` as a major bump.

(Co-located extensions version differently — see below.)

## Co-locating extensions in one repo (monorepo)

You can keep several extensions in one repo, each in its own subdirectory,
instead of a repo apiece — handy when you maintain a few related extensions:

```
cg-extensions/
  1password/
    cg-extension.json
    fragments/ notes/ …
  vpaas-backlog/
    cg-extension.json
    notes/ …
```

Each subdir is a complete extension (its own `cg-extension.json` + payloads).
Install one directly with `--path`:

```
cg extension install https://github.com/you/cg-extensions --path 1password
```

To make it installable by name, give its registry entry a `path` pointing at the
subdir (and a `manifestUrl` that includes the subdir):

```json
{
  "name": "1password",
  "description": "…",
  "repo": "https://github.com/you/cg-extensions",
  "path": "1password",
  "latestVersion": "0.1.0",
  "manifestUrl": "https://raw.githubusercontent.com/you/cg-extensions/main/1password/cg-extension.json"
}
```

Co-located extensions install, update, and remove independently of each other.
One caveat on **versioning**: a single-repo extension is updated to its latest
git tag, but a whole-repo tag can't identify one co-located extension's version,
so a subdir extension is updated by tracking the repo's default branch and
reading the manifest's own `version`. **Bump the `version` in the subdir's
`cg-extension.json`** to publish a new version of a co-located extension; don't
rely on a repo-wide tag.

## Publishing to the registry

Direct install works without the registry (root or subdir):

```
cg extension install https://github.com/you/cg-ext-foo
cg extension install https://github.com/you/cg-extensions --path foo
```

To let users install by name, open a PR against
[`mnemcik/cg-extensions-registry`](https://github.com/mnemcik/cg-extensions-registry)
adding your entry to `index.json`:

```json
{
  "name": "foo",
  "description": "…",
  "repo": "https://github.com/you/cg-ext-foo",
  "latestVersion": "0.1.0",
  "manifestUrl": "https://raw.githubusercontent.com/you/cg-ext-foo/main/cg-extension.json"
}
```

The registry is curated by PR. v1 does **not** sign or verify extensions —
installing an extension runs its contributions against your workspace, so only
install extensions you trust.

## Testing locally

Point `cg extension install` at a local clone (a `file://` URL or a path) and
verify each contribution applied, then `cg extension remove foo` and confirm the
workspace is clean. The framework ships an end-to-end fixture extension you can
use as a reference for the expected layout.
