# Plan — #301

Size **M**: one milestone, one PR on `gh-301-extension-hook-filters`, independent review before the PR opens and
after each substantive push. Title: `feat: matcher and if filters on extension hooks (manifest v2)`. Not breaking:
v1 manifests and hooks without filters behave as today (U1, H10). The description says `Fixes #301`. It ships in cg
2.0.0 with #259, #300 and #212 (decision 1), merged before the 2.0.0 release PR (decision 9).

**Start and base (Q1):** after the agreement and once #300's PR is open. The branch is based on `main` (rebased at
`8840343`, #259 included). Before opening the PR, it is rebased onto `main` as it is then, with #300 and #212 if
merged. After each rebase, compare `git diff <old-base>...<old-head> | git patch-id --stable` with the same for the
new base and head, list the files that landed on `main` since and overlap the branch, and say both on the PR.

Every new test must fail before its change. Guard tests (refusals and removals: H2, H3, H6, H7, H12, H14) also get a
mutation check: remove the guard, confirm the test fails, restore. `make check` passes at the milestone commit.
Windows rules from CONTRIBUTING apply: settings tests compare decoded JSON, not bytes with line endings, and install
paths are built with `filepath` and compared with `/`.

## M1 — filter fields on hook entries

Scenarios: H1–H15, U1–U4.

- `internal/extension/manifest.go`:
  - `HookContribution` gains `Matcher string` (`json:"matcher,omitempty"`) and `If string` (`json:"if,omitempty"`);
    `""` means unset (Q4).
  - An exported list of the `if` events (five or six, per Q3), the one place to change when Claude Code adds one.
  - `Validate`, in the hooks loop: the v2 gate (H2), with the same message shape as the `commands` gate; `if` only
    on an `if` event (H3); no matcher check (H4). After the loop: duplicates keyed by event, matcher (unset and `"*"`
    as one, Q5), `if` and the cleaned wrapper (`CleanSubdir`), naming both indexes (H6); install-path collisions
    between different cleaned wrappers through `hookDestRel`, naming both indexes and the path (H7), as the
    `git-hooks` check does.
- `internal/extension/settings.go`: `registerHook(root, event, commandRel)` becomes `registerHooks(root, regs)`, with
  one `{Event, Matcher, If, CommandRel}` per entry. It loads `settings.json` once, removes every registration of
  each distinct `CommandRel` in any event (`removeHookCommand`, unchanged), appends one group per entry in order
  (H9; the `if` key only when set; `matcher` written as `""` when unset, H10), and saves once (H13).
  `unregisterHook` and `removeHookCommand` stay as they are (H14, H15).
- `internal/extension/apply.go`: the hooks loop first copies each distinct wrapper once to its install path, then
  collects the registrations and calls `registerHooks` once, and appends one `LedgerHook{Event, Wrapper}` per entry (H11). A
  failure in this step is undone by the existing snapshot (`apply.go:157`, one capture per install path, restored
  on any apply error), which restores `settings.json` and removes the copied wrappers; no extra cleanup is added. `removeLedger` and `OrphanLedger` need no
  change: they work by wrapper path, and a repeated wrapper is a no-op the second time (test it).
- Tests:
  - `manifest_test.go`: H1–H8, each refusal with its message; H6 and H7 on a v1 manifest too; v1 with
    `"matcher": ""` validates (Q4); `"*"` against unset as a duplicate (Q5); two entries differing only in `command`
    are a duplicate; the same wrapper spelled `./hooks/a.sh` and `hooks/a.sh` is one wrapper, not a collision.
  - `settings_test.go`: the written shape (H9, H10) against a decoded `settings.json`; a wrapper in two events; two
    `if` rules on one event; the framework's and a user's hooks and unknown top-level keys kept (H15). H13:
    `saveSettings` itself becomes a package-level function variable (its other callers, `unregisterHook` and
    `NormalizeHookCommands`, are unaffected), which the counting tests replace and restore with `t.Cleanup`; they don't call `t.Parallel` (no
    test in the package does today). A `registerHooks` call with three entries (two wrappers, one repeated) saves
    exactly once.
  - `apply_test.go`: install, update with a changed filter, an update that drops an entry, remove (H11, H12, H14);
    a v1 extension's `settings.json` unchanged by this cg (H10); H13 at the apply level: a fresh install through
    `ApplyWith` (no earlier ledger) of an extension with two wrappers in three entries saves `settings.json` once (same counter). Mutation checks: saving inside
    `registerHooks`'s per-entry loop fails the first count, and calling `registerHooks` once per entry from the
    apply loop fails the second; as a regression test for H13's rollback (it passes
    today too, through the snapshot), the existing pattern (`apply_test.go:133`, a missing wrapper source) with two
    wrappers, the second missing: the apply fails, `settings.json` is byte-identical and the first wrapper is gone.
- Docs: `docs/extensions.md` (the `hooks` contribution point and the manifest-v2 list of what needs v2) and
  `EXTENSIONS.md` (`hooks`), as the spec's Documentation section says.

## Cross-repo (after the agreement; each merge on the maintainer's go-ahead)

1. **mnemcik/cg-extensions-registry schema PR (X1, Q2):** its own worktree of the registry on a fresh branch from
   `origin/main`, title `feat(schema): matcher and if on hook entries (manifest v2)`. Hook items gain `matcher` and
   `if`; a hook entry that sets either requires `"manifest": 2`; `if` only on the `if` events (Q6). The schema's
   top-level `if`/`then` already holds the `commands` gate, so the gates are combined under `allOf` rather than
   replacing it; the `manifest` description names the new reason for v2. Checked by validating every manifest on
   cg-extensions `main` and a set of passing and failing samples (each H2/H3 refusal, the `"if": ""` refusal of Q4,
   and a v1 manifest with `"matcher": ""` passing, so the v2 gate tests for a non-empty value as cg does)
   with `jsonschema`'s Draft 2020-12 validator, as cg-extensions CI does. Additive and harmless to indexed
   manifests, so it can merge any time after the agreement; it must merge before any extension manifest uses the
   fields. The schema PRs of #212 and #300 change the same file: whichever merges second rebases and keeps every
   gate under the one `allOf`.
2. **This PR** (M1), then the 2.0.0 release PR.
3. Extensions adopt the fields in their own changes after the release (cg-extensions#45; optionally `1password`
   dropping its second wrapper file). Not part of #301.

## Risks

- **Overlap with #300 and #212** in `manifest.go` (`Validate`), `apply.go` and `docs/extensions.md`: a rebase
  conflict, not a behaviour clash (#300 leaves hooks alone, its U4). Resolved by rebuilding on the merged code,
  then checking the patch-id.
- **Claude Code changes the `if` events.** cg refuses `if` on a new event until its list is updated (Q3); the fix
  is a one-line change plus a test.
- **An older Claude Code without `if` support** reading a workspace whose extension sets `if`: not checked
  (analysis, hidden constraints; decision 5 removed the prerequisite together with `onFailure`). Only workspaces
  with such an extension are exposed, and the `pr-workflow` plugin already writes `if` today.
- **Hand edits to an extension's groups** in `settings.json` are replaced on update, as today (H12); the docs say
  so.
