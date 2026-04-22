# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `SECURITY.md` — vulnerability disclosure via GitHub Security Advisories
- `CODE_OF_CONDUCT.md` — Contributor Covenant 2.1
- `.github/dependabot.yml` — weekly updates for `gomod` and `github-actions`
- `.github/ISSUE_TEMPLATE/config.yml` — disables blank issues, surfaces the security contact
- README maturity + project docs sections
- `install.sh` — one-liner installer for Linux/macOS: platform detection, checksum verification, state file at `${XDG_CONFIG_HOME:-$HOME/.config}/consigliere/installed.json`, supports `--tag`, `--dir`, `--force`, `CG_INSTALL_TAG`, `CG_INSTALL_DIR`

### Changed

- `CONTRIBUTING.md` — Go toolchain requirement aligned with `go.mod` (1.25+); added PR submission guidance
- README — Claude Code slash-command section rewritten to reflect the actual `cg init` install path (the `.claude-plugin/` marketplace approach was abandoned in v1.0)
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
