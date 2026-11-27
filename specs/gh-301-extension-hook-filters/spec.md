# Spec — extension hooks: `matcher` and `if` (#301)

Implements the decisions in [analysis.md](analysis.md#decision) (1–9). Each scenario below is one behaviour and maps
to at least one test; [plan.md](plan.md) says how it is delivered. Q1–Q6 at the end are gaps the decisions don't
settle, or places where `main` has moved since they were taken; each has a proposed answer for the owner's
agreement.

Written against `main` at `8840343`, which includes #259 (`10d241c`: manifest v2, `commands`, the pinned-manifest
helper). Checked against #300's agreed spec (`gh-300-extension-skills` at `0f5ba88`): #300 adds `skills` to the same
manifest and leaves hooks as they are (its U4); the two touch the same files (`manifest.go`, `apply.go`, `ledger.go`,
`docs/extensions.md`, the registry schema), not the same behaviour.

## Terms

- **Hook entry:** one item of `contributes.hooks`: `{ event, wrapper, command, matcher?, if? }`.
- **Filter fields:** `matcher` and `if`. Their names and meaning are Claude Code's
  ([hooks reference](https://code.claude.com/docs/en/hooks)); cg passes them through (decision 8). `onFailure` is not
  one of them (decision 5).
- **Set:** a filter field is set when its value is a non-empty string. An omitted field and `""` are the same (Q4).
- **`if` events:** the hook events on which Claude Code evaluates `if`: `PreToolUse`, `PostToolUse`,
  `PostToolUseFailure`, `PermissionRequest`, `PermissionDenied` (decision 7), and `PreSkillShellCommand` if Q3 is
  agreed. Compared exactly, case included.
- **Same wrapper:** two entries name the same wrapper when their `wrapper` paths are equal after cleaning
  (`hooks/a.sh` and `./hooks/a.sh` are one file).
- **Install path:** where a wrapper is copied in the workspace: `.claude/hooks/<extension>-<base name>`, as today.
- **Registration:** one matcher group cg writes in `.claude/settings.json` for one hook entry.

## Manifest shape

```json
"manifest": 2,
"contributes": {
  "hooks": [
    { "event": "PreToolUse",   "matcher": "Bash", "if": "Bash(gh pr *)",
      "wrapper": "hooks/pr-guard.sh", "command": "cg-pr guard" },
    { "event": "PreToolUse",   "matcher": "Bash", "if": "Bash(gh api *)",
      "wrapper": "hooks/pr-guard.sh", "command": "cg-pr guard" },
    { "event": "SessionStart", "matcher": "compact|clear",
      "wrapper": "hooks/pr-reminder.sh", "command": "cg-pr remind" }
  ]
}
```

`matcher` and `if` are optional strings. Claude Code's `if` holds exactly one permission rule, so a hook that needs
two rules lists two entries with the same wrapper (decision 6).

## Scenarios

### Manifest

H5–H7 apply to every manifest version: decision 6 has no version limit, and none of the three needs a new field. No
manifest on mnemcik/cg-extensions `main` repeats a wrapper or a wrapper's base name (checked 2026-11-27), so none
is refused by H6 or H7.

- **H1** WHEN a `"manifest": 2` manifest has hook entries with `matcher`, `if`, both or neither THEN it validates.
- **H2** WHEN a `"manifest": 1` manifest has a hook entry with a filter field set THEN validation fails, naming the
  entry's index and asking for `"manifest": 2` (decision 3), for example `hooks[0]: matcher and if need "manifest": 2
  (got 1)`. A v1 manifest whose hook entries set
  no filter field still validates.
- **H3** WHEN a hook entry sets `if` and its `event` is not an `if` event THEN validation fails, naming the entry's
  index, the event and the `if` events (decision 7). `pretooluse` is not `PreToolUse`.
- **H4** WHEN a hook entry sets `matcher` THEN cg doesn't check it: any string on any event validates, including an
  event Claude Code gives no matcher support (decision 7; Claude Code ignores such a matcher).
- **H5** WHEN several entries name the same wrapper (different events, or the same event with a different `matcher`
  or `if`) THEN the manifest validates (decision 6).
- **H6** WHEN two entries have the same event, the same matcher, the same `if` (or neither sets it) and the same
  wrapper THEN validation fails as a duplicate, naming both indexes (decision 6). Matchers compare as written,
  except that an unset matcher and `"*"` count as the same (Q5). `command` is not compared: cg never reads it (U3),
  and the two entries would register the same thing.
- **H7** WHEN two entries name different wrappers that have the same install path (`hooks/a.sh` and `other/a.sh`)
  THEN validation fails, naming both indexes and the path (decision 6), as `git-hooks` already does for its scripts.
- **H8** WHEN a hook entry carries a key cg doesn't know (`onFailure`, `timeout`, …) THEN it is ignored, as every
  unknown manifest key is today (decision 2: no strict decoding), and nothing is written for it.

### Settings written

- **H9** WHEN an entry is registered THEN cg appends one matcher group to its event in `.claude/settings.json`:
  `{"matcher": <matcher, or "" when unset>, "hooks": [{"type": "command", "command": <pinned install path>, "if":
  <if>}]}`, with the `if` key only when `if` is set. Groups follow the manifest's order, after the event's existing
  groups.
- **H10** WHEN no entry of an extension sets a filter field and no two entries name the same wrapper THEN the
  settings cg writes are the same as today's, key for key, so an update to a cg with this change shows no difference
  in `settings.json` for existing extensions. (A manifest that repeats a wrapper got only its last entry registered
  before; now it gets every entry, H11.)
- **H11** WHEN an extension with one wrapper in N entries is installed THEN the wrapper is copied once to its install
  path, N registrations point at it, and the ledger records one `{event, wrapper}` per entry.

### Install, update, remove

- **H12** WHEN an extension is installed, updated or re-installed by `cg init` THEN every earlier registration of each
  of its wrappers is removed first, in any event, and then every current entry is registered, so a changed filter
  takes effect, a dropped entry disappears, and a hand edit of one of these groups is replaced (as today).
- **H13** WHEN hooks are registered THEN `settings.json` is written once for all of an extension's entries, not once
  per entry. If applying the extension fails, the existing snapshot restores `settings.json` and the wrappers.
- **H14** WHEN an extension is removed THEN every registration of each of its wrappers and every wrapper file it
  installed are removed.
- **H15** WHEN cg registers or removes an extension's hooks THEN every other entry in `settings.json` is kept as it
  was: the framework's hooks, the user's, other extensions', and keys cg doesn't manage.

### Unchanged

- **U1** v1 manifests without filter fields that pass H6 and H7 and don't repeat a wrapper install, update and are
  removed as today.
- **U2** The ledger format and `cg extension list` are unchanged (analysis, Recommended item 8). The hook count in
  `cg extension list` counts entries.
- **U3** `command` stays required and unread by cg (analysis, out of scope).
- **U4** Other contribution types behave as today.

## Documentation

- `docs/extensions.md`, the `hooks` contribution point: the two fields, that they need `"manifest": 2`, that cg
  writes them as Claude Code's `matcher` (on the matcher group) and `if` (on the handler) with a link to the hooks
  reference instead of restating it, the `if` events, several entries per wrapper and the two refusals (H6, H7),
  and that cg writes no other handler field (`onFailure`, `timeout`, …).
- `EXTENSIONS.md`, `hooks`: the same in the author's words, with the example above.

## Cross-repo acceptance (outside this PR)

- **X1** mnemcik/cg-extensions-registry, `schema/cg-extension.schema.json` (additive, Q2): hook items accept
  `matcher` (string) and `if` (string, at least one character); a manifest with a hook entry that sets either is
  `"manifest": 2`; and an entry that sets `if` has one of the `if` events (Q6). Every manifest that validates today
  still validates. Merged before any extension manifest uses the fields, because cg-extensions CI validates every
  manifest against the schema on registry `main`.
- Extensions adopting the fields (the `pr-workflow` extension, mnemcik/cg-extensions#45; `1password` replacing its
  second wrapper file) are their own changes, not part of #301.

## Questions for the owner (gaps and drift, each with a proposed answer)

- **Q1 — base branch and order.** Decision 9: "its own branch and PR from #259's M1 commit, merged after #259's PR and
  before the 2.0.0 release PR"; building from `main` was rejected because `main` lacked M1 then. #259 has since merged
  (`10d241c`, M1 included), so M1 is on `main`. **Proposed:** the branch `gh-301-extension-hook-filters` stays based
  on `main` (rebased onto it at `8840343`); the implementation starts once #300's PR is open, since both change
  `internal/extension` and `docs/extensions.md`, and it rebases onto whatever of #300 and #212 has merged before it
  opens; its own PR, merged before the 2.0.0 release PR. *Alternative:* start now and let whichever lands second
  rebase, as #300's plan says for #301.
- **Q2 — the registry schema change.** The analysis puts it "alongside #259's X1 change" (Recommended item 7); X1
  merged as mnemcik/cg-extensions-registry#25 without it. **Proposed:** a separate additive registry PR (X1 above),
  opened by this issue's author after the agreement and merged before any extension uses the fields; not a hold on
  the release, since every field is optional and a manifest using one too early fails cg-extensions CI. Its
  description points to this issue as registry#25 pointed to #259, with no separate cg-extensions issue (the
  registry README asks for `Fixes mnemcik/cg-extensions#NNN`; #25 is the precedent for a schema change that a
  consigliere issue decides). *Alternative:* file a cg-extensions issue for it first.
- **Q3 — a sixth `if` event.** Decision 7 refuses `if` outside "the five tool events". Claude Code's hooks reference
  (read 2026-11-27) now lists six: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`,
  `PermissionDenied` and `PreSkillShellCommand`. **Proposed:** accept the six, following decision 8 (Claude Code's
  own names) and decision 7's reason (refuse only where Claude Code never runs the hook). An event Claude Code adds
  later is refused until cg's list is updated, which fails loudly rather than installing dead code. *Alternative:*
  the five, as decided; `if` on `PreSkillShellCommand` is then refused.
- **Q4 — empty strings.** The decisions don't say what `""` means. **Proposed:** `""` is the same as omitted for
  both fields. For `matcher` that is Claude Code's meaning (match all) and today's written value, so `"matcher": ""`
  doesn't need v2. For `if`, cg writes no `if` key and the entry runs unfiltered; the registry schema refuses
  `"if": ""` (X1), so an indexed manifest can't carry it. *Alternative:* refuse `"if": ""` in cg too (a decoder that
  tells absent from empty).
- **Q5 — what counts as the same matcher for a duplicate.** Decision 6 refuses "an exact duplicate entry". Claude
  Code treats `"*"`, `""` and an omitted matcher alike (match all), so two entries that differ only there register
  the script twice for the same calls. **Proposed:** those three count as one for the duplicate check; every other
  matcher compares as written (`Bash|Edit` and `Edit|Bash` are different). *Alternative:* compare as written, so only
  omitted and `""` (the same after Q4) match.
- **Q6 — what the registry schema checks beyond the fields.** The analysis asks the schema to accept the fields "for
  `manifest: 2`" (Recommended item 7); decision 7 is about cg's validation only. **Proposed:** the schema also
  refuses `if` outside the `if` events, so cg-extensions CI catches it before anyone installs the extension; the
  duplicate and install-path rules (H6, H7) stay in cg only, since JSON Schema can't express them without copying
  cg's path cleaning. *Alternative:* the schema accepts the fields with the v2 gate and nothing more.
