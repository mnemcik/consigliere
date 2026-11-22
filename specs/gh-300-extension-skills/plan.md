# Plan — #300

Size M (analysis D10): one milestone, delivered as a small group of commits on `gh-300-extension-skills`, with an
independent review of the implementation once the group is complete. Title: `feat!: extensions contribute skills
they own`. The description closes #300. It ships in cg 2.0.0 with #259 and #301 (D5); its squash body adds to the
2.0.0 migration note: a manifest that declares `skills` needs `"manifest": 2`, and cg 1.x refuses it.

**Start:** after #259's M1 has landed on `main` (manifest v2, `Commands` in the ledger, the pinned-manifest helper,
the pre-apply conflict check). The branch is then rebased onto it. Until then nothing is implemented; if #259's spec
changes before agreement, this spec and plan are re-checked first.

Every new test must fail before its change. Guard tests (refusals: S2–S8, S12, S15–S20, S18b, S18c, S18d, S33) also get a mutation
check: remove the guard, confirm the test fails, restore. `make check` passes at every commit. Windows rules from
CONTRIBUTING apply: path comparisons use `filepath.EvalSymlinks` and don't depend on case; the mode tests (S13–S13c)
skip on Windows with a reason, where modes are never compared (S13c), and a Windows test checks that nothing is
reported.

## Commits

### 1 — manifest and configuration

Scenarios: S1–S5, S12 (config part).

- `internal/extension/manifest.go`: `SkillContribution{Src}`; `Contributions.Skills`; `Validate`: path inside the
  extension, base-name syntax, duplicates, refused under `"manifest": 1` (S5, the same check #259 adds for
  `commands`).
- `internal/workspace/workspace.go`: `Skills *SkillsConfig` (`targets []string`), absent → `["claude"]`; a
  `SkillTargets()` accessor.
- New `internal/extension/skills.go`: the target table (`claude` → `.claude/skills`, `agents` → `.agents/skills`,
  with the user-scope dir for each), unknown-target error (S12).

### 2 — reading and installing a skill

Scenarios: S6–S11, S8b, S13–S13c, S14, S20, S22–S24, S42.

- `skills.go`: read a skill from the pinned tree with one `git ls-tree -r` at the pinned commit: file list, modes,
  and refusal of `120000`/`160000` entries (S8, Q3, Q4); a legacy clone without a commit uses the clone's `HEAD`.
  Require `SKILL.md`; parse its frontmatter for `name` and `description` (S7).
- `internal/extension/ownership.go`: for skill files, the extension's version is (content id, executable); a
  content match with a mode mismatch is a mode fix, not a write and not drift (S13a, S13c).
- `internal/extension/apply.go`: `planCopies` gains the skill files (one plan per file per target), reusing
  `decideCopy` and the snapshot; `writeFileAtomicMode` with the extension's mode, and a chmod for a mode fix;
  `Reverse` and `OrphanLedger` cover skill files and remove directories left empty under `<target dir>/<name>/`
  (S24, S37); the snapshot also records the directories a run creates and removes them on rollback (S42).
- `internal/extension/ledger.go`: `Skills []LedgerSkill{Name, Targets, Files}` (`Files` = the workspace-relative
  paths installed, S14); bases in the ledger's `Files` map.

### 3 — ownership: drift, and moves between `templates` and `skills`

Scenarios: S25–S27, S38–S41.

- `internal/extension/apply.go`: `ApplyOptions.base` (`:57-68`) also treats a destination as tracked when the old
  ledger lists it in a skill's `Files`. Templates→skills already passes it (the old ledger lists the files under
  `Templates`); skills→templates needs it (S41). `OrphanLedger` (`:337-347`) counts skill files as still shipped in
  both directions (S38, S41).
- For a skill file, `copyKeep` is reported in the update's output as drifted instead of kept silently; notes and
  templates unchanged (U1). Nothing is recorded (Q1).
- `ledger.go`: `Drifted []DriftedFile{Dest, Acknowledged string}` holding only `--keep` acknowledgements (Q1); an
  acknowledgement goes when the extension's version of the file changes (S27).
