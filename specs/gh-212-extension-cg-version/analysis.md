---
title: "Analysis — an extension states the cg version it needs (#212)"
created: 2026-11-22
item: "https://github.com/mnemcik/consigliere/issues/212"
size: full
verdict: proceed (decided 2026-11-22)
---

# Analysis — an extension states the cg version it needs (#212)

- **Status:** reviewed (3 rounds, last clean); decided by the owner on 2026-11-22, decisions 1–9 (see [Q→A log](#qa-log)). No code yet.
- **Branch:** `gh-212-extension-cg-version`, from `main` at `61abdfd`.
- **Related in-flight work, read on their branches and not changed here:** #259 (PR #303, branch
  `gh-259-extension-commands` at `decbfc0`), #300 (branch `gh-300-extension-skills` at `021cbd9`), #301 (branch
  `gh-301-extension-hook-filters` at `9038139`).

## Question

An extension can depend on a cg release: a contribution point added later, or a fix. The manifest can't say so,
and cg never checks, so an older cg installs the extension, reports success, and the extension then fails with an
error that doesn't point at the cg version (#212: `shopping` on cg 1.30.1, `cg shop` → `unknown command "shop"`,
because the fix it needed, #210, shipped in 1.30.2). It affects every workspace that runs an older cg than an
extension needs, and every extension author, who can only write the requirement into the registry description.

This is a **"how"** item with a time-sensitive **"whether"** inside it: cg 2.0.0 introduces manifest v2, and the
question is whether a minimum-cg-version field belongs in v2 from the start, and what that means for the 2.0.0
release.

## Current state (evidence)

**Manifest decoding and the version check.**
- `ParseManifest` is a plain `json.Unmarshal` (`internal/extension/manifest.go:111-117`): an unknown key is dropped
  without a word.
- `Validate` refuses a manifest whose `manifest` integer isn't the one this build understands, with
  `unsupported manifest schema version %d (this cg understands %d)` (`manifest.go:131-134`). The message doesn't say
  that a newer cg would understand it, or how to get one.
- There is no field for a required cg version, and no code that compares a manifest with the running cg
  (`manifest.go:40-46` lists every top-level field: `manifest`, `name`, `version`, `description`, `contributes`).

