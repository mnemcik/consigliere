# Plan — #300

Size M (analysis D10): one milestone, delivered as a small group of commits on `gh-300-extension-skills`, with an
independent review of the implementation once the group is complete. Title: `feat!: extensions contribute skills
that cg manages`. The description closes #300. It ships in cg 2.0.0 with #259 and #301 (D5); its squash body adds to
the 2.0.0 migration note: a manifest that declares `skills` needs `"manifest": 2`, and cg 1.x refuses it.

**Start:** after #259's M1 has landed on `main` (manifest v2, `Commands` in the ledger, the pinned-manifest helper,
the pre-apply conflict check). The branch is then rebased onto it. Until then nothing is implemented; if #259's spec
changes before agreement, this spec and plan are re-checked first.

Every new test must fail before its change. Guard tests (refusals: S2–S11, S16, S16b, S19–S25b, S31, S34b, S37,
S38) also get a
mutation check: remove the guard, confirm the test fails, restore. `make check` passes at every commit. Windows rules
from CONTRIBUTING apply: path comparisons use `filepath.EvalSymlinks` and don't depend on case; the mode tests (S17,
S32) skip on Windows with a reason; the symlink test (S9) skips where the platform can't create one.

## Commits

### 1 — manifest and configuration

Scenarios: S1–S6, S16 (config part).

- `internal/extension/manifest.go`: `SkillContribution{Src, Executables}`; `Contributions.Skills`; `Validate`: path
  inside the extension, the Agent Skills name rule, duplicates, `executables` paths, refused under `"manifest": 1`
  (S5, the same check #259 adds for `commands`).
- `internal/workspace/workspace.go`: `Skills *SkillsConfig` with `Targets *[]string` (no `omitempty` on the slice),
  so an absent key and `[]` stay distinct through `Config.Save` (`workspace.go:130-131`), which every update runs;
  absent → `["claude"]`; a `SkillTargets()` accessor; a round-trip test (S15).
- New `internal/extension/skills.go`: the target table (`claude` → `.claude/skills`, `agents` → `.agents/skills`,
  with the user-scope dir for each), unknown-target error (S16).

### 2 — reading and installing a skill

Scenarios: S7–S12, S12b, S13–S15, S16b, S17, S18, S25, S25b, S27, S27b, S28, S29, S32, S33, S33b, S42, S42b.

- `skills.go`: read a skill from the pinned tree: walk `src` with `Lstat`, refuse symbolic links (S9), require
  `SKILL.md` and refuse a shipped `SKILL.local.md` (S11), parse its frontmatter for `name` and `description` (S8),
  check `executables` against the files (S10), and warn when `SKILL.md` doesn't mention `SKILL.local.md` (S12b).
- A managed-file plan, separate from `decideCopy` (which stays for notes and templates, U1): per file and target,
  checked in this order: adopt when disk already holds the new version or is absent for a drop (S27b); write when
  unedited (S27); set mode only (S32); refuse unowned (S25b); replace with message (S30) or stop (S31) when edited.
  The plan is made for every extension being updated before anything is written.
- For each managed path, the longest existing prefix (and each directory before it is removed as empty) resolved
  with `filepath.EvalSymlinks` and refused when outside the workspace, in the pre-apply pass; directories below it
  are created by cg; "exists" by `Lstat`, and a link that can't be resolved is refused (S16b). Tests: a fresh
  install with no target dir, a symlinked target dir, a symlinked `<name>/`, a dangling link at `<name>/`, a
  symlinked subdirectory inside the skill.
- `internal/extension/apply.go`: apply the plan through the existing snapshot; `writeFileAtomicMode` with `0755` or
  `0644` (S17); deletion of dropped files and of directories left empty, never local files (S29, S33), with the
  local files left behind reported (S33b); deletions go through the snapshot too, and it records the directories a
  run creates, so rollback restores both (S42); the ledger is saved last, and S27b makes a run cut short before
  that converge (S42b).
- `internal/extension/ledger.go`: `Skills []LedgerSkill{Name, Targets, Files []string}`; bases in the existing
  `Ledger.Files` map, so `reapply`'s and `runExtRemove`'s "no content ids yet" check (`cmd/extension.go:196-208`,
  `:662-665`) and `ApplyOptions.base` (`apply.go:62-63`) keep working unchanged (S18).

### 3 — edits: replaced, or stopped when uncommitted

Scenarios: S30, S31, S34, S34b, S35, S36, S36b, S36c.

- `internal/gitx`: a helper returning, for a list of paths in the checkout being applied to, which are committed:
  one `git status --porcelain=v1 -z --untracked-files=all --ignored -- <paths>` (`-z` for quoted paths; git applies
  line endings and filters), any entry meaning not committed (a rename or copy entry's two paths both matched), an
  ignored entry reported as such; plus `git ls-files -v -z -- <paths>`, where a lowercase tag (assume-unchanged) or
  `S` (skip-worktree) means not committed; an unborn `HEAD` or a non-repository → none committed (Q10). Long path
  lists are split into batches that fit the platform's command-line limit. Tests: CRLF checkout, staged-only change,
  ignored path, assume-unchanged and skip-worktree paths, a rename, unborn `HEAD`.
- The plan of commit 2 uses it: edited and committed → replace with the S30 message; edited and not committed →
  stop the whole run with the S31 message (pre-apply, over every extension being updated). Output collects the
  messages once per file.
- `cg extension remove` goes through the same plan (S35). `cg init`'s re-install uses it with one difference: an
  edited managed file is kept with the S36 message, never replaced and never a stop (Q11).
  A kept file keeps the extension's content id as its base (not adopted). `reinstallMissingExtensions`
  (`cmd/extension.go:361-403`) keeps `reapply`'s result to print its S36 and S33b messages, which it discards today
  (`:398`), runs S16 and the target-directory part of S16b, before the first fetch, for every extension it is about to
  re-install (S36b), and words any later stop per S36c, listing every pinned extension fetched but not applied.
  Tests: init keeps an uncommitted edit, then `update` stops (S31); init stops on a symlinked target dir before
  fetching, fix, `cg init` again writes the skills; two extensions where one conflicts, both listed by S36c; A fetched
  and B's fetch failing, A listed.
- S34: `reapply` installs for the current targets and deletes managed files under targets no longer listed.

### 4 — conflicts, moves, list

Scenarios: S19–S24, S26, S37–S41.

- Skill-name conflicts in the same pre-apply pass as #259's `CheckCommands` (install, update over the whole new set,
  `cg init` over the whole set), per target; the framework's skill and command names from `frameworkClaudeFiles`
  (S20, Q8); a note or template inside a skill's directory (S24); the conditional hint (S22).
- The pre-apply pass also builds the graph of skill moves between installed extensions (dropper → gainer): no cycle
  → refuse with the ordered commands (a topological order, S37); a cycle → refuse naming it (S38). `runExtUpdate`
  (`cmd/extension.go:730-750`) keeps applying one target at a time. Tests: one move, a chain of three, a swap, a
  ring of three, and S39 following the printed commands, with and without an uncommitted edit.
- `internal/extension/apply.go`: `ApplyOptions.base` (`:57-68`) also treats a destination as tracked when the old
  ledger lists it as a skill's file (S41; templates→skills already passes, since the old ledger lists the files
  under `Templates`); `OrphanLedger` (`:337-347`) counts skill files as still shipped in both directions (S40, S41).
