# Plan — #212

One PR from `gh-212-extension-cg-version` into `main`, one commit (or a small group) per milestone, an independent
review after each milestone and a final round over the whole PR. Title: `feat: extensions state the cg version they
need (requires.cg)`. The description says `Fixes #212`. It merges before the 2.0.0 release PR (#312), rebased on
whichever of #300 and #301 merged first ([spec Q1](spec.md#q1)). A separate registry PR carries the schema
(step R below).

Every new test must fail before its change. Guard tests (refusals: S2, S7–S10, S13, S13b, S14) also get a mutation
check: remove the guard, confirm the test fails, restore. `make check` passes at every milestone commit. Windows:
the change is string handling and the existing apply/dispatch paths; no paths, modes or processes are added
(CONTRIBUTING "Supported platforms"); dispatch tests that exec binaries skip on Windows as #259's do.

Tests set the running version through `cmd.Version` (saved and restored with `t.Cleanup`), never through a build.

## M1 — the field, its format and the comparison

Scenarios: S1–S6, S14.

- `internal/extension/manifest.go`: `Manifest.Requires *Requires` (`json:"requires,omitempty"`), `Requires{CG
  *string json:"cg,omitempty"}`; a pointer, so a present empty string is refused (S2) rather than read as absent.
  `Validate`: `requires.cg` against `versionRe` (the d2 pattern, one constant, its test table shared with the
  registry step's samples); message as S2 with `%q`. The schema-version refusal adds the update hint when
  `m.Manifest > SchemaVersion` (S14).
- New `internal/extension/requires.go`:
  - `RunningCore(v string) (string, bool)`: an optional leading `v`, then `^(\d+)\.(\d+)\.(\d+)` followed by
    the end, `-` or `+`; all three parts required, so `dev`, a hash (also an all-digit one such as `1234567-dirty`)
    and empty return false (S6).
  - `UnmetRequirement(m *Manifest, running string) (need, have string, unmet bool)`: `semver.Compare("v"+need,
    "v"+core) > 0`; short forms compare as their `.0` completions (S5).
  - `RequirementMessage(name, need, have string) string` and the shared `UpdateHint` constant (spec Terms), used by
    S7–S14.
- Tests: table tests for the pattern (every S2 value, every S1 form, v1 and v2), `RunningCore` (S5/S6 values,
  including `v2.1.0-4-gabc1234-dirty` and GoReleaser's `2.1.0`), the comparison, S4's wrong types, S14 for `3`
  and for `0`.

## M2 — refused before anything is applied

Scenarios: S7–S11, U3.

- `cmd/extension.go`: `loadValidManifest` stays load + validate + name check. A new
  `checkRequirements(ms []*extension.Manifest) error` builds one error listing every unmet requirement (S10), one
  requirement message per line, `cgerr.ExitUsage`; it reads `Version`.
  - install: after `installFrom`, before any apply (S7).
  - update: after the `resolveUpdate` loop, before `checkCommands` (S8). Only the run's targets.
  - `cg init`: in `reinstallMissingExtensions`, after the fetch loop, before `checkCommands` (S9). Only the
    entries in `todo`; the deferred clean-up already removes fetched trees on error.
  - The fetch loops keep stopping at a fetch or validation error; only the requirement errors are collected
    ([spec Q7](spec.md#q7)).
- Tests (with mutation checks): install refused with the workspace byte-for-byte unchanged (files, `.cg.json`,
  ledger, settings), also with `--force`; update-all with one unmet target applies none and keeps every version;
  update of a target whose neighbour (not a target) has an unmet requirement proceeds; `cg init` with one unmet
  missing extension applies none and leaves no fetched tree; a present extension with an unmet requirement doesn't
  stop `cg init`; two unmet extensions are both named; `Version = "dev"` installs (S6).

## M3 — runtime readers

Scenarios: S12, S13, S13b, S13c, U2.

- `verifiedManifest` unchanged ([spec Q4](spec.md#q4)), so `installedCommands`, `unverifiableExtension`,
  `ownedCommands` (and #300's caller) count the extension as verified (S12).
- `cmd/external.go` `namespaceEntries`: after the existing checks (manifest, declared with that binary, binary
  present), and only when they all pass, an unmet requirement sets the entry's `unverified` reason and a
  `requirement` flag; `dispatchIn` keeps S17's prefix (`cg %s: extension %q (installed in %s) can't run it: `) and
  prints the requirement and the update hint instead of S17's reason and fix (S13's exact text). A failing S17
  check wins over the requirement. The deciding-root rules don't change, so the stop, the
  namespace claim and "no shorter entry stands in" come from #259's code.
- `cmd/start.go` `findLauncher`: an unmet requirement warns per S13b and skips the launcher.
- `cmd/extension.go` `cg extension list`: `listedExtension.RequirementUnmet string`
  (`json:"requirementUnmet,omitempty"`) and the text line ([spec Q6](spec.md#q6)), from `verifiedManifest` read for
  every extension, before the ledger checks (today it is read only when there is a ledger).
- Tests (with mutation checks for S13, S13b): a session root whose `shop` extension needs a newer cg stops
  `cg shop list` with the requirement message and doesn't run a clone's or `PATH`'s `cg-shop`; a later longer match
  doesn't change that; an entry that is both unmet and missing its binary gets S17's message; an install of another extension that declares an unrelated command proceeds, and one that
  declares the same command conflicts (S12); the launcher test; `list` text and `--json`; git-hook trust unchanged
  for such an extension (U2).

## M4 — docs

Scenario: S15.

- `docs/extensions.md`: field table row for `requires`; a "Requiring a cg version" section (when to set it, D6's
  version bump still applies, a v2 manifest implies 2.0, when the check runs, the update hint); the schema-version
  paragraph (`:124-131`) mentions the hint. `EXTENSIONS.md`: one line for authors.

## R — registry schema (its own PR)

In mnemcik/cg-extensions-registry, after the agreement ([spec Q2](spec.md#q2)):

1. File an issue in mnemcik/cg-extensions: "registry schema: accept `requires.cg`", pointing at #212's agreement
   comment, which covers the content.
2. A fresh worktree of `~/source/cg-extensions-registry` on a new branch from `origin/main`; read its README's
   PR section first.
3. `schema/cg-extension.schema.json`: a top-level `requires` property (X1): `{"type": "object",
   "additionalProperties": false, "properties": {"cg": {"type": "string", "pattern":
   "^(0|[1-9][0-9]*)(\\.(0|[1-9][0-9]*)){0,2}$", "not": {"pattern": "\\s"}, "description": "Lowest cg release the extension works with
   (cg 2.0+ enforces it; older cg ignores it)."}}}`, outside the `if`/`then`, so v1 and v2 both accept it.
4. Test locally with Python `jsonschema` (Draft 2020-12, as cg-extensions' `validate.yml`): every manifest on
   cg-extensions `main` validates; v1 and v2 samples with `2`, `2.1`, `2.1.0` validate; `v2`, `2.1.0-rc.1`, `02`,
   `2.1.0.0`, `""`, `"2.1.0\n"`, `{"cg": 2}`, `{"cg": null}`, `{"other": "x"}` don't. Put the commands and results in the PR description.
5. PR title `feat(schema): requires.cg, the minimum cg version`; `Fixes mnemcik/cg-extensions#NNN`. Independent
   review, then hand it to the maintainer side. Can merge before or after cg 2.0.0; it must merge before any
   extension sets `requires.cg`. Not a hold on #312.

#300's and #301's registry changes edit the same file; whichever opens second rebases on the first. No extension
manifest changes in this rollout ([spec Q3](spec.md#q3)).

## Risks

- **Same files as #300 and #301** (`manifest.go`'s struct and `Validate`, `docs/extensions.md`'s field table):
  textual conflicts only; rebase on whatever merged and re-run the full suite.
- **A release candidate or a `git describe` build counts as its release** (S5): a `2.1.0-rc.1` binary meets
  `2.1.0`. Accepted by decision 4.
- **A fresh clone on an older cg restores no extension** until cg is updated (decision 5's stated cost); the
  message says which and how.
