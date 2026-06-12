# Binary Auto-Update

How the `cg` **binary** keeps itself current — manual `cg update` commands, a
detached background freshness check, and a warn-only gate for major (breaking)
releases.

> **Status:** shipped. Manual `cg update check` / `upgrade`, the background
> worker, and the major-version gate are all in place.

## Two kinds of "update" — don't confuse them

| Concern | Command | What it changes | Owner |
|---|---|---|---|
| Replace the `cg` **binary** with a newer release | `cg update` | the executable on `$PATH` | this subsystem |
| Reconcile a workspace's **content** with the framework | `cg sync` | `CLAUDE.md` sections + framework notes in the workspace | [workspace sync](workspace-sync.md) |

They are independent. `cg update` swaps the binary; `cg sync` reconciles the
files in a workspace. The verbs are kept disjoint on purpose.

## Commands

| Command | What it does |
|---|---|
| `cg update check` | Report the current vs latest released version and whether an upgrade is available. Read-only. |
| `cg update upgrade` | Download, verify, and install the latest release in place (explicit consent — installs majors too). |
| `cg update snooze --major` | Silence the pending major-release notice for 7 days. |
| `cg update ignore --major` | Permanently dismiss the pending major (re-arms only when a *newer* major appears). |

Discovery uses the **anonymous** GitHub Releases API — no `gh` and no token,
because the repo is public (mirroring `install.sh`). Development builds (`dev`,
a dirty `git describe`) skip update behavior entirely.

## How the upgrade works

1. Resolve the latest release tag from `api.github.com`.
2. Download the platform archive (`consigliere_<version>_<os>_<arch>.tar.gz`, or
   `.zip` on Windows) and `checksums.txt` from the release.
3. Verify the archive's SHA-256 against `checksums.txt` — the same artifact
   `install.sh` trusts.
4. Extract the `cg` binary and atomically replace the running executable via
   [`minio/selfupdate`](https://github.com/minio/selfupdate), which rolls back
   on any failure. A botched download never leaves a broken binary.

### Externally-managed installs are never self-replaced

If `cg` was **not** installed by `install.sh`, it will not overwrite itself —
doing so would corrupt a package manager's bookkeeping. Provenance is decided by:

1. the running executable's path is under a Homebrew prefix
   (`/opt/homebrew`, `/usr/local`, `/home/linuxbrew/.linuxbrew` — covering both
   `Cellar/` and `Caskroom/`) → **Homebrew**, or
2. `installed.json`'s `method` field is `"install.sh"` → **self-managed**, else
3. anything else → **unknown** (e.g. `go install`, a manual copy).

Only `install.sh`-managed binaries self-replace. Homebrew installs are pointed
at `brew upgrade --cask cg`; unknown installs at the `install.sh` one-liner.

## Background check

On each meaningful `cg` run, a **detached** background worker is spawned (its own
session, stdio closed) and the foreground command returns immediately. The worker:

- runs at most once per **24h** (a debounce stamp), guarded by a pidfile lock
  with PID-liveness + stale-takeover so two workers never race;
- for `install.sh`-managed installs, **auto-installs minor/patch** releases and
  writes a one-shot marker so the next `cg` run prints `✅ cg updated to vX`;
- for a **major** release, writes a warn-only marker instead of installing (see
  below);
- never installs for Homebrew/unknown installs;
- swallows every error to `autoupdate.log` — it must never block or break the
  user's actual command.

The background check is skipped for the `version`, `help`, and `update` commands,
for development builds, and when opted out.

## Major-version gate

Breaking (major) releases are never auto-installed. When the worker sees one it
records `major-available.json`, and every subsequent `cg` run prints a persistent,
warn-only two-line notice (after the command's own output) pointing at the
changelog. Resolve it with:

- `cg update upgrade` — install it now (explicit consent), or
- `cg update snooze --major` — silence for 7 days, or
- `cg update ignore --major` — dismiss permanently (re-arms on a newer major).

The marker auto-clears once the installed version reaches or passes the flagged
major (whether via upgrade or a later release).

## Opt-outs

| Mechanism | Effect |
|---|---|
| `--no-auto-update` (flag) | Skip the background check for this run. |
| `CONSIGLIERE_AUTO_UPDATE=0` | Disable the background worker entirely. |
| `CONSIGLIERE_NO_UPDATE_NOTICE=1` | Silence the updated + major notices. |

### Advanced overrides (forks / testing)

| Variable | Default | Purpose |
|---|---|---|
| `CONSIGLIERE_AUTO_UPDATE_REPO` | `mnemcik/consigliere` | Target `owner/repo` for releases. |
| `CONSIGLIERE_GITHUB_API_BASE` | `https://api.github.com` | REST API base (GitHub Enterprise / tests). |
| `CONSIGLIERE_GITHUB_DOWNLOAD_BASE` | `https://github.com` | Release-asset host (tests). |

## State files

All under `${XDG_CONFIG_HOME:-$HOME/.config}/consigliere/`:

| File | Written by | Purpose |
|---|---|---|
| `installed.json` | `install.sh`; refreshed after a self-update | Install method, version, tag, path. The provenance signal for self-replace. |
| `last-update-check` | worker | Unix-millis debounce stamp (24h window). |
| `autoupdate.lock` | worker | Pidfile guarding against concurrent workers. |
| `updated.json` | worker | One-shot "updated to vX" marker; printed + cleared on the next run. |
| `major-available.json` | worker | Pending major-release marker (+ optional `snoozeUntil`). |
| `major-ignored.json` | `cg update ignore --major` | Major versions permanently dismissed. |
| `autoupdate.log` | worker | Append-only best-effort error log for background failures. |
