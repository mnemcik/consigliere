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
  embed_templates/                # Templates embedded into binary (go:embed)
internal/
  workspace/                      # Workspace detection (.cg.json)
templates/                        # Source templates (human-editable)
skills/                           # Claude Code skill wrappers
.claude-plugin/plugin.json        # Claude Code plugin manifest
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

- Templates live in `templates/` (source of truth) and `cmd/embed_templates/` (embedded copy). Keep them in sync.
- Version is injected at build time via `-ldflags` — see Makefile.
- `.cg.json` type field must be `"consigliere"` for workspace detection.
- Sentinel comments in `templates/workspace/CLAUDE.md` use `cg:section` / `user:section` prefixes.
- Follow [Conventional Commits](https://www.conventionalcommits.org/) for commit messages.

## Release Process

1. Update `CHANGELOG.md`
2. Bump version in `templates/workspace/.cg.json` (the version `cg init` stamps into new workspaces)
3. Commit: `git commit -m "release: vX.Y.Z"`
4. Tag: `git tag vX.Y.Z`
5. Push: `git push origin main --tags`
6. GitHub Actions runs GoReleaser, creating the release with cross-platform binaries automatically.
