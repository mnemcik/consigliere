# Spec — an extension states the cg version it needs (#212)

Implements decisions 1–9 of [analysis.md](analysis.md#open-decisions), as agreed in its
[Q→A log](analysis.md#qa-log). Each scenario is one behaviour and maps to at least one test; [plan.md](plan.md)
names the milestone that delivers it. The [questions](#questions-for-the-owner) at the end are gaps the decided
analysis doesn't settle, or places where `main` has moved since; each has a proposed answer for the owner's
agreement and is not decided here.

Written against `main` at `8840343` (#303, #259's implementation, merged as `10d241c`), #259's merged
[spec](../gh-259-extension-commands/spec.md) and [plan](../gh-259-extension-commands/plan.md), and #300's agreed
spec on `gh-300-extension-skills` at `0f5ba88`. "#259 S17" means scenario S17 of #259's spec.

## Re-check against `main`

The analysis was pinned to #259 at `decbfc0` and #300 at `021cbd9`. Against `main` and #300 at `0f5ba88`:

- **The field, the check and the messages (decisions 1–6) still fit.** `ParseManifest` is still a plain
  `json.Unmarshal` (`internal/extension/manifest.go:121-127`), `Validate` still refuses a newer `manifest` with no
  update hint (`:141-144`, now `unsupported manifest schema version %d (this cg understands 1 to %d)`), and there
  is still no version field or check. `golang.org/x/mod/semver` is still a dependency (`internal/autoupdate`).
- **Every apply path still loads through `loadValidManifest`** (`cmd/extension.go:509-522`): install
  (`installFrom`, `:486-506`), update (`resolveUpdate`, `:945`, called for every target before
  `checkCommands`, `:866-878`) and `cg init` (`reinstallMissingExtensions`, `:444`, before its whole-set check
  and before any `reapply`, `:461-474`). Decision 5's "refuse before anything is applied" falls out of where the
  check sits.
- **`verifiedManifest` exists, with more callers than the analysis named** (`cmd/extension_commands.go:27-41`). It
  returns an error, not a distinct result. Callers: the conflict check (`installedCommands`,
  `unverifiableExtension`, `ownedCommands`), `cg extension list` (`cmd/extension.go:610`), dispatch
  (`cmd/external.go:792`) and the launcher (`cmd/start.go:235`). #300's agreed spec adds a caller (#300 S20, "already ships a
  skill for `claude`"). Two readers of the pinned manifest don't use it:
  `TrustedGitHookScripts` (`internal/extension/githooks.go`) and `LegacyBases` (`internal/extension/ownership.go`).
  → [Q4](#q4), [Q5](#q5), [Q6](#q6).
- **`unverifiableExtension` already hints at a newer cg** for an installed tree whose manifest this cg can't use
  (`cmd/extension_commands.go:182`). Unchanged; S14 makes the manifest's own message say it too.
- **#300 at `0f5ba88`** (DEC-034's `executables` in the `skills` entries) adds no version field and doesn't touch
  the check. It edits the same `Manifest` struct, `Validate` and `docs/extensions.md` field table, and needs its own
  additive registry schema change in the same file (`schema/cg-extension.schema.json`). Order only; no behaviour
  overlaps.
- **Decision 7 has lapsed:** it applied "only if it is released before #303 is ready to merge". #303 merged and
  no 1.33.0 was released (latest release `v1.32.1`). Nothing in this spec or plan carries it.
- **Decision 9's sequencing no longer has its anchors:** #259's M1 commit is history on `main`
  ([Q1](#q1)); the registry schema change it was to ride with (#259's cross-repo step 1) merged as
  mnemcik/cg-extensions-registry#25 without `requires` ([Q2](#q2)); and the owner's note of 2026-11-26 on #212
  keeps "(cg 2.0+)" in the `live-runs` / `shopping` descriptions ([Q3](#q3)).

## Terms

- **Requirement:** the value of `requires.cg` in a manifest: the lowest cg release the extension works with.
- **Version pattern:** `^(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*)){0,2}$` (decision 2): `MAJOR`, `MAJOR.MINOR` or
  `MAJOR.MINOR.PATCH`, no leading `v`, no leading zeros, no pre-release or build suffix. Missing parts are `0`:
  `2` is `2.0.0`, `2.1` is `2.1.0`.
- **Running version:** `cmd.Version`. Its **core** is its `MAJOR.MINOR.PATCH`, after an optional leading `v`,
  with any pre-release or build suffix dropped: `v2.1.0-4-gabc1234-dirty` and `2.1.0` both have core `2.1.0`. A
  running version with no such core (`dev`, a bare commit hash, empty) has none.
- **Unmet:** a requirement greater than the running version's core. With no core, no requirement is unmet.
- **Update hint:** `Update cg (cg update upgrade, or brew upgrade --cask cg), then run this again.`
- **Requirement message:** `extension "<name>" needs cg <requirement> or newer; this is cg <core>. <update hint>`
  (decision 4's text).

## Manifest shape

```json
"manifest": 2,
"name": "shopping",
"requires": { "cg": "2.1.0" },
"contributes": { … }
```

`requires` is optional, top level, in v1 and v2 manifests (decision 3). In this change it takes only `cg`.

## Scenarios

### Manifest

- **S1** WHEN a v1 or a v2 manifest has `requires.cg` matching the version pattern THEN it validates.
- **S2** WHEN `requires.cg` is present and doesn't match the version pattern (empty, `v2.1.0`, `2.1.0-rc.1`,
  `2.1.0+build`, `02.1`, `2.1.0.0`, `2.x`, surrounding spaces, a control character) THEN validation fails with
  `requires.cg: invalid version "<value>": want MAJOR, MAJOR.MINOR or MAJOR.MINOR.PATCH`, the value quoted with Go's
  `%q` so no control character reaches the terminal.
- **S3** WHEN `requires` is absent, is `{}`, or has keys other than `cg` THEN the manifest validates; the other
  keys are ignored ("an older cg ignores a key it doesn't know", decision 2), and with no `cg` there is no
  requirement.
- **S4** WHEN `requires` isn't an object, or `requires.cg` isn't a string THEN parsing fails, as for any field of
  the wrong type today (`parsing cg-extension.json: …`).

### The comparison

- **S5** WHEN the running version's core is greater than or equal to the requirement (`2.1` against `2.1.0`;
  `2.0.0` against `v2.0.0`; `2.1.0` against `v2.1.0-4-gabc1234-dirty`; `2` against `2.0.1`) THEN the requirement
  is met.
- **S6** WHEN the running version has no core (`dev`, `abc1234`, empty) THEN no requirement is unmet: the check is
  skipped.

### Install, update and `cg init`: refused before anything is applied

- **S7** WHEN `cg extension install` (registry, direct URL, `--path`, with or without `--force`) fetches a manifest
  whose requirement is unmet THEN it fails with the usage exit code (`cgerr.ExitUsage`) and the requirement message, and the workspace
  (files, `.cg.json`, ledgers, `.claude/settings.json`) is unchanged.
- **S8** WHEN `cg extension update` (one target or all) fetches a new version whose requirement is unmet, for any
  target THEN the run fails with the requirement message before any target is applied, and every installed version
  stays (decision 5). Extensions that aren't targets of the run aren't checked against their requirement.
- **S9** WHEN `cg init` re-installs an extension (its tree was missing) whose recorded version's requirement is
  unmet THEN it fails with the requirement message before any extension is applied, and the trees it fetched are
  removed again (today's clean-up), so the next `cg init` on an updated cg restores them (decision 5). Extensions
  whose tree is present aren't checked against their requirement.
- **S10** WHEN more than one extension in one run has an unmet requirement THEN the error names each of them, one
  line per extension ([Q7](#q7)).
- **S11** WHEN the requirement is met (S5) or skipped (S6) THEN install, update and `cg init` behave as today.

### Runtime: a pinned manifest whose requirement is unmet

An installed extension whose pinned manifest (#259's Terms) validates but whose requirement is unmet:

- **S12** **counts as verified for ownership and the conflict check** (#259 S6–S11): its verified commands are
  owned and conflict with an incoming extension's, it is not "unverifiable" under #259 S6, and installing or
  updating another extension isn't refused because of it (decision 4).
- **S13** **is declared but not runnable for dispatch:** WHEN the typed words' longest match in the deciding root
  (#259 S15) belongs to it THEN dispatch stops as #259 S17 does (same exit code), with the requirement message instead of
  S17's `cg init` hint; it doesn't fall through to a later root or the `PATH` fallback, and the namespace counts as
  declared for #259 S16 and S33. A shorter entry of another extension never stands in for it (#259 S15).
- **S13b** WHEN its launcher is the workspace's launcher THEN `cg start` warns
  `cg start: extension <name>: needs cg <requirement> or newer; this is cg <core>; <update hint>; starting the agent
  without it` and starts the agent directly, as for any launcher it can't trust today (`cmd/start.go:235-242`).
- **S13c** WHEN `cg extension list` runs THEN the extension is shown with its unmet requirement, in text and
  `--json` ([Q6](#q6)).

### The newer-schema message

- **S14** WHEN a manifest's `manifest` is greater than this cg understands THEN `Validate` fails with
  `unsupported manifest schema version <n> (this cg understands 1 to <max>): it needs a newer cg. <update hint>`
  (decision 6). A `manifest` below 1 keeps today's message, without the hint.

### Docs

- **S15** `docs/extensions.md` documents `requires` in the field table and in a section on requiring a cg version:
  when to set `requires.cg` (a cg fix the extension depends on; a contribution an older cg ignores by policy, such
  as `gitignore` or `launcher`, that the extension relies on), that a field an older cg must not ignore still takes
  a `manifest` version bump (#300's D6), that a v2 manifest already implies cg 2.0, and when the check runs (S7–S13c).
  `EXTENSIONS.md` gets the author-facing line.

### Unchanged

- **U1** A manifest without `requires.cg` installs, updates, dispatches and launches as today.
- **U2** Git-hook trust (`TrustedGitHookScripts`) and the legacy base of a pre-content-id ledger (`LegacyBases`)
  don't check the requirement ([Q5](#q5)).
- **U3** No setting or flag skips the check ([Q8](#q8)); a development build skips it by S6.

## Cross-repo acceptance (outside this PR)

- **X1** mnemcik/cg-extensions-registry `schema/cg-extension.schema.json`: an optional top-level `requires` object,
  `additionalProperties: false`, with one optional property `cg`: a string with the version pattern; allowed with
  `manifest` 1 and 2 (no `if`/`then` ties it to a version). Every manifest on cg-extensions `main` still validates.
  Merged before any extension sets `requires.cg`; not a hold on the cg release ([Q2](#q2)).
- **X2** No extension sets `requires.cg` in this rollout, and the registry descriptions keep their "(cg x+)" text
  ([Q3](#q3)).

## Questions for the owner

Each has a proposed answer; none is decided here.

- <a id="q1"></a>**Q1 — where the PR branches from (decision 9, analysis Options item 5).** The analysis says "its
  own PR from #259's M1 commit (as #301 does)". #259 is on `main` now. *Proposed:* this branch, rebased onto `main`
  (done at `8840343`); the PR goes into `main` and merges before the 2.0.0 release PR (#312), rebased on whichever
  of #300 and #301 merged first (all three touch `internal/extension/manifest.go` and `docs/extensions.md`).
- <a id="q2"></a>**Q2 — the registry schema change as its own PR (decision 9: "with #259's cross-repo step 1").**
  That step merged as mnemcik/cg-extensions-registry#25 without `requires`, and the schema is closed, so a manifest
  that sets `requires.cg` fails cg-extensions CI until the schema accepts it. The registry's README asks every
  registry PR to say `Fixes mnemcik/cg-extensions#NNN`. *Proposed:* an additive registry PR (X1), its own issue in
  mnemcik/cg-extensions that points at this agreement, which covers its content (no separate analysis); it can
  merge any time, before or after cg 2.0.0 (a cg that predates the field ignores it, and no manifest uses it yet).
- <a id="q3"></a>**Q3 — no `requires.cg` and no description change in this rollout (decision 9's last part).**
  Decision 9 says extensions set `requires.cg` where they depend on a fix when they move to v2, and drop the
  "(cg x+)" text. Today every stated minimum is at or below 2.0.0 (`bundle-backup` 1.25, `obsidian` 1.26, `tmux`
  1.32; `shopping` and `live-runs` move to v2, which implies 2.0). Every cg that enforces `requires.cg` is 2.0.0 or
  later, so setting those minimums changes nothing, while for a cg 1.x user the description is the only hint. The
  owner already kept "(cg 2.0+)" for `live-runs` / `shopping` (note on #212, 2026-11-26). *Proposed:* no extension
  sets `requires.cg` now, and every description keeps its text; the first extension that needs a minimum above
  2.0.0 sets it, and drops the text then.
- <a id="q4"></a>**Q4 — how the "distinct result" of decision 4 is built.** Decision 4 says `verifiedManifest`
  "returns a distinct 'requirement unmet' result". On `main` it returns `(dir, manifest, error)` and has six
  callers (seven with #300). *Proposed:* `verifiedManifest` keeps its contract (load and validate; the requirement
  is not an error there), so every ownership and conflict-check caller treats the extension as verified (S12) with
  no change; dispatch and the launcher call a separate `unmetRequirement(m)` on the manifest they got and refuse
  (S13, S13b). The behaviour is exactly decision 4's; only the shape differs.
- <a id="q5"></a>**Q5 — which pinned-manifest readers check the requirement.** Decision 4 names the apply paths and
  `verifiedManifest`. *Proposed:* only the ones that run something from the extension, dispatch (S13) and the
  launcher (S13b), plus `cg extension list` for display (Q6). Git-hook trust and `LegacyBases` don't (U2): git hooks
  were approved when the extension was applied, by a cg that met the requirement, and `LegacyBases` reads an old
  version only for file bases.
- <a id="q6"></a>**Q6 — `cg extension list` shows an unmet requirement.** Not covered by the analysis. *Proposed:*
  text: `  needs cg <requirement> or newer; this is cg <core>` under the extension; `--json`: a
  `requirementUnmet` string (the requirement message), omitted when met. Its commands are listed as verified
  (S12), since `list` reports what dispatch would refuse only through this field.
- <a id="q7"></a>**Q7 — several unmet requirements in one run.** Decision 5 names "the extension". *Proposed:*
  install, update and `cg init` load every manifest of the run, then report each unmet requirement (S10), so one cg
  update fixes the run; nothing is applied either way.
- <a id="q8"></a>**Q8 — no override.** *Proposed:* `--force` (install, `cg init`) doesn't skip the check, and there
  is no flag or variable to skip it; a development build skips it (S6). Reason: the extension is known not to work
  on that cg; the fix is updating cg.

## Out of scope

- A last 1.x release (decision 7, lapsed), requirements on other tools (decision 8, #308), version ranges or
  upper bounds, and updating cg automatically when a requirement is unmet (analysis, "Deferred").
