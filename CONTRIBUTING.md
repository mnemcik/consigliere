# Contributing

Thank you for considering contributing to Consigliere.

## Development

### Prerequisites

- Go 1.25+ (`go version`) — matches `go.mod`
- golangci-lint at the version CI pins (the `version:` of the golangci-lint steps in `.github/workflows/ci.yml`). Another version can report different findings. Install that version with:

  ```bash
  v=$(sed -n 's/^ *version: \(v[0-9][0-9.]*\)$/\1/p' "$(git rev-parse --show-toplevel)/.github/workflows/ci.yml" | head -n 1)
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" "${v:?could not read the golangci-lint version from ci.yml}"
  golangci-lint --version
  ```

  This installs into `$(go env GOPATH)/bin`, which must be on your `PATH`. On Windows, run it in Git Bash. `brew install golangci-lint` installs the latest release, which may not match. See the [install docs](https://golangci-lint.run/welcome/install/) for other options.

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

## Supported platforms

cg supports **Linux, macOS and Windows**, and CI runs the tests on all three. A change isn't done until it works on each, and the Windows test check is required to merge. On Windows, cg's hooks and status line need Git for Windows; `cg init` and `cg sync` warn when Git Bash isn't found (`internal/gitbash`). Things that differ on Windows:

- **Paths.** Build disk paths with `filepath`. Paths written to files cg stores or compares (`.cg.json`, ledgers, manifests) use `/`; convert with `filepath.ToSlash` / `FromSlash` at the boundary. Don't assume a path starts with `/`: Windows paths have a drive letter.
- **Line endings.** Git on Windows checks files out with CRLF by default (`core.autocrlf`). Code that hashes, compares or parses text must not depend on the line ending (`manifest.HashContent` and `manifest.LF` normalise), and tests that compare file content must not either. A workspace's shell scripts are checked out with LF through cg's `.gitattributes` block ([workspace sync](docs/workspace-sync.md#line-endings)).
- **File modes.** Windows has no executable bit, and `chmod` only toggles read-only. Don't let behaviour or tests depend on mode bits there.
- **Open files.** A file that is open can't be deleted or renamed over on Windows. Close files before replacing or removing them, including in tests (`t.TempDir` cleanup fails on a file left open).
- **Locks and processes.** Platform code goes in `_unix.go` / `_windows.go` files behind build tags, as in `internal/session/lock_*.go`. Both sides need tests that run on their platform.
- **Shell.** Don't shell out to `sh`, `bash` or Unix tools from Go; use Go or `git`. The one exception is the status line's `upstream` command, which is a user's shell command by design (`runUpstream`). On Windows it runs with Git Bash (`gitbash.Find`) rather than the first `bash` on `PATH`, which can be WSL's; only when no Git Bash is found does it fall back to `PATH`. Hook wrappers are bash scripts that `settings.json` runs by path. Claude Code runs hooks in Git Bash on Windows when it is installed, and in PowerShell otherwise ([hooks reference](https://code.claude.com/docs/en/hooks)), so on Windows they work only with Git for Windows installed. The status line is a bash script too.

When a test can't apply on one platform, skip it there with a reason (`t.Skip("…on Windows: …")`) rather than leaving it to fail.

## Issues and priority

The open issues in this repository and in [cg-extensions](https://github.com/mnemcik/cg-extensions) are the backlog for cg and its extensions. Problems with the extension registry are filed in cg-extensions. Report a security problem privately through [Security Advisories](https://github.com/mnemcik/consigliere/security/advisories/new), not in a public issue.

### Type

Each issue gets a GitHub issue type:

- **Bug:** cg or an extension doesn't do what its documentation says, or the documentation is wrong.
- **Feature:** a new capability, or a change in behaviour.
- **Task:** work with no user-visible change, such as CI, refactoring, dependencies or missing documentation.

Documentation issues also carry the `documentation` label. In this repository the issue templates set the type and add `needs-triage`, which stays until the maintainer has triaged the issue. cg-extensions has no issue templates. In either repository, an issue with no type or no priority hasn't been triaged yet.

### Priority

Priority is set in the issue's **Priority** field: High, Medium or Low. The field's Urgent option is not used.

Priority is how badly an issue hurts multiplied by how many it hurts, and lower when there is a workaround. The effort to fix it doesn't count. "The usual setup" means a supported platform with the default configuration that `cg init` sets up.

- **High:**
  - a security problem;
  - data loss, or a workspace left broken;
  - upgrade, sync or session start fails in the usual setup, with no workaround;
  - the issue blocks another High issue.
- **Medium:**
  - a bug with a workaround;
  - a bug only on one supported platform or in a non-default setup;
  - a well-defined feature for the usual setup, meaning its scope and expected behaviour are clear;
  - documentation that is wrong or misleading.
- **Low:**
  - cosmetic issues and wording;
  - narrow edge cases, such as a rare combination of inputs or settings in any setup;
  - features that are still ideas, with scope or behaviour not yet settled;
  - missing documentation for something that works.

A bug starts at Medium. A feature starts at Medium if it is well defined and at Low if it is still an idea. The criteria above then move it up or down.

### Order

Both repositories form one queue, worked in this order:

1. pinned issues, first;
2. then by priority, higher before lower;
3. within the same priority, bugs before other types;
4. then oldest first.

Pinned issues follow the same rules among themselves. The maintainer changes the order by pinning an issue; GitHub allows up to three pinned issues per repository. An issue that hasn't been triaged has no priority yet and isn't in the queue. To find the next issues across both repositories, search for:

```text
is:issue is:open (repo:mnemcik/consigliere OR repo:mnemcik/cg-extensions) field.priority:high sort:created-asc
```

Then search with `medium`, then `low`. The results come oldest first; pinned issues, and bugs within a priority, are picked out by eye.

### Triage

The maintainer triages each new issue in one of two ways:

- **Keep it:** set the type and the priority, record the estimated size on the issue (see [From issue to pull request](#from-issue-to-pull-request)), and remove `needs-triage`.
- **Close it:** as a duplicate (with a link to the issue that stays), or as not planned, with a short reason.

If an issue is missing details, such as steps to reproduce, the cg version or the platform, the maintainer asks for them in a comment.

## Submitting changes

A change goes through three roles. Whoever fills a role, a person or an agent, has its responsibilities:

- **Author:** picks up the issue, writes the analysis and the change, opens the pull request and carries it until it is ready to merge.
- **Maintainer:** triages issues, agrees the approach, reviews the impact, sets `impact-review`, approves and merges. Someone acting with the maintainer's authorisation, such as an intake agent, can set `impact-review`, approve and merge for the maintainer.
- **Reviewer:** reviews the code; a person or an automated reviewer.

The author never sets `impact-review`, approves or merges its own PR, even under the maintainer's account. Acting with the maintainer's authorisation means an explicit grant from the maintainer, for one PR or a standing one the maintainer set up.

The same rules apply to outside contributors. How an agent acts in a role is defined in the [`pr-workflow` plugin](https://github.com/mnemcik/claude-marketplace) (skills `pr-author` and `pr-intake`), and later in the cg `pr-workflow` extension ([cg-extensions#45](https://github.com/mnemcik/cg-extensions/issues/45)).

For every pull request:

- Open it against `main`.
- PR titles follow [Conventional Commits](https://www.conventionalcommits.org/) (see below) — they become the squash-merge commit subject.
- Run `make check` locally before pushing.
- CI (lint, including a Windows build, tests on Linux, macOS and Windows, cross-platform build) must be green.
- When a change can behave differently per platform, run CI on your pushed branch before opening the PR: `gh workflow run ci.yml --ref <branch>` prints the run's URL (on gh older than 2.87, find the run with `gh run list --workflow ci.yml --branch <branch>`); then `gh run watch <run-id> --exit-status`. From a fork, run both in your fork (pass `-R <you>/consigliere`; Actions must be enabled there).
- No DCO sign-off is required.

### From issue to pull request

1. **Issue first.** Every change starts as an issue. The maintainer triages it ([Triage](#triage)) and estimates its size: **S** (a few lines), **M** (one milestone) or **L** (several milestones, or new behaviour across components). The analysis confirms or changes the size; the agreement fixes it.
2. **The item's folder.** The author writes the item's documents in `specs/<tracker>-<id>-<slug>/` (for an issue in this repository, `specs/gh-<number>-<slug>/`): `analysis.md`; `spec.md`, with the acceptance criteria; and `plan.md` and `tasks.md` as the size needs. The author pushes the folder on the item's branch, named like the folder, before asking for the agreement. The folder is committed with the change and kept as history.
3. **The agreement.** The maintainer agrees the approach in one comment on the issue, naming the folder, the commit of the agreed `spec.md` and `plan.md`, and their committed content hashes (`git rev-parse <commit>:<path>`; "plan: none" for an S item). The maintainer posts it, or someone posts it for the maintainer and the comment says so. A later change to the spec or the plan needs a new agreement comment. Nothing is implemented before the agreement.
4. **The pull request** is opened when the work is done, never as a draft. Its description says `Fixes #NNN`.

### How a PR gets to merge

1. **Impact review.** The maintainer reviews what the PR changes for every workspace (the `impact-review` status is described in the next section). It checks:
   - **No private data** in the diff, the description or the commit messages: no names of people, or of the employers, clients or customers behind a workspace (attribution for copied content excepted), and no internal hosts or paths, ticket keys or incidents. The repository is public.
   - **Fit and layer:** the change belongs in core (the binary, or the framework templates every workspace gets) rather than in an extension or a single workspace. Third-party tools, a specific review bot for example, go in an extension, not in core.
   - **Real and current:** the problem exists on `main`, no other open PR or issue already covers it, and it doesn't conflict with an earlier maintainer decision (the review comment says which, if one applies).
   - **Impact stated and true:** the "Impact on workspaces" section matches the diff. Additions to the CLAUDE.md template keep every-session rules short and put trigger-only detail in a note. A rule that hands a step to a skill or hook still works in workspaces that don't install the Claude Code integration.
   - **Shape:** one concern per PR, tests for code, docs where behaviour changes, works on Windows, and the title's type matches the change. Copied content needs a compatible licence and attribution.
   - **Safety:** changes to workflows, `install.sh`, release config, hooks or settings get extra scrutiny. A workflow job named like a required check (`impact-review` above all) is rejected, since its result would satisfy that check without a review.
2. **The outcome comes as a PR comment:** agreed (and `impact-review` is set), or changes requested. A plain agree on the maintainer's own PR may come without a comment.
3. **The PR's author carries it from there:** makes the requested changes, updates the branch from `main`, and runs the review loop until CI is green and every review comment is answered. On a PR from a fork, CI runs only after the maintainer approves the workflow run, so a waiting CI isn't a stalled PR.
4. **A push leaves the new head without `impact-review`** (see [Impact review](#impact-review)). Before merge, the maintainer reviews the PR again at its final head: the whole PR, not only the new commits. A change in scope can mean a new review rather than a quick re-check; a push that only updates the branch from `main` needs just the check described there.
5. **One approving review** is required by branch protection. The approval is the maintainer's: the maintainer approves, or obtains an automated reviewer's approval; on PRs opened under the maintainer's account, GitHub allows only the automated reviewer. Later pushes don't dismiss it.
6. **Merging is the maintainer's.** Only the maintainer, or someone acting with the maintainer's authorisation, merges.

**The description becomes the commit message.** PRs are squash-merged with the PR description as the commit body, so it ends up in `main`'s history. Keep it current, updating it after every push that changes what the PR does, and keep private details out of it.

### Impact review

Every PR changes what every workspace gets on `cg sync`, so every PR gets an **impact review** by the maintainer before it can merge, whoever opened it. PRs opened by the release and dependency bots (`consigliere-release-bot`, Dependabot, Renovate) are exempt.

- **The `impact-review` check** (required) is a commit status on the PR's head commit. Until it is `success`, the PR shows it as expected and can't merge. The Impact Review workflow sets it to `success` for the bots and posts nothing for other PRs, so it can't replace an approval. After reviewing what the PR changes for every workspace, the maintainer sets it to `success` on that commit:

  ```bash
  gh api -X POST repos/mnemcik/consigliere/statuses/<head-sha> \
    -f state=success -f context=impact-review -f description="Impact reviewed"
  ```

- **New commits need a new review.** A status belongs to one commit, so a push leaves the new head without `success` until the maintainer reviews the PR again at its new head. That includes updating the branch from `main` (branch protection requires up-to-date branches): the maintainer re-marks the new head after checking that the PR's own change is unchanged and that nothing which landed on `main` since the last reviewed head contradicts it, already covers it, or removes what it builds on. If resolving conflicts changed the PR's own change, the resolution is reviewed too, and a larger change can mean a new review; if something that landed clashes with it, the maintainer comments as in step 2 of [How a PR gets to merge](#how-a-pr-gets-to-merge).
- **Who sets it:** for PRs that aren't exempt, only the maintainer, or someone acting with the maintainer's authorisation, and only after reviewing that commit. Any account or app with write access to statuses technically can; fork contributors can't.
- **Bot PRs are exempt by who opened them.** Commits a person pushes to a bot's PR get `success` too, so don't push changes to bot branches; open a separate PR. Updating a bot's branch from `main` by a **clean** merge (GitHub's **Update branch** when it reports no conflicts, or `gh pr update-branch`, which refuses on a conflict) is fine: it brings in only what is already on `main`, which was reviewed, or exempt, when it landed. The maintainer's re-check after an update from `main` (the bullet above) doesn't apply to bot PRs: the bot's own change is exempt from the impact review, so the only check left is the one at the end of this bullet. If the update conflicts, don't resolve it on the bot's branch: a conflict resolution is a person's edit, and it would get `success` unreviewed. Let the bot regenerate its branch, or open a separate PR. The exemption still sets `success` on the new head, so whoever updates the branch, and the maintainer before merging, checks that it adds nothing beyond `main` and the bot's own changes. A release PR needs this when a commit `release-please` doesn't release (`docs:`, `ci:`) lands on `main`, because `release-please` then leaves its branch behind.
- **Fill in "Impact on workspaces"** in the PR template: what changes on `cg sync` for workspaces that never edited the affected files, which edited files will show as drifted (and, if known, what kind of workspaces edited them), and how Claude behaves differently.

## Release process

Releases are automated by [release-please](https://github.com/googleapis/release-please).
**Do not** hand-edit `CHANGELOG.md`, bump a version, or push tags.

1. PRs merge to `main` with Conventional-Commits titles (`feat:`, `fix:`, etc.). The
   PR title is validated by the **PR Title Lint** check and — because we squash-merge —
   becomes the single commit on `main` that release-please reads.
2. release-please keeps an open **"chore: release X.Y.Z"** Release PR that accumulates
   the `CHANGELOG.md` section and the version bump derived from the merged commits.
   The version `cg init` writes into `.cg.json` comes from the binary, not a file.
3. **To ship, the maintainer merges the Release PR.** release-please then tags the repo and creates the
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
