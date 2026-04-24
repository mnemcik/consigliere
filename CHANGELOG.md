# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `cg init --wizard` / `-i` — interactive setup walkthrough built on `charmbracelet/huh`. Collects name/role/responsibilities (written into `PROFILE.md`), an optional first area (slug, name, tags, overview — written to `areas/<slug>.md` and linked from `areas/INDEX.md`), and confirms `git init` + slash-command install. TTY-only; errors cleanly when stdin isn't a terminal. Non-interactive `cg init` behavior is unchanged.
- `cg --version` / `cg -v` root flags — identical output to `cg version` (`cg version <semver>`). The existing `cg version` subcommand is unchanged.
- **Pull Request Review Loop** rule shipped in the embedded workspace `CLAUDE.md` (new section `pr-review-loop`, delimited by `<!-- cg:section:start=pr-review-loop -->` / `<!-- cg:section:end=pr-review-loop -->`). Defines the post-`gh pr create` autoloop: fetch CI + inline + issue + review comments; for each comment either fix in a follow-up commit, reply with reasoning, or escalate to the user; re-run the fetch after each push; exit when all CI is green and every comment has been addressed. Explicitly prohibits silent resolution, amending review-branch commits, and dismissing bot reviews by default. Covers both human and bot reviewers (CodeRabbit, Copilot, dependabot).

### Changed

- **Area taxonomy: free-form tags replace category enum.** Area files now carry a `Tags:` field (comma-separated, multi-valued, normalized to lowercase) instead of a binary `Category: Service/System | Practice/Platform` choice. `areas/INDEX.md` is now a single flat table with a Tags column instead of two category-split tables. Rationale: the category split forced blurry areas into arbitrary buckets, read enterprise-flavored (poor fit for personal-project workspaces), and was inconsistent with how ideas already use free-form tags. The wizard's category-select step is replaced by a free-text tag input. The `cg:version` marker in the embedded workspace CLAUDE.md stays at `1.0.0` — existing workspaces need a manual migration (add `Tags:` lines to area frontmatter; flatten `areas/INDEX.md`) since there is no automated migrator in this release.

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