**Every path that applies a manifest goes through one function.** `loadValidManifest` (`cmd/extension.go:435-449`)
is called by `installFrom` (install, `:412-433`), `updateOne` (update, `:766-799`) and
`reinstallMissingExtensions` (`cg init` on a clone, `:358-410`, called at `cmd/init.go:343`), each before
`reapply` writes anything. Runtime readers that trust a pinned manifest call `LoadManifest` + `Validate` directly:
the launcher check in `cg start` (`cmd/start.go:220-227`), and #259's planned `verifiedManifest` helper for command
dispatch (#259 plan, M1).

**The running version** is `cmd.Version`, set by `-ldflags`, `"dev"` otherwise (`cmd/root.go:10-11`). A local
`make build` sets it from `git describe --tags --always --dirty` (`Makefile:2`), so a contributor's binary reports
e.g. `v2.1.0-4-gabc1234-dirty`: valid semver **with a pre-release suffix**, which sorts *below* 2.1.0. A checkout
without tags reports a bare commit hash. `autoupdate.IsReleaseVersion` is only `semver.IsValid` after adding a
leading `v` (`internal/autoupdate/github.go:99-112`), so it doesn't tell such a build from a release.
`golang.org/x/mod/semver` is already a dependency (`go.mod:10`, used in `internal/autoupdate`).

**#259 changes the install, update and `cg init` flows** (agreed plan, M1). Update will "resolve and load every
target's new manifest first, check the whole new set, then apply and save" (S10). `cg init` loads the manifests of
the extensions to re-install and of those present, checks the whole set, then applies (S11). A shared
`verifiedManifest(root, ref)` helper loads and validates the manifest in the pinned tree, for the launcher and for
the commands conflict check. M1 replaces today's loops, which stop at the first failing extension after applying the
earlier ones (`cmd/extension.go` `runExtUpdate`, `cmd/init.go:343-346`). Dispatch closes the `PATH` fallback for a
whole declared namespace (spec Q6, S16).

**Documented policy: an older cg ignores a key it doesn't know.** `docs/extensions.md:257-259` (`gitignore`) and
`:282-285` (`launcher`). The field reference says only that cg refuses a `manifest` version it doesn't understand
(`docs/extensions.md:164`).

**Where the requirement lives today:** free text in registry descriptions. At registry `main` (`37db607`), five
entries carry it: `bundle-backup` `(cg 1.25+)`, `obsidian` `(cg 1.26+)`, `shopping` `(cg 1.30.2+)`, `live-runs`
`(cg 1.30.2+)`, `tmux` `(cg 1.32+, tmux 3.2+)`; `github-digest` carries `(gh 2.48+)`. No code reads it, and a direct
install (`cg extension install <url> --path …`) never sees it. The registry's manifest schema is closed
(`schema/cg-extension.schema.json`: `"additionalProperties": false` at the top level, `"manifest": {"const": 1}`),
so any new top-level field needs a registry schema change too.

**When a workspace runs an older cg** (from the issue, checked against the docs):
- auto-update off (`--no-auto-update`, `CONSIGLIERE_AUTO_UPDATE=0`), or a Homebrew or unknown install, which never
  self-replaces (`docs/auto-update.md:49-56`, `:69`);
- **a major release is pending: never auto-installed** (`docs/auto-update.md:76-88`). After cg 2.0.0 ships, every
  auto-updating workspace stays on 1.x until its user runs `cg update upgrade`;
- the workspace is cloned or pulled on another machine with an older binary. `cg init` then re-installs each
  extension whose tree is missing at the commit `.cg.json` records (`docs/extensions.md:393-399`), with the older
  binary.

**What 2.0.0 already decides about manifest versions** (in-flight work, read, not assumed):
- **#259** (agreed on the issue 2026-11-21, "including Q1–Q10 as written"): manifest **v2** for any manifest with
  `commands`; v1 without `commands` keeps validating; cg 1.x refuses v2 with its existing error (spec Q1, S5).
  Unknown fields stay ignored, including the removed `subcommands` (spec Q8). Cross-repo order: registry schema
  first, cg 2.0.0, then the extensions move to v2 and the registry descriptions say "cg 2.0+" (plan, Cross-repo
  steps 1–5).
- **#300** (D1–D14 agreed): `skills` only in v2, shipped in 2.0.0 (D4, D5). **D6, agreed 2026-11-21:** a field an
  older cg must not ignore gets a manifest version bump (v3, …), only for manifests that use it; harmless fields keep
  "an older cg ignores the key"; no strict decoding. It weighed **(c) a minimum cg version field** and recommended it
  "only if the owner wants a general mechanism" (#300 analysis, "Shipping: manifest version and release").
- **#301** (decided 2026-11-22): hook `matcher`/`if` only in v2, in 2.0.0; decision 2 is the same as #300's D6.
  Its analysis lists the minimum-cg field as an alternative for that decision, with the trade-off that it relies on
  each author raising the minimum and helps only from the release that introduces the check
  (#301 analysis, "Alternatives for decision 2").

So the owner has already chosen how **schema** additions after 2.0.0 stay safe (version bumps). This analysis does
not reopen that. It asks a different question: what covers the dependencies a version bump can't express.

**Prior art** (read 2026-11-22):
- Go's `go` line in `go.mod`: "Before Go 1.21, Go toolchains treated the `go` line as an advisory requirement";
  "Go 1.21 changed the `go` line to be a mandatory requirement"; "The Go toolchain refuses to load a module or
  workspace that declares a minimum required Go version greater than the toolchain's own version." The check was
  also backported: "Go 1.19 releases starting at Go 1.19.13 and Go 1.20 releases starting at Go 1.20.8, refuse to
  load … Go 1.22 or later" (https://go.dev/doc/toolchain). The same lesson as here: a minimum only protects from the
  release that enforces it, and Go closed the gap with point releases of the older lines.
- VS Code: `engines.vscode` is a required manifest field, a minimum such as `^1.0.0`
  (https://code.visualstudio.com/api/references/extension-manifest). Whether VS Code refuses an unsatisfied
  extension isn't stated on that page: not verified.
- npm `engines`: advisory, a warning unless `engine-strict` is set (https://docs.npmjs.com/cli/v10/configuring-npm/package-json;
  read through a summary of the page, not verbatim).

**Recon checked:**
- this repo: `internal/extension` (manifest, registry), `cmd/extension.go`, `cmd/init.go`, `cmd/start.go`,
  `cmd/root.go`, `internal/autoupdate`, `docs/extensions.md`, `docs/auto-update.md`, `CONTRIBUTING.md`, `CLAUDE.md`
  → findings above; no existing version-requirement mechanism.
- #259, #300, #301 analyses, specs and plans on their branches → the manifest v2 decisions and D6 above. None of them
  adds a minimum-cg field.
- `mnemcik/cg-extensions-registry` (`index.json`, `schema/`) → requirement as description text; closed schema.
- `mnemcik/cg-extensions` `shopping/cg-extension.json` → manifest v1, `subcommands`, no requirement field.
- #210 → merged 2026-11-13, the fix `shopping` depends on.
- Installed plugins and skills in this session → nothing relevant to extension version requirements.
- External: Go toolchain, VS Code, npm (above).

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes. Nothing in the manifest or the code expresses or checks a cg version; the observed failure (1.30.1 installing `shopping`) follows from that. | `manifest.go:40-46`, `:111-134`; #210; registry descriptions |
| Is the expected behaviour right? | Yes: refuse at install, naming the needed cg and how to update, instead of failing later. The documented "older cg ignores the key" policy is the reason the failure is silent, and it stays right for harmless keys (#300 D6). | `docs/extensions.md:257-259`, `:282-285` |
| Is it still current? | Yes, and partly overtaken: for **schema** additions after 2.0.0, #300's D6 (version bumps) already makes an older 2.x refuse instead of ignoring. It is not covered for **behaviour** dependencies (a fix such as #210, which is exactly the observed case), nor for harmless-by-policy keys an extension still relies on (`gitignore`, `launcher`). | #300 D6; #301 decision 2 |
| Is it the right problem? | Yes. The need is "the install fails at the right moment with the right message". A field is the means; the message for a manifest version newer than cg understands is part of the same need. | `manifest.go:132-134` |

**Verdict: partly valid.** Real problem: a behaviour or harmless-field dependency on a cg release can't be stated
or checked, and the existing "newer manifest version" refusal doesn't tell the user to update cg. Schema additions
after 2.0.0 are already handled by #300's D6.

## Options

### Recommended — an optional minimum-cg field in manifest v2, enforced from cg 2.0.0

- **What:**
  1. **Field:** an optional top-level `"requires": { "cg": "2.1.0" }`: the lowest cg release the extension works
     with. A minimum only, no ranges and no upper bound. Accepts `MAJOR`, `MAJOR.MINOR` or `MAJOR.MINOR.PATCH`
     (`x/mod/semver` accepts the short forms), so the registry's "1.25+" style maps directly. No leading `v`, no
     pre-release or build suffix: `Validate` refuses anything else. Accepted in v2 and in v1 manifests: a cg that predates it ignores it (today's behaviour), a cg
     that knows it enforces it. `requires` takes only `cg` in this change; other keys inside it follow the same
     "older cg ignores the key" policy.
  2. **Check:** one helper, `CheckRequires(m, running)`, called wherever a manifest is loaded: in
     `loadValidManifest` after `Validate`, so install, update and `cg init` refuse **before anything is applied**
     (inside #259's load-then-check phase, decision 5), and in #259's `verifiedManifest`. Message:
     `extension "shopping" needs cg 2.1.0 or newer; this is cg 2.0.0. Update cg (cg update upgrade, or brew upgrade
     --cask cg), then run this again.`
     - **Running version:** compared by its `MAJOR.MINOR.PATCH` only, ignoring any pre-release or build suffix, so
       a `git describe` build after the 2.1.0 tag (`v2.1.0-4-gabc1234-dirty`) counts as 2.1.0. A version that isn't
       semver at all (`dev`, a bare commit hash) skips the check.
     - **Which extensions:** only those about to be applied: the install target, the update targets, and the
       extensions `cg init` re-installs. A present extension that isn't being applied isn't checked by that run;
       the runtime readers below cover it.
     - **At runtime** (pinned-manifest readers): `verifiedManifest` returns the manifest **with a distinct
       "requirement unmet" result**, not an error. Ownership and the commands conflict check (#259 S6–S11, which
       count only verified commands, and S9's "owns nothing" for an unreadable tree) treat it as **verified**, so
       its commands stay owned and a later install can't claim them. Dispatch treats it as declared but not
       runnable: it refuses with the update hint and never falls through to a `cg-<namespace>` on `PATH` or another
       root (keeping #259's Q6 closure). The launcher check treats it as unverified: it warns with the update hint
       and starts the agent directly, as it does today for any manifest it can't trust (`cmd/start.go:220-227`).
  3. **The newer-schema message:** `Validate`'s refusal of a `manifest` version greater than it understands also
     names the update path (`… needs a newer cg; update cg, then run this again`). D6's future version bumps (v3, …)
     then fail with an actionable message on 2.0.x.
  4. **Docs:** `docs/extensions.md` field reference and `EXTENSIONS.md`: when to set `requires.cg` (a fix you depend
     on; a harmless-by-policy contribution you rely on), and that a must-not-ignore field still takes D6's version
     bump.
  5. **Ships in cg 2.0.0**, its own PR from #259's M1 commit (as #301 does), merged after #303 and before the 2.0.0
     release PR. Cross-repo: the registry manifest schema accepts `requires` (with #259's cross-repo step 1); the
     extensions that already say "(cg x+)" set `requires.cg` when they move (#259's step 4), so their descriptions
     can drop the text. An extension on v2 doesn't need `requires.cg` below 2.0.0: v2 already implies it.
- **Why in v2, now:** a minimum only protects from the release that enforces it (as Go found before 1.21). If 2.0.0
  ships without the check, every 2.0.x ignores a `requires` added later, so a 2.1-era extension that depends on a
  2.1 fix installs silently on 2.0.x: #212 again, one major later. If 2.0.0 enforces it, every 2.x is covered.
  Adding it in 2.0.0 costs one field, one comparison and one message, with no change to anyone's manifest.
- **Relation to #300's D6 (unchanged):** complementary. D6 covers a **field** an older cg must not ignore (a
  version bump, enforced by the schema). `requires.cg` covers what a version bump can't express: a **fix** or
  behaviour change with no new field, and a harmless-by-policy key the extension relies on. It doesn't replace the
  bump: a forgotten minimum would give the silent drop back for a safety-relevant field (#301's argument).
- **Trade-off:** one more field and check in the 2.0.0 window, touching `manifest.go`, which #259, #300 and #301
  also change. It relies on authors setting the minimum for fixes; nothing can detect that automatically.
- **Wrong choice when:** 2.0.0 must not take one more change, or the owner expects no behaviour dependency after
  2.0.0 (every future need would be a new field, which D6 covers).
- **Rabbit holes:** version ranges and upper bounds (not needed: breaking changes bump cg's major and the manifest
  version); requirements on other tools (`tmux 3.2+`, `gh 2.48+` in descriptions: a separate concern, see decision
  8); pre-release ordering of local builds (avoided by comparing the running version's `MAJOR.MINOR.PATCH` only);
  per-contribution minimums; self-updating when a requirement is unmet (below).

### Alternative — the same field, plus a last 1.x release that knows it (runner-up as an addition)

- **What:** everything above, and a final 1.x minor (e.g. 1.33.0) with steps 1–3 for v1 manifests and a better
  message for `"manifest": 2` (`needs cg 2.0 or newer; run cg update upgrade`). The Go backport pattern
  (1.19.13/1.20.8).
- **Trade-off:** 1.x is where the observed failure happened, and it is where most workspaces will sit right after
  2.0.0, because auto-update never installs a major (`docs/auto-update.md:76-88`). The 1.x minor is auto-installed,
  so those workspaces get "update to 2.0" instead of `unsupported manifest schema version 2 (this cg understands 1)`
  when the extensions move to v2. **Cost:** release-please releases from `main`, so the 1.x change must merge and
  release before #303 merges (or a maintenance branch must be set up); it puts a step in front of the 2.0.0 chain.
  Workspaces with auto-update off or on Homebrew don't get it either.
- **Wrong choice when:** #303 is about to merge, or the owner accepts the 1.x message for the few weeks the
  extensions take to move.
- **Rabbit holes:** a release-please maintenance branch for 1.x; double implementation if #259's M1 has reshaped
  `manifest.go` (the 1.x change should land first and #259 rebase on it).
- **Wins instead if:** the owner wants the 2.0.0 transition itself to explain itself on 1.x, and #303 isn't merging
  this week.

### Alternative — defer the field to a 2.x minor

- **What:** keep 2.0.0 as planned; add `requires` in, say, 2.1.0.
- **Trade-off:** 2.0.0 doesn't wait. But 2.0.x ignores the field forever, so any extension that depends on a 2.1+
  fix is unprotected on 2.0.x, and the field itself is a harmless-by-policy key that 2.0.x drops.
- **Wrong choice when:** 2.0.x workspaces are expected to linger (Homebrew, auto-update off); they are the ones the
  field is for.
- **Wins instead if:** 2.0.0 is frozen.

### Alternative — do nothing beyond #300's D6

- **What:** keep the requirement as registry description text; rely on version bumps for new fields.
- **Trade-off:** no new code. But the observed case (a fix, no new field) stays silent, direct installs never see
  the text, and nothing changes the unhelpful 1.x/2.x message for a newer manifest version.
- **Wrong choice when:** extensions keep depending on fixes, as `shopping` and `live-runs` (1.30.2) already do.

Strict decoding of v2 (refuse unknown keys) isn't listed: the owner rejected it twice (#300 D6, #301 decision 2).

**Deferred: update cg automatically when a requirement is unmet** (Go's answer after 1.21 is toolchain switching,
`GOTOOLCHAIN`, https://go.dev/doc/toolchain). cg could offer `cg update upgrade` at the refusal. It only works on
an `install.sh` install: Homebrew and unknown installs never self-replace (`docs/auto-update.md:46-56`), and a
major needs the user's explicit consent (`:76-88`), so it would be an offer, not an automatic switch. It needs the
refusal to exist first. A later addition, not part of this change.

## Hidden constraints

- **Reversibility:** the field is optional and additive; removing it later would mean older manifests carry an
  ignored key. Easy to undo before release, cheap after.
- **Failure mid-operation:** the check runs when manifests are loaded, before anything is applied. After #259's M1,
  update and `cg init` load and check the whole set before applying any of it (S10, S11), so an unmet requirement
  in one extension being applied either refuses the whole run with nothing applied, or drops that extension from
  the set (decision 5). Either way no extension is left half-applied. **Consequence of refusing:** a fresh clone on
  an older cg, of a workspace where someone on a newer cg installed an extension that needs it, gets **no**
  extension restored by `cg init` until cg is updated; the message says which extension and which cg.
- **Data and compliance:** none.
- **Security:** a manifest could claim an absurd minimum (`99.0.0`): the install is refused, nothing runs. The value
  is printed, so `Validate` must refuse non-semver text (no control characters reach the terminal).
- **Performance:** one string comparison per manifest load.
- **Dependencies:** `x/mod/semver`, already in `go.mod`. Registry schema change (closed schema).
- **Operations / platforms:** pure Go string handling; nothing differs on Windows (CONTRIBUTING "Supported
  platforms": no paths, modes, files or shelling out involved).
- **Who else is affected:** extension authors (a new optional field, documented); the registry (schema, and the
  "(cg x+)" text can go once manifests carry the field); #259, #300, #301 (same file, `manifest.go`; sequencing as
  #301's decision 9); every 1.x user when the extensions move to v2 (the runner-up's concern).
- **Already-installed extensions on an older binary:** files an extension wrote are committed in the workspace and
  stay; a missing tree goes through `cg init`, which now refuses with the message; a present tree is re-checked by
  the pinned-manifest readers. No new session-start check is needed.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Does a minimum-cg field belong in manifest v2, enforced from 2.0.0? | Yes: an optional field, checked by 2.0.0, so every later 2.x release is covered. Deferring means 2.0.x ignores it for good. | agreed |
| 2 | Field shape and name | `"requires": { "cg": "<min>" }`, a minimum only (`MAJOR[.MINOR[.PATCH]]`), no ranges or upper bound; `requires` takes only `cg` in this change. *Alternatives:* a flat `"requires-cg"`; npm-style `"engines": {"cg": ">=…"}` with a range parser. | agreed |
| 3 | Accept it in v1 manifests too? | Yes: older cg ignores it as today; a cg that knows it enforces it. | agreed |
| 4 | Where and how is it checked? | One helper, called from `loadValidManifest` (install, update, `cg init`; only extensions being applied) before anything is applied, and from #259's `verifiedManifest`, which returns a distinct "requirement unmet" result. The running version is compared by `MAJOR.MINOR.PATCH`, ignoring pre-release/build suffixes; a non-semver version (`dev`, a hash) skips it. At runtime such an extension stays verified for ownership and the conflict check, stays declared for dispatch (refused with the update hint, no `PATH` fallback), and is unverified for the launcher (warn, start the agent directly). | agreed |
| 5 | `cg extension update` (all) and `cg init` when one extension being applied has an unmet requirement, inside #259's load-then-check flow (S10, S11). Only extensions being applied are checked; present, non-target ones aren't. | Refuse the whole run before anything is applied, naming the extension and the cg it needs: one rule, and the fix is a single step (update cg, run again). Cost: a fresh clone on an older cg restores no extension until cg is updated. *Alternative:* drop that extension from the set (it keeps its installed version; on `cg init` its tree stays missing), apply the rest, exit non-zero; better if partial restores on old machines are common. Unlike a command conflict (a workspace defect, why #259 refuses the whole set), this depends on the local cg, so either answer is defensible. | agreed |
| 6 | The refusal of a newer `manifest` version names the update path | Yes, in 2.0.0, so D6's later bumps (v3, …) fail with an actionable message on 2.0.x. | agreed |
| 7 | A last 1.x release with the field and a "needs cg 2.0" message for v2 manifests? | Owner's call on timing. Recommended **only if** it can merge and release before #303 merges; otherwise skip and accept the 1.x message during the move to v2. | agreed (only if released before #303 is ready) |
| 8 | Requirements on other tools (`tmux 3.2+`, `gh 2.48+`) | Out of scope; a separate issue if wanted. | agreed (#308 filed) |
| 9 | Release and sequencing | In cg 2.0.0: own branch and PR from #259's M1 commit, merged after #303 and before the release PR (as #301). Registry schema accepts `requires` with #259's cross-repo step 1; extensions set `requires.cg` when they move (step 4). | agreed |

**Not a decision:** #300's D6 (agreed) stays as it is. Version bumps remain the rule for a field an older cg must
not ignore; `requires.cg` adds what a bump can't express.

## Review

**Review 1** — `specs/gh-212-extension-cg-version/analysis.md` @ content hash `58b504d` (uncommitted) · 2026-11-22
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: n/a (analysis)
- Findings: 0 blocker, 3 should-fix, 2 nit → fixed: the running version's comparison for local `git describe` builds (pre-release suffix) and the accepted `requires.cg` syntax; decision 5 and the mid-operation constraint restated against #259's load-then-check flow (S10, S11); the runtime meaning of an unmet requirement for #259's dispatch (namespace still declared, no `PATH` fallback) and launcher; D6 moved from the decision table to a note; self-update recorded as a deferred option. owner's: none; rejected: none; dropped in validation: none
- Builder questions: 5 answered (by the fixes above: comparison of the running version, the whole-set behaviour as decision 5, namespace claim, one shared helper, no leading `v`), 0 minor, 0 owner's
- Verified true: code and docs line references; registry descriptions and closed schema; #259 agreement "including Q1–Q10"; #300 D6 and #301 decision 2 text; Go toolchain quotes; no private context; no agreed decision reopened; no Windows impact
- Not verified: #210's merge date (from `gh issue view`, not given to the reviewer); VS Code and npm pages (flagged in the text, not load-bearing); the `shopping` manifest; whether release-please can release 1.x from a maintenance branch without setup

**Review 2** (delta) — same path @ content hash `3bb49ea` (uncommitted), diff against `58b504d` · 2026-11-22
- Reviewers: doc-reviewer (delta; regression check of round 1's five findings)
- Tests: n/a (analysis)
- Findings: 0 blocker, 2 should-fix, 1 nit → fixed: `verifiedManifest` returns a distinct "requirement unmet" result so ownership and the conflict check (#259 S6–S11, S9) still count the extension's commands; decision 5 says only extensions being applied are checked and states the fresh-clone consequence of refusing; the deferred self-update option corrected (majors install with consent; the limit is the install method). owner's: none; rejected: none; dropped in validation: none
- Builder questions: 2 answered (by the fixes above), 0 minor, 0 owner's
- Verified true: round 1's five findings resolved; `Makefile:2`, `IsReleaseVersion` behaviour; S10/S11 and M1 quotes; dispatch consistent with S16/S17/S33 and Q6; launcher behaviour (`cmd/start.go`); no agreed decision reopened
- Not verified: go.dev not re-fetched this round (citation unchanged); unchanged sections not re-reviewed

**Review 3** (delta) — same path @ content hash `c8ef679` (uncommitted), diff against `3bb49ea` · 2026-11-22 · **clean**
- Reviewers: doc-reviewer (delta; regression check of round 2's three findings)
- Tests: n/a (analysis)
- Findings: 0 blocker, 0 should-fix, 0 nit. Considered and not raised: an extension whose tree is still missing after `cg init` owns nothing, so a later install could claim its commands; that is #259's S9 as agreed, not introduced or worsened here. owner's: none; rejected: none
- Builder questions: none left
- Verified true: round 2's findings resolved; consistent with #259 S9–S11, S17, Q6, plan M1, `cmd/start.go:220-227`, `docs/auto-update.md:44-56`, `:76-88`; no private context; no agreed decision reopened
- Not verified: unchanged sections not re-reviewed in this round (reviewed in rounds 1–2); go.dev not re-fetched (unchanged citation, verified in round 1)

## Verdict

**proceed**, to the owner's decisions: the problem is real (partly valid: schema additions are already covered by
#300's D6, behaviour dependencies and the newer-version message aren't), the recommendation was reviewed in three
rounds, the last one clean, and no assumption could flip it. The owner agreed decisions 1–9 on 2026-11-22.
Time-sensitive: decision 1 must be taken before #303 merges, so that the change can land between #303 and the 2.0.0
release PR (decision 9); decision 7 (a last 1.x release) only makes sense before #303 merges.

## Q→A log

- 2026-11-22 — The owner is going through the open decisions one at a time:
  - Decision 1: a minimum-cg field goes into manifest v2, enforced from cg 2.0.0 → **agreed** as recommended.
  - Decision 2: `"requires": { "cg": "<min>" }`, a minimum only, `MAJOR[.MINOR[.PATCH]]`, no `v`, suffixes or
    ranges → **agreed**. Q: should the format be linted? → A: yes, in two places: cg's `Validate` refuses a malformed
    value before anything is applied (covers direct installs), and the registry's `cg-extension.schema.json` gives
    `requires.cg` a pattern (`^(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*)){0,2}$`), which `mnemcik/cg-extensions`' existing
    `validate` workflow already applies to every manifest on each PR. No separate `cg extension validate` command:
    installing from a local path already runs the full validation.
  - Decision 3: `requires.cg` is accepted in v1 manifests too (an older cg ignores it, a cg that knows it enforces
    it; the registry schema allows it in both) → **agreed** as recommended.
  - Decision 4: one helper, called from `loadValidManifest` (install, update, `cg init`; only extensions being
    applied) and from #259's `verifiedManifest` (a distinct "requirement unmet" result); the running version compared
    by `MAJOR.MINOR.PATCH`, a non-semver version skips it; at runtime the extension stays verified for ownership and
    the conflict check, declared for dispatch (refused with the update hint, no `PATH` fallback), unverified for the
    launcher → **agreed** as recommended.
  - Decision 5: an unmet requirement in any extension being applied refuses the whole `cg extension update` or
    `cg init` run before anything is applied, naming the extension and the cg it needs (a fresh clone on an older cg
    restores no extension until cg is updated) → **agreed** as recommended.
  - Decision 6: in 2.0.0, the refusal of a `manifest` version newer than cg understands names the update path
    (`cg update upgrade`, or `brew upgrade --cask cg`) → **agreed** as recommended.
  - Decision 7: a last 1.x minor (1.33.0) with `requires.cg` for v1 manifests and a "needs cg 2.0; run
    `cg update upgrade`" message for v2 manifests, as a `feat:` PR from `main`, **only if it is released before #303
    is ready to merge**; it must not delay #303. If it isn't ready in time, it's dropped → **agreed** (option C).
  - Decision 8: requirements on other tools (`tmux`, `gh`) are out of scope; `requires` leaves room for them later
    → **agreed**. Filed on the owner's request as #308 (2026-11-22).
  - Decision 9: in cg 2.0.0; registry schema accepts `requires` (with the format pattern) alongside #259's cross-repo
    step 1; #212's own branch and PR from #259's M1 commit, merged after #303 and before the 2.0.0 release PR (a
    1.33.0 under decision 7 comes before #303); extensions set `requires.cg` where they depend on a fix when they
    move to v2, and drop the "(cg x+)" description text → **agreed** as recommended.
