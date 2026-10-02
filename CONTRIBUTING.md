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

make e2e-autoupdate   # End-to-end test of the auto-update subsystem (needs python3)
```

`make e2e-autoupdate` exercises the real `cg update` / background-worker / major-gate
code paths against a throwaway local server standing in for GitHub Releases — no network
and no release required. See [`docs/auto-update.md`](docs/auto-update.md) for the design.

### Project structure

```
cmd/                    # CLI commands (cobra)
internal/
  workspace/            # Workspace detection and config
templates/              # Framework content embedded into the binary (package templates)
```

### Adding a new command

1. Create `cmd/<name>.go` with a cobra command
2. Add tests in `cmd/<name>_test.go`
3. Register in the `init()` function: `rootCmd.AddCommand(<name>Cmd)`
4. Update README.md

### Updating templates

Everything `cg init` installs lives in `templates/`: item templates, framework notes, the workspace `CLAUDE.md`, slash commands, skills and hooks. It is the only copy. `templates/embed.go` embeds it into the binary, so an edit there ships with the next build and there is nothing to sync.

A new top-level file or directory must be added to the `//go:embed` pattern list in `templates/embed.go`.

After changing a slash command, skill, hook wrapper or the status line, run `go generate ./cmd`. It regenerates `cmd/claude_file_history_gen.go`, the hashes `cg sync` uses to tell an unedited old copy in a workspace from an edited one. `TestClaudeFileHistoryCoversEmbed` fails until you do. The generator reads the release tags, so fetch them first (`git fetch --tags`).

## Submitting changes

- Open a pull request against `main`.
- PR titles follow [Conventional Commits](https://www.conventionalcommits.org/) (see below) — they become the squash-merge commit subject.
- Run `make check` locally before pushing.
- CI (lint + test + cross-platform build) must be green.
- No DCO sign-off is required.

## Release process

Releases are automated by [release-please](https://github.com/googleapis/release-please).
**Do not** hand-edit `CHANGELOG.md`, bump a version, or push tags.

1. Merge PRs to `main` with Conventional-Commits titles (`feat:`, `fix:`, etc.). The
   PR title is validated by the **PR Title Lint** check and — because we squash-merge —
   becomes the single commit on `main` that release-please reads.
2. release-please keeps an open **"chore: release X.Y.Z"** Release PR that accumulates
   the `CHANGELOG.md` section and the version bump derived from the merged commits.
   The version `cg init` writes into `.cg.json` comes from the binary, not a file.
3. **To ship, merge the Release PR.** release-please then tags the repo and creates the
   GitHub Release; that release event triggers GoReleaser, which builds the
   cross-platform binaries, checksums, Homebrew cask, and `install.sh` artifacts.

Bump rules: `feat:` → minor, `fix:` → patch, and any `type!:` commit or a
`BREAKING CHANGE:` footer → major.

Manual hotfix escape hatch (automation down): create the release directly with
`gh release create vX.Y.Z --notes "…"` — this fires GoReleaser. A bare `git push --tags`
will not (the Release workflow triggers on the release event, not the tag).

## Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — new feature
- `fix:` — bug fix
- `docs:` — documentation
- `chore:` — maintenance
- `ci:` — CI/CD changes
- `test:` — test changes
- `refactor:` — code restructuring

Release commits are produced automatically by release-please — you don't author them.
