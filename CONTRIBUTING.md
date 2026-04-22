# Contributing

Thank you for considering contributing to Consigliere.

## Development

### Prerequisites

- Go 1.25+ (`go version`) — matches `go.mod`
- golangci-lint (`brew install golangci-lint` or see [install docs](https://golangci-lint.run/welcome/install/))

### Setup

```bash
git clone https://github.com/mnemcik/consigliere.git
cd consigliere
make build
```

### Common tasks

```bash
make help       # Show all available targets
make build      # Build the binary
make test       # Run tests with race detector
make lint       # Run linters
make check      # Run everything (fmt, tidy, lint, test)
make clean      # Remove build artifacts
```

### Project structure

```
cmd/                    # CLI commands (cobra)
  embed_templates/      # Templates embedded into the binary
internal/
  workspace/            # Workspace detection and config
templates/              # Source templates (copied to embed_templates)
skills/                 # Claude Code skill wrappers
```

### Adding a new command

1. Create `cmd/<name>.go` with a cobra command
2. Add tests in `cmd/<name>_test.go`
3. Register in the `init()` function: `rootCmd.AddCommand(<name>Cmd)`
4. Update README.md

### Updating templates

Templates live in two places:
- `templates/` — the source of truth (human-editable)
- `cmd/embed_templates/` — copy used by Go's `embed` (must stay in sync)

After editing a template in `templates/`, copy it to `cmd/embed_templates/`:
```bash
cp templates/idea.md cmd/embed_templates/idea.md
```

## Submitting changes

- Open a pull request against `main`.
- PR titles follow [Conventional Commits](https://www.conventionalcommits.org/) (see below) — they become the squash-merge commit subject.
- Run `make check` locally before pushing.
- CI (lint + test + cross-platform build) must be green.
- No DCO sign-off is required.

## Release process

1. Update `CHANGELOG.md`
2. Commit: `git commit -m "release: vX.Y.Z"`
3. Tag: `git tag vX.Y.Z`
4. Push: `git push origin main --tags`
5. GitHub Actions runs GoReleaser automatically, creating the release with cross-platform binaries

## Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — new feature
- `fix:` — bug fix
- `docs:` — documentation
- `chore:` — maintenance
- `ci:` — CI/CD changes
- `test:` — test changes
- `refactor:` — code restructuring
- `release:` — version release