- A helper `DriftAll(root, refs)` that computes drift from disk for every skill file (Q1), minus acknowledged
  content ids.

### 4 — conflicts, list, notice, resolve

Scenarios: S15–S19, S18b–S18e, S21, S28–S37.

- Skill-name conflicts in the same pre-apply pass as #259's `CheckCommands` (install, update over the whole new
  set, `cg init` over the whole set), per target; the framework's skill and command names from
  `frameworkClaudeFiles` (Q8); a note or template inside a skill's directory (S18c).
- The pre-apply pass also builds the graph of skill moves between installed extensions (dropper → gainer): no
  cycle → refuse with the ordered `cg extension update` commands (a topological order, S18b); a cycle → refuse
  naming it (S18d); a conflict with an extension not being updated adds the "update it first" hint (S18).
  `runExtUpdate` (`cmd/extension.go:730-750`) keeps applying one target at a time. Tests: one move, a chain of three,
  a swap, a ring of three, and S18e following the printed advice, once with no edited file and once with an edited
  file the dropper keeps (the gainer's update refuses it, then succeeds after it is moved aside).
- User-scope shadowing warning at install and update, and in `cg extension list` (S21, S28).
- `cmd/extension.go` `list` (text and `--json`): skills, targets, drifted and pending counts.
- `cmd/extension_resolve.go`: `pending` lists drifted files in their own group (S29); `diff` and `resolve` accept a
  drifted file: `--take` writes the extension's version with its mode (S13b; `writeWorkspaceFile` writes `0644`
  today, `extension_resolve.go:341-343`), `--keep` records the acknowledgement, `--merge` refused (S31–S34).
- `cmd/session.go` `pendingFilesNotice`: adds the drifted files not yet shown on this machine, recorded in
  `<git-common-dir>/cg/drift-shown.json` (Q2, S30, S35). The notice's text for Claude names the drift options
  separately from the pending procedure.
- S36: `reapply` installs for the current targets and reverses skill files under targets no longer listed.

### 5 — docs

Scenarios: X3.

- `docs/extensions.md`: the `skills` contribution point (shape, targets, ownership with the drift row added to the
  "Files you change" table, conflicts, modes, user-scope shadowing); `templates` wording; v2 for `skills`; the
  `skills.targets` setting.
- `EXTENSIONS.md`: authoring and review notes (relative paths, `allowed-tools`).

## Cross-repo (after this PR is approved, before the 2.0.0 release PR is merged)

1. mnemcik/cg-extensions-registry: schema accepts `skills` under `manifest: 2`, in the same change as #259's
   schema step (X1).
2. mnemcik/cg-extensions `github-digest`: the move to `skills` (X2), version bump, registry index bump.
3. After the release: `cg update`, then `cg extension update github-digest` in a workspace that has the old
   version, checking S38 end to end.

## Overlap

With #259's M1: `manifest.go`, `ledger.go`, `apply.go`, the pre-apply check and `list` in `cmd/extension.go`,
`docs/extensions.md`, `EXTENSIONS.md` and the registry schema. Nothing here touches dispatch (`cmd/external.go`),
`cg start` or session state, which #259's M2–M4 change, so this can run beside them once M1 has landed. #301 changes
hook entries in `manifest.go` and `settings.go`; whichever lands second rebases.

## Risks

- `git ls-tree` per install for modes: one call per extension, measured once.
- A skill installed for two targets doubles its files in the workspace and in Copilot or Cursor, which read both
  directories: documented, not de-duplicated (analysis, rabbit holes).
- The drift check at session start reads every skill file of every extension: expected to be a few dozen files;
  measured once in commit 4.
- A workspace committed from Windows records skill scripts as `100644`; every other platform fixes the mode
  locally on the next install, update or `cg init` (S13a), but the committed mode stays until someone commits it
  from a platform with modes. Documented.
- `<git-common-dir>/cg/` is new local state; a workspace without git (none today: cg requires it) would skip the
  "once" record and show drift every session.
