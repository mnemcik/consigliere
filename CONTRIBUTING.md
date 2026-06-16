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

Releases are automated by [release-please](https://github.com/googleapis/release-please).
**Do not** hand-edit `CHANGELOG.md`, bump a version, or push tags.

1. Merge PRs to `main` with Conventional-Commits titles (`feat:`, `fix:`, etc.). The
   PR title is validated by the **PR Title Lint** check and — because we squash-merge —
   becomes the single commit on `main` that release-please reads.
2. release-please keeps an open **"chore: release X.Y.Z"** Release PR that accumulates
   the `CHANGELOG.md` section, the version, and the `templates/workspace/.cg.json`
   version bump derived from the merged commits.
3. **To ship, merge the Release PR.** release-please then tags the repo and creates the
   GitHub Release; that release event triggers GoReleaser, which builds the
   cross-platform binaries, checksums, Homebrew cask, and `install.sh` artifacts.

Bump rules: `feat:` → minor, `fix:` → patch, `feat!:`/`fix!:` or a `BREAKING CHANGE:`
footer → major.

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