- User-scope shadowing warning at install and update, and in `cg extension list` (S26).
- `cmd/extension.go` `list` (text and `--json`): skills and the targets each is installed for.

### 5 — docs

Scenarios: X3.

- `docs/extensions.md`: the `skills` contribution point (shape, `executables`, targets, the managed model with its
  two messages, `SKILL.local.md`, conflicts, user-scope shadowing); `templates` wording; v2 for `skills`; the
  `skills.targets` setting.
- `EXTENSIONS.md`: the authoring rule for `SKILL.local.md` (Q12), relative paths, `allowed-tools` in review.

## Cross-repo (after this PR is approved, before the 2.0.0 release PR is merged)

1. mnemcik/cg-extensions-registry: schema accepts `skills` under `manifest: 2`, in the same change as #259's schema
   step (X1).
2. mnemcik/cg-extensions `github-digest`: the move to `skills` (X2), version bump, registry index bump.
3. After the release: `cg update`, then `cg extension update github-digest` in a workspace that has the old version,
   checking S40 end to end.

## Overlap

With #259's M1: `manifest.go`, `ledger.go`, `apply.go`, the pre-apply check and `list` in `cmd/extension.go`,
`docs/extensions.md`, `EXTENSIONS.md` and the registry schema. Nothing here touches dispatch (`cmd/external.go`),
`cg start` or session state, which #259's M2–M4 change, so this can run beside them once M1 has landed. #301 changes
hook entries in `manifest.go` and `settings.go`; whichever lands second rebases.

## Risks

- The `HEAD` comparison runs one `git ls-tree` per extension with skills on every update; measured once.
- A skill installed for two targets doubles its files in the workspace and in Copilot or Cursor, which read both
  directories: documented, not de-duplicated (analysis, rabbit holes).
- A skill whose author leaves out the `SKILL.local.md` line can't be customised without copying it under a new name;
  caught in review (Q12), not by cg.
- `cg init` decides what to re-install from whether the extension's tree is in the machine-wide store, so a
  workspace cloned again on a machine that already has the tree isn't re-installed (today's behaviour for every
  contribution, not changed here).
- A machine set up with `cg init` runs an edited skill text until the next `cg extension update` (S36, Q11).
- A `cg init` that stops after fetching (S36c) can't be resumed by running `cg init` again, and the only command
  that re-applies (`cg extension update`) moves the pin to the latest version. Re-applying at a recorded commit
  would be a new path in `updateOne` (`cmd/extension.go:771-779` always resolves the latest tag or default branch);
  not added here. #259's whole-set check in `cg init` (its S12) has the same property; raised for #259.
