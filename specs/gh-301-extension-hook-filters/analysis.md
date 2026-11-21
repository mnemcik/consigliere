---
title: "Analysis — extension hooks: filters and failure behaviour (#301)"
created: 2026-11-21
item: "https://github.com/mnemcik/consigliere/issues/301"
size: full
verdict: needs clarification
---

# Analysis — extension hooks: filters and failure behaviour (#301)

- **Item:** https://github.com/mnemcik/consigliere/issues/301
- **Size:** full (several decisions; touches the manifest schema and its compatibility story, shared with #259)
- **Status:** analysis; awaiting the owner's decisions. No spec or code yet.
- **Evidence base:** consigliere `origin/main` at `61abdfd` (2026-11-21); #259's branch `gh-259-extension-commands`
  at `e54d3b5`; mnemcik/cg-extensions at `87c51e5`; mnemcik/cg-extensions-registry at `37db607`; the `pr-workflow`
  plugin 0.2.1 from mnemcik/claude-marketplace; Claude Code hooks reference https://code.claude.com/docs/en/hooks
  and settings reference https://code.claude.com/docs/en/settings (read 2026-11-21; the pages show no date; the
  local Claude Code is 2.1.330).

## Question

A hook contribution is `{ event, wrapper, command }`. cg registers the wrapper for `event` with an empty matcher, so
it runs on every occurrence of that event. It can't carry Claude Code's filters (`matcher`, `if`) or its failure
behaviour (`onFailure`). Extension authors therefore filter inside the script on every call, and can't say whether
a failing hook lets the call go ahead or blocks it. This affects every extension that contributes a hook, and it
blocks the `pr-workflow` extension (mnemcik/cg-extensions#45), which has to replace a plugin hook that already uses
`matcher` and `if`.

This is a **"how"** item: the gap is real (below) and an extension on the critical path needs it closed. The
questions are which fields, what shape, which cg release, and what an older cg does with a manifest that uses them.

## Current state (evidence)

**Manifest.** `HookContribution` has `Event`, `Wrapper` and `Command` only (`internal/extension/manifest.go:81-85`).
`Validate` checks that the three are non-empty and that the wrapper is inside the extension
(`manifest.go:177-184`). It doesn't check the event name. `command` is required, but cg doesn't read it: the
wrapper is what gets registered (`apply.go:475-487`). `command` documents what the wrapper `exec`s
(`docs/extensions.md:186-190`).

**Parsing.** `ParseManifest` uses `json.Unmarshal` into the struct (`manifest.go:111-117`), so an unknown field is
dropped without a word. `Validate` refuses an unknown **schema version** up front (`manifest.go:132-134`, comment at
`:128-130`: "so a future v2 manifest fails with a clear message rather than silently losing fields").

**Documented policy for new manifest keys: an older cg ignores them.** `gitignore` and `launcher` were added to
manifest 1 that way, and the docs say so for each: "A cg older than this contribution point ignores the key"
(`docs/extensions.md:257-259`, `:282-285`); the registry schema says the same of `launcher` ("older cg ignores it",
`schema/cg-extension.schema.json:109`). For a launcher that is harmless (cg keeps its older behaviour). For a hook
filter or a `block` failure mode it isn't: the hook runs unfiltered, or fails open. No manifest in
mnemcik/cg-extensions (`87c51e5`, 11 manifests, all `"manifest": 1`) carries a key cg doesn't know today.

**Registration.** `registerHook` removes every earlier registration of the same wrapper, **in any event**, and then
appends one matcher group with `"matcher": ""` and a single `{type: command, command: <pinned path>}` handler
(`internal/extension/settings.go:49-69`). `settings.json` is read and written as a generic map, so fields cg doesn't
manage survive (`settings.go:10-16`). Removal is by wrapper path across all events (`settings.go:73-87`), and the
ledger records `{event, wrapper}` per hook (`ledger.go:48-53`, `apply.go:231`).

Two consequences:

- **No filters.** Every extension hook runs on every occurrence of its event. The `1password` extension's
  PostToolUse hook says so in its header: "cg registers hooks with an empty matcher, so this runs after every tool
  call and filters on `tool_name` itself" (mnemcik/cg-extensions `1password/hooks/ssh-auth-hint.sh`).
- **One wrapper, one registration.** Registering a wrapper drops its earlier registration. Two manifest entries that
  share a wrapper leave only the last one registered, while the ledger records both. Nothing in `Validate` refuses
  this. The same `1password` extension ships two byte-identical files to serve PostToolUse and PostToolUseFailure:
  "cg registers each wrapper under exactly one event and drops any earlier registration of the same file, so one
  file cannot serve both events" (same file, header). This matters here: Claude Code's `if` holds exactly one rule
  (below), so a hook that needs two rules needs two handlers.

**cg's own hooks do use matchers.** The framework's `settings.json` template registers `startup`, `Write|Edit|NotebookEdit`
and `Bash` matchers (`templates/workspace/.claude/settings.json:5,21,37`); only extension hooks lack them.

**Extensions with hooks today** (mnemcik/cg-extensions `87c51e5`), all on manifest 1:

| Extension | Event | What it needs |
|---|---|---|
| `1password` | PostToolUse, PostToolUseFailure | `Bash` only; one wrapper for both events |
| `bundle-backup` | SessionStart | none |
| `gh-account-resolver` | SessionStart | none |

**The hook #45 must replace** (`pr-workflow` plugin 0.2.1, `plugins/pr-workflow/hooks/hooks.json`): PreToolUse with
`"matcher": "Bash"` and a handler `"if": "Bash(gh *)"`; SessionStart with `"matcher": "compact|clear"`. Its script
fails open on any error (`trap 'exit 0' ERR`, `pr-guard.sh`). Ported as an extension hook today, it would run on
every tool call and on every SessionStart, startup and resume included.

**Claude Code** (hooks reference, read 2026-11-21):

- Three levels: event → matcher group (`matcher`, `hooks`) → handler. Handler fields common to all types: `type`,
  `if`, `timeout`, `statusMessage`, `once`. Command handlers add `command`, `args`, `async`, `asyncRewake`, `shell`,
  `onFailure`.
- `matcher`: `"*"`, `""` or omitted match all; letters, digits, `_`, `-`, spaces, `,` and `|` form an exact list;
  anything else is an unanchored JavaScript regex. Supported on tool events and many others (SessionStart, Setup,
  SessionEnd, Notification, SubagentStart/Stop, PreCompact, …). "If you add a `matcher` field to an event without
  matcher support, it is silently ignored."
- `if`: "Permission rule syntax to filter when this hook runs, such as `"Bash(git *)"` … Only evaluated on tool
  events: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, and `PermissionDenied`. On other
  events, a hook with `if` set never runs." "The `if` field holds exactly one permission rule. There is no `&&`,
  `||`, or list syntax … define a separate hook handler for each." The filter is "best-effort"; when the Bash input
  can't be split, the hook runs regardless.
- `onFailure`: `"continue"` or `"block"`, "Requires Claude Code v2.1.295 or later". Default: "On most events, when a
  hook fails or times out, Claude Code still carries out the action." `"block"` makes a failure act like exit 2. No
  effect on Stop, SubagentStop, TaskCompleted, TeammateIdle, or async hooks. Failures: can't start, exit code other
  than 0 or 2, timeout, invalid output.
- **Windows without Git Bash:** a shell-form command hook runs in PowerShell; "a shell-form `PreToolUse` or
  `PermissionRequest` command hook that fails with `onFailure` unset can instead block the tool call", and "any
  exit code other than 0 or 2 from that hook counts as a failure". The documented fix for "the call should go ahead
  when the hook fails" is `"onFailure": "continue"`.
- **Not documented:** what a Claude Code older than 2.1.295 does with an `onFailure` field, or any unknown handler
  field; nor the version that introduced `if` (not found in the hooks reference or the changelog's recent entries).
  The settings page distinguishes a **Settings Error** ("invalid JSON or a value the schema rejects": at the start of
  an interactive session a dialog lets the user continue without the broken settings; under `-p` the broken file
  is skipped) from a **Settings Warning** ("only individual entries fail, such as a malformed permission rule or an
  unknown hook event name. Claude Code skips those values and keeps the rest of the file in effect"). It also says
  "The schema can lag behind the newest CLI releases, so a validation warning on a recently documented key doesn't
  mean your configuration is invalid."
- **The published settings schema** (https://www.schemastore.org/claude-code-settings.json, read 2026-11-21) closes
  the command-hook object (`"additionalProperties": false`) and lists `if` but not `onFailure`. Which category an
  unknown handler key falls into on an older Claude Code (whole file lost, entry skipped, or key ignored) is
  **unverified**.

**Registry.** The manifest JSON Schema has `"additionalProperties": false` on hook entries with exactly `event`,
`wrapper`, `command` (mnemcik/cg-extensions-registry `schema/cg-extension.schema.json`). A manifest with new hook
fields fails registry validation until the schema changes.

**#259 (decided, specified, not agreed or implemented).** Its spec proposes manifest **v2** for any manifest with
`commands`; v1 manifests without `subcommands` or `commands` keep validating; a cg 1.x refuses v2 with its existing
error (spec Q1, S6). Its M1 changes `manifest.go` (`SchemaVersion`, `Contributions`, `Validate`, and a raw decode
of `contributes` to find a `subcommands` key), `ledger.go` (`Commands`) and `apply.go` (records commands near
`:240`); it is planned for cg 2.0.0 (plan `M1`, cross-repo steps).

**#300** (skills and subagents from extensions) adds new contribution types to the same manifest and faces the same
question: an older cg drops an unknown contribution type silently. Its analysis (branch `gh-300-extension-skills`
at `9e48467`, "Shipping: manifest version and release", D4–D6) recommends: `skills` only in the manifest version
the shipping release introduces (v2 in 2.0.0, whichever of #259, #300 and #301 introduces it), and for later fields
**(b) a manifest version bump per release that adds a field an older cg must not ignore**, keeping the documented
"older cg ignores the key" policy for harmless fields; (a) strict decoding and (c) a minimum-cg field are its
alternatives.

**Observed this session (2026-11-21):** the `pr-workflow` plugin hook blocked three read-only commands
(`gh issue view` twice, a `git show` in a registry clone) on their first call per repo. That over-firing is the
plugin's own choice of what to remind on, not a missing filter: the plugin already uses `matcher` and `if`.
Narrowing it to PR actions is #45's design, which this issue enables (`if` rules such as `Bash(gh pr *)`) but
doesn't decide.

**Recon checked:**

- consigliere `internal/extension/` (manifest, settings, apply, ledger), `docs/extensions.md`,
  `docs/cg-subcommands.md`, the workspace template's `settings.json`: as above.
- #259's analysis, spec and plan on its branch: manifest v2 proposal, file overlap.
- #300, #266 (Claude Code-specific parts of cg into an opt-in extension; the hook contribution writes
  `.claude/settings.json`, so it is Claude Code-specific today).
- mnemcik/cg-extensions manifests and hook scripts: three extensions with hooks, the self-filter and two-file
  workaround in `1password`.
- mnemcik/cg-extensions-registry schema: hook entries closed to new fields.
- `pr-workflow` plugin 0.2.1 hooks: `matcher` + `if` in use.
- Claude Code hooks and settings references: fields, semantics, Windows behaviour; unknown-field behaviour not
  documented.
- Other agent tools' hook formats: not checked; cg contributes hooks to Claude Code only.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes. Extension hooks register with an empty matcher and no handler fields; a wrapper can be registered only once. | `settings.go:49-69`; `1password` hook header |
| Is the expected behaviour right? | Yes for filters: Claude Code's own fields are the documented way to filter. For failure: partly. On macOS and Linux the default already lets the call go ahead. The blocking risk the issue names is Windows without Git Bash, a setup cg already doesn't support for its hooks ("On Windows, cg's hooks and status line need Git for Windows; `cg init` and `cg sync` warn when Git Bash isn't found", `CONTRIBUTING.md:71`, also `:78`). The real gap is the opposite: a guard hook can't opt into `block`. | hooks reference, `onFailure` and Windows sections; `CONTRIBUTING.md:71,78` |
| Is it still current? | Yes. Not fixed on `main` or on #259's branch; no duplicate issue. | `61abdfd`, `e54d3b5` |
| Is it the right problem? | Yes, plus one cause the issue doesn't name: the one-registration-per-wrapper rule, without which multi-rule `if` filters and one wrapper on two events stay impossible. | `settings.go:59`; `if` holds one rule |

**Verdict:** valid, with the failure part restated: "a hook can't choose `continue` or `block`", and the
one-registration rule added to the scope.

## Options

### Recommended — pass Claude Code's fields through, in manifest v2, shipped in cg 2.0.0

- **What:**
  1. Each hook entry gains optional `matcher`, `if` and `onFailure`, with Claude Code's names and meaning. cg writes
     `matcher` on the matcher group and `if`/`onFailure` on the handler. The docs link to Claude Code's reference
     instead of restating it.
  2. The fields need `"manifest": 2`; a v1 manifest that uses them is refused with a message asking for 2, as #259's
     S6 does for `commands`. A v1 manifest without them keeps validating.
  3. A wrapper may appear in several entries (several events, or several `if` rules). Apply unregisters the wrapper
     once, then registers every entry for it; the ledger keeps one `{event, wrapper}` per entry. Two entries with the
     same event, matcher, `if` and wrapper are refused as duplicates. Two different wrappers that install to the
     same path (same base name) are refused, as git-hooks already are (`manifest.go:217-233`).
  4. Validation: `onFailure` must be `continue` or `block`; `if` is refused on any event other than the five tool
     events (Claude Code never runs such a hook, so it would be dead code installed silently); `matcher` is not
     checked against the event (Claude Code's list of events with matcher support changes; a wrong one is silently
     ignored, so the hook runs unfiltered, which a script that also checks its input tolerates).
  5. Later fields (decision 2, the same decision as #300's D6): a release that adds a field an older cg must not
     ignore bumps the manifest version (v3, …), and only manifests that use the field move to it; harmless fields
     keep the documented "an older cg ignores the key" policy (`docs/extensions.md:257-259`, `:282-285`, registry
     schema `:109`). For #301 itself this means: the three fields are accepted only in v2 (item 2), so nothing in
     2.0.0 has to change for it. *Alternative:* strict decoding of v2 (an unknown key is refused), which reverses
     the documented policy for v2.
  6. Settings shape: one matcher group per manifest entry (as today: one group per registration); an omitted
     `matcher` is written as `""` (as today); `if` without `matcher` is allowed (Claude Code evaluates it per tool
     call).
  7. Registry schema: hook entries accept the three fields for `manifest: 2`, alongside #259's X1 change.
  8. `cg extension list` doesn't show filters; the ledger stays as it is.
- **Trade-off:** small code (mostly `Validate`, `registerHook`, `applyHook`) and no new concepts: extension authors
  write what they'd write in a plugin's `hooks.json`. The cost is coupling: the manifest takes Claude Code's
  vocabulary, and cg 2.0.0 waits for this change too.
- **Wrong choice when:** hooks must work for a second agent tool soon (#266's direction). Then an agent-neutral
  filter vocabulary would be worth its translation layer. Today cg writes hooks only to `.claude/settings.json`,
  and the event names are Claude Code's already.
- **Rabbit holes:** validating matcher regexes or permission-rule syntax (Claude Code owns both; skip it); passing
  every handler field through (`timeout`, `async`, `shell`, `args`, `statusMessage`) "while at it": none is asked for,
  and each adds a validation and docs line. Add them when an extension needs one.

### Alternative — same fields, shipped later (cg 2.1)

- **What:** as above, but after 2.0.0, in a minor release.
- **Trade-off:** 2.0.0 doesn't wait. But a 2.0.x cg accepts a v2 manifest whose hook carries `matcher` or `onFailure`
  and drops them: the hook runs unfiltered and its failure mode reverts to the default, with no message. Avoiding
  that needs a manifest v3 for the fields (decision 2's recommendation), or strict v2 decoding in 2.0.0.
- **Wrong choice when:** #45 needs the fields in the same release as #259's commands, which the chain says it does
  (#45, #46, #49, #50 are blocked by #259, #300 and #301 "via the cg release").
- **Rabbit holes:** a v3 bump for three optional fields.
- **Wins instead if:** #259 must release before #301 is implemented. Then 2.1 needs v3 for the fields if 2.0.0
  already accepts v2 (#259's Q1 = v2, or #300 brings v2 in 2.0.0), or strict v2 decoding in 2.0.0; otherwise #301
  introduces v2 in 2.1 and needs no v3.

### Alternatives for decision 2 — strict v2 decoding, or a minimum cg version in the manifest

- **Strict v2 decoding:** an unknown key in a v2 manifest is refused, naming the key and asking for a newer cg. It
  needs no judgement per field: a field added later can't be dropped by an older 2.x, whoever forgets what. But
  every harmless addition then makes older 2.x refuse the manifest, and it reverses the documented policy for v2.
  #259's M1 already decodes `contributes` raw, so the code is small. **Wins instead if** the owner prefers a rule
  that can't be forgotten over keeping harmless additions installable.

**Minimum cg version:**

- **What:** instead of a version bump, a manifest may declare the cg it needs (for example
  `"requires": {"cg": ">=2.1"}`); a cg that is too old refuses it with a clear message. Unknown keys stay ignored,
  as documented today. It helps only from the release that introduces the check: a cg before it ignores the field.
- **Trade-off:** harmless additions (a cosmetic field, a launcher-like key) keep installing on an older cg, and new
  fields can ship in minor releases with no schema bump. But it relies on each extension author remembering to raise
  the minimum when they use a field that matters; forgetting gives back the silent drop. A registry check could
  enforce it for indexed extensions, not for extensions installed from a git URL.
- **Wrong choice when:** the fields that will be added are mostly safety-relevant (filters, failure modes, skills
  and agents that #300 adds), where a forgotten minimum is worse than a refused harmless key.
- **Rabbit holes:** a version-range syntax and parser; comparing pre-release versions.
- **Wins instead if:** the owner expects many harmless additive fields and wants older 2.x cgs to keep installing
  newer extensions.

### Alternative — agent-neutral filter vocabulary

- **What:** cg-defined fields (for example `tools: ["Bash"]`, `commands: ["gh *"]`, `failure: "ignore"|"block"`)
  that cg translates into each agent tool's format.
- **Trade-off:** ready for #266, but cg invents and maintains a mapping for one agent tool, and authors read two
  vocabularies.
- **Wrong choice when:** only Claude Code receives hooks, as today.
- **Wins instead if:** #266 is scheduled before the `pr-workflow` extension ships, or a second agent tool gets hook
  contributions.

**Do nothing** isn't credible: #45 would ship a hook that runs on every tool call and every SessionStart, and
hooks that need two events keep shipping duplicate files.

## Hidden constraints

- **Older cg (1.x).** With the fields gated on manifest 2, a 1.x cg refuses the manifest ("unsupported manifest
  schema version"), so it never installs a hook without its filters. A v1 manifest that carries the fields is
  refused by 2.x and by the registry schema, so an author testing on 2.x, or the registry, catches it. One residual
  case remains: such a v1 manifest installed by a 1.x cg straight from a git URL still loses the fields silently (and
  likewise a 2.0 cg reading a v1 manifest that uses a later field). It's unlikely and doesn't change the recommendation.
- **Older Claude Code.** `onFailure` needs Claude Code 2.1.295 or later; the minimum for `if` isn't stated (the
  `pr-workflow` plugin already relies on `if`). What an older version does with an unknown handler key is
  **unverified**, with three possible outcomes: the key is ignored (a `block` hook fails open), the entry is skipped
  (the hook doesn't run), or the whole `settings.json` is treated as a Settings Error and the session runs without
  it, the framework's own hooks and permissions included. The published schema doesn't settle it: it omits
  `onFailure`, which the current Claude Code (2.1.330) accepts, so it isn't what the CLI validates against. The same
  risk applies to `if` on a Claude Code older than the version that introduced it. **Checking it on a Claude Code
  older than 2.1.295 is a prerequisite for decision 5** (whether `onFailure` ships, and `if` likewise): if a key the
  version doesn't know makes it lose the whole file, the choice is to leave the field out of this change, or ship it
  with the minimum Claude Code documented and accept that an older one loses the workspace's settings. Since cg writes
  the fields only when an extension declares them, only workspaces with such an extension are exposed. cg can't
  check the Claude Code version at install.
- **Defaults on update.** Applying an update rewrites the extension's registrations (unregister, then register), so a
  changed filter takes effect and a user's hand edit of an extension hook's matcher in `settings.json` is
  overwritten, as today.
- **Failure mid-operation.** Several registrations per wrapper write `settings.json` once per apply; the existing
  snapshot and rollback (`apply.go` snapshot) cover it. The register-all step should build the whole hooks map and
  save once, so a failure can't leave half the entries.
- **Security.** `if` and `matcher` only narrow when a hook runs; they grant nothing. A `block` failure mode makes a
  broken guard hook stop tool calls, which is the extension author's explicit choice.
- **Windows.** Without Git Bash, a bash wrapper on PreToolUse/PermissionRequest blocks every matching call unless
  `onFailure` is `continue`. cg already requires Git for Windows for its hooks and warns when it's missing
  (`CONTRIBUTING.md:71,78`), so this is an unsupported setup, not a reason on its own to write `continue` by
  default (decision 4).
- **Performance.** Filters cut process spawns: today an extension PreToolUse/PostToolUse hook starts bash on every
  tool call.
- **Who else is affected.** The three existing extensions with hooks don't have to change (v1 without the fields
  stays valid). `1password` can drop its duplicate file once on v2. The registry schema changes.

### Overlap with #259

| File | #259 (M1) | #301 |
|---|---|---|
| `internal/extension/manifest.go` | `SchemaVersion` 2, v1 still accepted without `commands`; `Contributions.Commands`; `Validate`; raw decode of `contributes` | `HookContribution` fields; `Validate` for hooks and the v2 gate; strict decoding, only if decision 2 picks it, would reuse #259's raw decode |
| `internal/extension/apply.go` | records commands (`:240`) | `applyHook` and the hook loop (`:226-231`): unregister once, register all |
| `internal/extension/ledger.go` | `Commands` replaces `Subcommands` | probably unchanged (one `LedgerHook` per entry already fits); changes only if `cg extension list` shows filters |
| `internal/extension/settings.go` | — | `registerHook` takes the filter fields and a set of entries |
| `docs/extensions.md`, registry schema | v2, `commands` | hook fields, v2 gate |

The `manifest.go` edits touch the same functions (`Validate`, the version check), so #301 should start from #259's
M1 commit, not from `main`.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| 1 | Which release carries the fields? | cg 2.0.0, inside manifest v2, so the chain's extensions need one cg release. Implemented after #259's M1 (decision 9). | open |
| 2 | How do fields added after 2.0.0 avoid being dropped by an older 2.x cg? **The same decision as #300's D6**: one answer for both. | As #300 recommends: a manifest version bump per release that adds a field an older cg must not ignore; harmless fields keep "an older cg ignores the key". *Alternatives:* strict v2 decoding (refuse unknown keys; reverses the documented policy for v2); a minimum-cg field. Doesn't affect #301 itself if it ships in 2.0.0 with v2. | open (shared with #300 D6) |
| 3 | Do the new fields need `"manifest": 2`? Depends on #259's Q1, which isn't agreed. | Yes, whatever #259's Q1 decides: if Q1 keeps v1 for `commands`, #301 (or #300, as its D4 proposes) introduces v2 in 2.0.0, so a 1.x cg, already released, refuses manifests that use the fields instead of dropping them. The only alternative to a version is to accept the silent drop on 1.x and document the minimum cg. | open |
| 4 | When an entry omits `onFailure`, what does cg write? | Nothing (Claude Code's default). Windows without Git Bash is already unsupported for cg's hooks. *Alternative:* write `continue` by default (more exposure to the older-Claude-Code risk in decision 5, since every extension hook would carry the key). | open |
| 5 | Which fields? | `matcher`, `if`, `onFailure` only; others when an extension needs them. **Prerequisite:** check what a Claude Code older than 2.1.295 does with `onFailure` (and an older one with `if`) in `settings.json`: key ignored, entry skipped, or whole file lost. The `if` check is best-effort until the version that introduced it is found; the `pr-workflow` plugin already writes `if`, the same exposure today. If the key is ignored or the entry skipped, ship it: a `block` guard then fails open there, as without the field. If the whole file is lost: leave the field out, or ship it with the minimum Claude Code documented. | open: needs the check |
| 6 | Several entries per wrapper (events, `if` rules)? | Yes, with duplicate and same-install-path refusals. *Alternative:* `if` as a list that cg expands; doesn't solve one wrapper on two events. | open |
| 7 | Validation | `onFailure` ∈ {`continue`, `block`}; `if` only on the five tool events; matcher not checked. | open |
| 8 | Filter vocabulary: Claude Code's names, or agent-neutral? | Claude Code's, until #266 or a second agent tool gets hooks. | open |
| 9 | Where is it implemented? | Its own branch from #259's M1 commit, its own PR, merged before the 2.0.0 release PR. | open |

Out of scope, noted: `command` is required but never read by cg; it stays as it is.

## Review

**Review 1** — `analysis.md` @ content hash `0edfe76` (commit `1a3e57c`) · 2026-11-21
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules, public-repo check)
- Tests: not applicable (analysis)
- Findings: 0 blocker, 4 should-fix, 1 nit → fixed: Windows-without-Git-Bash is already unsupported (`CONTRIBUTING.md:71,78`); older Claude Code's worst case (whole `settings.json` lost) named, checking it made a prerequisite; decision 2 named as a reversal of the documented "older cg ignores the key" policy, shared with #300; decision 3's fallback spelled out for both outcomes of #259's Q1; line numbers. Owner's: none. Rejected: none. Dropped in validation: none
- Builder questions: 6; 2 answered by the fixes (v1 vs v2 refusal; the gate without v2); 3 minor, my call (one matcher group per entry; `if` without `matcher` allowed, empty matcher written as `""`; `cg extension list` unchanged); 1 owner's (decision 4's default, after the older-Claude-Code check)
- Missed option added: a manifest-level minimum cg version (alternative for decision 2)
- Verified true: the code, registry, plugin and #259 claims; the Claude Code hooks-reference claims; public-repo check clean
- Not verified: the session observation of the plugin hook's blocks (no source in the snapshot); the hooks page past its first ~100k characters; older Claude Code behaviour

**Review 2** (delta) — `analysis.md` @ content hash `721fdbf` (uncommitted, on `1a3e57c`) · 2026-11-21
- Reviewers: doc-reviewer (regression check of round 1, delta)
- Findings: 0 blocker, 3 should-fix, 2 nit → fixed: decision 3 now says nothing protects a released 1.x without v2; the older-Claude-Code prerequisite moved to decision 5 and covers `if` too; decision 2 states the "new fields only in v2" rule #300 must follow; the schema inference softened; a wrong decision cross-reference. Owner's: none. Rejected: none
- Builder questions: 2 owner's (if an older Claude Code loses the whole file: drop `onFailure` or ship with a documented minimum; #300's types on v2, now part of decision 2)
- Verified true: round-1 findings 1, 2, 3 and 5 resolved; the new citations (CONTRIBUTING, docs, registry, settings page, schema)
- Not verified: older Claude Code behaviour; the version that introduced `if`

**Review 3** (delta) — `analysis.md` @ content hash `9d04a47` (uncommitted, on `1a3e57c`) · 2026-11-21 · **clean**
- Reviewers: doc-reviewer (regression check of round 2, delta)
- Findings: 0 blocker, 0 should-fix, 2 nit → fixed after the round (text only): the 1.x residual case (a v1 manifest with the fields installed from a git URL); the `if` check is best-effort while its introducing version is unknown
- Builder questions: 1 minor, my call: if an older Claude Code skips the entry rather than losing the file, `onFailure` still ships (the guard fails open there, as without the field)
- Verified true: round-2 findings 1–5 resolved; no contradiction between the decisions table, the recommendation and the hidden constraints; no private context
- Not verified: older Claude Code behaviour with unknown handler keys; the version that introduced `if`

**Review 4** (delta) — `analysis.md` @ content hash `f59c630` (uncommitted, on `c2c336f`) · 2026-11-21 · **clean**
- Why: after round 3, #300's analysis (branch `gh-300-extension-skills` at `9e48467`) turned out to answer decision 2's question differently (its D6: a version bump per release, not strict decoding). Decision 2 reframed as the same decision as #300's D6, recommending its option (b); decision 3, Recommended item 5, the shipped-later alternative, the overlap table and the verdict adjusted; round 3's nits applied
- Reviewers: doc-reviewer (delta, with #300's analysis as a source)
- Findings: 0 blocker, 0 should-fix, 1 nit → fixed after the round (text only): the shipped-later alternative needs v3 only if 2.0.0 already accepts v2
- Builder questions: none
- Verified true: the claims about #300's analysis (D4–D6); decisions 2 and 3 consistent with each other, the recommendation, the hidden constraints and #300's D4–D6; no leftover text recommending strict decoding; round-3 nits reflected; no private context
- Not verified: older Claude Code behaviour (still a prerequisite for decision 5)

## Verdict

**Needs clarification.** The recommendation stands, but three things force "needs clarification":
- decision 5 waits on a check of what an older Claude Code does with `onFailure` (and `if`) in `settings.json`;
- decisions 1–3 depend on #259's Q1 (manifest v2), which isn't agreed;
- decision 2 is shared with #300's D6 and has to be decided once for both.

## Q→A log

- 2026-11-21: analysis written and reviewed (4 rounds; rounds 3 and 4 clean; round 4 after aligning decision 2 with #300's D6); the open decisions go to the owner.
