# CLAUDE.md

This file provides guidance to Claude Code when working on the Consigliere (cg) project.

## What This Repo Is

A Go CLI tool + Claude Code plugin for personal workspace management. The `cg` binary is a self-contained executable with all templates embedded via `go:embed`.

## Repository Structure

```
main.go                           # Entry point
cmd/                              # CLI commands (cobra)
  root.go                         # Root command
  init.go                         # cg init
  match.go                        # cg match
  status.go                       # cg status
  version.go                      # cg version
  *_test.go                       # Tests
internal/
  workspace/                      # Workspace detection (.cg.json)
templates/                        # Framework content, embedded via templates/embed.go (the only copy)
.github/workflows/                # CI + Release automation
.golangci.yml                     # Linter config
.goreleaser.yml                   # Release config
Makefile                          # Build targets
```

## Development

```bash
make help       # Show all targets
make build      # Build binary
make test       # Run tests with race detector
make lint       # Run golangci-lint
make check      # Run everything
```

## Key Conventions

- Framework content lives only in `templates/`, embedded by `templates/embed.go`. A new top-level entry there must be added to its `//go:embed` pattern list.
- Version is injected at build time via `-ldflags` — see Makefile.
- `.cg.json` type field must be `"consigliere"` for workspace detection.
- Sentinel comments in `templates/workspace/CLAUDE.md` use `cg:section` / `user:section` prefixes.
- Follow [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.

## Release Process

Automated by release-please; see the Release process section in [CONTRIBUTING.md](CONTRIBUTING.md). Do not hand-edit `CHANGELOG.md`, bump a version or push tags.
