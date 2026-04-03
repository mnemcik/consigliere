# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

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
