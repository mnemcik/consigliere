# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **`cg sync` — workspace content reconciliation (report-only).** New command that classifies every framework-managed artifact (CLAUDE.md `cg:section` blocks and framework notes) by comparing three content hashes: what's on disk, what the manifest recorded `cg` last wrote, and what the current binary ships. Each artifact is labelled **up-to-date**, **updatable** (untouched by the user, framework changed — safe to auto-apply), **drifted** (user-edited — never clobbered), **new**, **removed**, or **missing**, and printed as a grouped report. This release is a **dry run**: `cg sync` never writes — apply lands in a later release (the `/cg-sync` skill will drive the semantic-conflict decisions hashing can't). The deterministic classifier lives in a new pure, fully-unit-tested `internal/sync` package (workspace-sync DEC-004/DEC-005).
- **Workspace-sync manifest foundation.** `cg init` now writes `.cg/manifest.json` recording every framework-managed `CLAUDE.md` section (by sentinel id) with a SHA-256 content hash, plus the framework version. This is durable, committed workspace state (not gitignored) that a future `cg sync` will use to upgrade workspace *content* in place — auto-updating untouched framework sections, never clobbering ones the user edited. A new internal `manifest` package parses `cg:section` blocks (ignoring user-owned `user:section` blocks) and reads/writes the manifest. The two-zone ownership contract and the `cg sync` (content) vs `cg update` (binary) boundary are documented in `docs/workspace-sync.md`. Existing workspaces are unaffected until re-initialized or synced; `notes` tracking is seeded empty and populated by the load-on-demand work.
- **Workspace-sync manifest now tracks framework notes.** `cg init` copies every framework note shipped under the embed tree's `notes/` directory into the workspace's `notes/` (skip-if-exists, like `CLAUDE.md`) and registers each in `.cg/manifest.json` under `notes` with a SHA-256 content hash, keyed by portable forward-slash workspace-relative path. The copied set mirrors the registered set exactly, so every manifest note record has a backing file on disk. A new `manifest.NotesFromFS` walks a notes tree and hashes each file, skipping directories and dotfiles — so the `.gitkeep` that keeps the otherwise-empty framework-notes directory present in the embed tree is never registered or copied. No user-facing change yet: the framework ships no notes today, so `notes` stays `{}` until the load-on-demand work populates the embed tree, at which point the records light up with no further `cg init` changes.
- `cg init --wizard` / `-i` — interactive setup walkthrough built on `charmbracelet/huh`. Collects name/role/responsibilities (written into `PROFILE.md`), an optional first area (slug, name, tags, overview — written to `areas/<slug>.md` and linked from `areas/INDEX.md`), and confirms `git init` + slash-command install. TTY-only; errors cleanly when stdin isn't a terminal. Non-interactive `cg init` behavior is unchanged.
- `cg --version` / `cg -v` root flags — identical output to `cg version` (`cg version <semver>`). The existing `cg version` subcommand is unchanged.
- **Pull Request Review Loop** rule shipped in the embedded workspace `CLAUDE.md` (new section `pr-review-loop`, delimited by `<!-- cg:section:start=pr-review-loop -->` / `<!-- cg:section:end=pr-review-loop -->`). Defines the post-`gh pr create` autoloop: fetch CI + inline + issue + review comments; for each comment either fix in a follow-up commit, reply with reasoning, or escalate to the user; re-run the fetch after each push; exit when all CI is green and every comment has been addressed. Explicitly prohibits silent resolution, amending review-branch commits, and dismissing bot reviews by default. Covers both human and bot reviewers (CodeRabbit, Copilot, dependabot).

### Changed

- **Workspace CLAUDE.md template — two-zone ownership split finalized.** The embedded workspace `CLAUDE.md` now has zero orphan content between zones (workspace-sync DEC-002): the framework intro paragraph is wrapped in a new `cg:section:start=intro` block so `cg sync` keeps the contract description current, and the dead `<!-- cg:version=1.0.0 -->` marker was removed — no Go code read it, and the framework version is now owned authoritatively by `.cg/manifest.json` (`frameworkVersion`) and `.cg.json` (workspace-sync DEC-003). A new test (`TestEmbeddedCLAUDEHasNoOrphanContent`) enforces the contract: every non-blank line outside the H1 title must sit inside a `cg:section` or `user:section` block, with no nested or unclosed zones.
- **Area taxonomy: free-form tags replace category enum.** Area files now carry a `Tags:` field (comma-separated, multi-valued, normalized to lowercase) instead of a binary `Category: Service/System | Practice/Platform` choice. `areas/INDEX.md` is now a single flat table with a Tags column instead of two category-split tables. Rationale: the category split forced blurry areas into arbitrary buckets, read enterprise-flavored (poor fit for personal-project workspaces), and was inconsistent with how ideas already use free-form tags. The wizard's category-select step is replaced by a free-text tag input. Existing workspaces need a manual migration (add `Tags:` lines to area frontmatter; flatten `areas/INDEX.md`) since there is no automated migrator in this release.

## [1.0.1] - 2026-04-23

Distribution-only release — no Go code changes vs `v1.0.0`. Ships the public-release hygiene + install infrastructure.

### Added

- `SECURITY.md` — vulnerability disclosure via GitHub Security Advisories
- `CODE_OF_CONDUCT.md` — Contributor Covenant 2.1
- `.github/dependabot.yml` — weekly updates for `gomod` and `github-actions`
- `.github/ISSUE_TEMPLATE/config.yml` — disables blank issues, surfaces the security contact
- README maturity + project docs sections
- `install.sh` — one-liner installer for Linux/macOS: platform detection, checksum verification, state file at `${XDG_CONFIG_HOME:-$HOME/.config}/consigliere/installed.json`, supports `--tag`, `--dir`, `--force`, `CG_INSTALL_TAG`, `CG_INSTALL_DIR`
- Homebrew tap (`mnemcik/homebrew-tap`) — `brew install mnemcik/tap/cg`. GoReleaser publishes a Homebrew Cask on each release (removes macOS quarantine xattr on install). `skip_upload: auto` keeps the tap pointer stable across prerelease tags.

### Changed

- `CONTRIBUTING.md` — Go toolchain requirement aligned with `go.mod` (1.25+); added PR submission guidance
- README — Claude Code slash-command section rewritten to reflect the actual `cg init` install path (the `.claude-plugin/` marketplace approach was abandoned in v1.0); install section re-ordered around Homebrew + `install.sh` as the recommended paths
- Issue and PR templates — clarified requested environment info and linked to `CONTRIBUTING.md`

## [1.0.0] - 2026-04-03

### Added

- `cg init` command — bootstrap a Consigliere workspace with embedded templates
- `cg match` command — deterministic keyword-based project matching
- `cg status` command — workspace overview (project/area/idea/note counts)
- `cg version` command — print installed version
- Full template set: projects, ideas, notes, insights, areas, subagent briefings
- Framework CLAUDE.md with sentinel-delimited sections (`cg:section` / `user:section`)
- `.cg.json` workspace identity file
- Claude Code plugin skills (`/cg-init`, `/match-project`) as thin CLI wrappers
- CI pipeline with golangci-lint and tests
- GoReleaser configuration for cross-platform builds
