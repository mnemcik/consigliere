---
title: "Analysis — extensions can't contribute skills or subagent definitions (#300)"
created: 2026-11-21
item: "https://github.com/mnemcik/consigliere/issues/300"
size: full
verdict: proceed
---

# Analysis — extensions can't contribute skills or subagent definitions (#300)

- **Item:** https://github.com/mnemcik/consigliere/issues/300
- **Size (proposed):** M: one new contribution type with its own ownership rule, install targets per agent tool,
  conflict checks, and a migration for the one extension that ships a skill today. Subagents are left out (D1).
- **Evidence base:** consigliere `origin/main` at `61abdfd` (2026-11-21); mnemcik/cg-extensions at `87c51e5`;
  mnemcik/cg-extensions-registry at `37db607`; #259's spec on branch `gh-259-extension-commands` at `e54d3b5`
  (awaiting agreement); agent tools' docs fetched 2026-11-21 (sources in [Agent tools](#agent-tools)).

## Question

An extension can contribute CLAUDE.md sections, notes, hooks, subcommands, templates, git hooks, gitignore lines and
a launcher, but no skill and no subagent definition. Extensions whose behaviour is agent instructions must put them
into CLAUDE.md text or notes behind a prose trigger, or copy a skill into `.claude/skills/` through `templates`,
where the copy is the user's file rather than the extension's. This affects extension authors and the workspaces
that install their extensions, first of all the planned `pr-workflow` extension (mnemcik/cg-extensions#45), which
ships its author and maintainer rules as skills.

A **"how"** item: the gap is real (below); the question is the shape of the contribution, who owns the installed
files, where they go for each agent tool, and how it ships relative to #259's manifest change.

## Current state (evidence)

**Contribution types.** `Contributions` has eight fields, none for skills or agents
(`internal/extension/manifest.go:51-63`). `json.Unmarshal` drops unknown fields (`manifest.go:111-116`), so a
manifest field this cg doesn't know is ignored without a message. Only the schema version is checked up front
(`manifest.go:132`).

**The workaround in use.** `github-digest` (cg-extensions `github-digest/cg-extension.json`, v0.2.0) copies
`SKILL.md` and its fetcher script with `templates` to `.claude/skills/github-digest/`. The docs describe
`templates` as copying "into the workspace `templates/` tree" (`docs/extensions.md:199-201`), while `Validate` only
requires `dest` to be inside the workspace (`manifest.go:206-216` and `copyPathsInside`). Copied files are written `0644`
(`apply.go:197`), so the skill has to call its script through `python3` and says so (`github-digest/templates/skill/SKILL.md:15`).
Its `SKILL.md` also hard-codes `.claude/skills/github-digest/` in its commands (`SKILL.md:15`, `:25`, `:34`).

**Ownership of copied files** (`internal/extension/ownership.go:20-36`, `docs/extensions.md:345-385`). A three-way
comparison of the base cg last wrote, the file on disk, and what the extension ships now:

| On disk vs base | Extension vs base | What cg does |
|---|---|---|
| unchanged | changed | updates the file |
| changed | unchanged | keeps the user's file, **silently** |
| changed | changed | keeps the user's file, records it as **pending**; raised at every session start (`cmd/session.go:163`, `:190-200`) until `cg extension resolve` |
| not recorded | differs | install refuses without `--force` |

Hook wrappers and git-hook scripts are excluded: "code that must match the extension's source for the trust model,
so cg always owns them" (`ownership.go:33-35`).

**Core's own skill.** `wrap` is one of the framework's `.claude/` files (`cmd/init.go:567-577`, `skills/wrap/SKILL.md`
→ `.claude/skills/wrap/SKILL.md`). `cg sync` reports a user-edited one as **drifted, never clobbered**, and updates
an unedited one (`docs/workspace-sync.md:39-46`, `:88-103`). That is the behaviour the issue asks extensions to get.

**Removal and renames.** `Reverse` deletes copied files still as cg wrote them and keeps edited ones
(`apply.go:290-306`); it removes files only, not directories (`apply.go:500-505`). `OrphanLedger` treats a
destination as still shipped only if it is in the new ledger's `Notes` or `Templates` (`apply.go:337-347`); reapply
runs it after applying the new version (`cmd/extension.go:200-215`).

**Registry.** The registry's manifest schema is closed: `additionalProperties: false` at the top and in
`contributes`, and `manifest` is `const: 1` (cg-extensions-registry `schema/cg-extension.schema.json`). A manifest
with a new key fails the registry's check.

**#259 (in flight, not agreed).** Its spec proposes manifest **v2** for any manifest that declares `commands`, with
v1 manifests without `subcommands` still valid, so an older cg refuses the new manifests with its existing error (spec
Q1). Its M1 changes `manifest.go` (`SchemaVersion`, a new contribution, a raw decode of `contributes` to find the
removed `subcommands` key), `ledger.go` (`Commands`), `apply.go:240`, a new `commands.go` with a cross-extension
conflict check, and the install/update/`cg init` flows in `cmd/extension.go`; it is planned as cg 2.0.0
(`specs/gh-259-extension-commands/plan.md`, M1).

**Who needs what.** #300's "Blocks" list names cg-extensions #45, #46, #49 and #50, and says #45 "needs the skills
part only, it ships no subagents of its own". #45 now uses the `independent-review` plugin's reviewers rather than
its own (mnemcik/cg-extensions#45, "Skills and reviewers it uses"). #266 notes that a Claude Code extension couldn't
carry `/wrap` without this.

### Agent tools

Where tools discover skills (the `SKILL.md` folder format, https://agentskills.io/specification) and subagents,
from each tool's docs on 2026-11-21:

| Tool | Project skills | Same name, user vs project | Subagent format, project dir |
|---|---|---|---|
| Claude Code | `.claude/skills/` (not `.agents/skills`) | personal wins over project, which is shadowed | Markdown + frontmatter, `.claude/agents/`; project beats user |
| Codex CLI | `.agents/skills/` | no precedence; both listed | TOML, `.codex/agents/` |
| GitHub Copilot (VS Code, CLI, coding agent) | `.github/skills/`, `.claude/skills/`, `.agents/skills/` | not documented | `.agent.md`, `.github/agents/` (VS Code also `.claude/agents/`) |
| Cursor | `.agents/skills/`, `.cursor/skills/`, also `.claude/skills/`, `.codex/skills/` | not documented | Markdown, `.cursor/agents/`, `.claude/agents/`, `.codex/agents/` |
| Gemini CLI | `.gemini/skills/`, alias `.agents/skills/` | workspace wins over user | Markdown, `.gemini/agents/` |

Sources: https://code.claude.com/docs/en/skills, https://code.claude.com/docs/en/sub-agents,
https://learn.chatgpt.com/docs/build-skills, https://learn.chatgpt.com/docs/agent-configuration/subagents.md,
https://code.visualstudio.com/docs/copilot/customization/agent-skills (dated 2026-11-18),
https://docs.github.com/en/copilot/concepts/agents/about-agent-skills, https://cursor.com/docs/context/skills,
https://cursor.com/docs/context/subagents, https://geminicli.com/docs/cli/skills/,
https://geminicli.com/docs/core/subagents/.

What follows from it:
- No single directory reaches every tool: `.claude/skills/` misses Codex and Gemini; `.agents/skills/` misses
  Claude Code. The spec defines the folder format, not an install directory.
- Copilot and Cursor read both `.claude/skills/` and `.agents/skills/`, so installing one skill into both shows it
  twice there (Cursor duplicates reported on its forum; not in its docs).
- Subagent definitions have no shared format (Markdown with different frontmatter, TOML, `.agent.md`).
- In Claude Code a user-scope skill of the same name shadows the workspace's copy; elsewhere the rule differs or
  isn't documented. cg can't control this, only detect it.
- **Not verified:** whether any tool needs the execute bit on a skill's scripts. Claude Code's docs show both
  `python3 ${CLAUDE_SKILL_DIR}/scripts/x.py` and a direct `${CLAUDE_SKILL_DIR}/scripts/render.sh` call; a direct
  call needs `+x` (assumption, high).

**Recon checked:** consigliere `internal/extension/` (manifest, ledger, apply, ownership), `cmd/init.go`,
`cmd/extension.go`, `cmd/session.go`, `docs/extensions.md`, `docs/workspace-sync.md`: as above. cg-extensions: the
11 manifests; one (`github-digest`) writes into `.claude/`, none declares anything else agent-specific.
cg-extensions-registry schema: closed, v1 only. #259's analysis, spec and plan: manifest v2 proposal, overlap below.
Issues #266, #231, #265, #238, #301; cg-extensions #38, #45, #46, #49, #50. External: the five tools' docs above
and the Agent Skills spec. Existing solutions: Claude Code plugins ship `skills/` and `agents/` with namespaced
names (`plugin:skill`), but they are tool-specific and installed per user or project through the tool, not by cg;
nothing found that installs one skill for several agent tools.

## Validity

| Question | Answer | Evidence |
|---|---|---|
| Does the problem exist? | Yes for skills: no contribution type, and the `templates` workaround is undocumented, loses the execute bit and hands the file to the user. | `manifest.go:51-63`; `docs/extensions.md:199-201`; `apply.go:197`; `github-digest` manifest |
| Is the expected behaviour right? | Mostly. "Owned by the extension, a local edit shown as drift" matches core's `cg sync` for `wrap`. One claim is overstated: an edited copy doesn't **silently** stop following; when the extension changes it too, it is pending and raised every session. It is silent only while the extension leaves the file unchanged. | `ownership.go:20-36`; `cmd/session.go:163`; `docs/workspace-sync.md:88-103` |
| Is it still current? | Yes. Nothing on `main` or on #259's branch adds skills. | `61abdfd`; #259 spec "Unchanged" U5 |
| Is it the right problem? | Partly. Skills are on the critical path (#45, #50). Subagents are not: no listed blocker needs them, and their formats differ per tool, so one contribution can't serve them the way `SKILL.md` serves skills. | #300 "Blocks"; #45 "Skills and reviewers it uses"; [Agent tools](#agent-tools) |

**Verdict:** partly valid. Real problem: an extension can't ship a **skill** that it owns (installed, updated,
removed with it, edits reported), into the place each of the workspace's agent tools reads. Subagent definitions are
a separate, smaller-demand problem (D1).

## Options

### Recommended: a `skills` contribution, owned by the extension, installed per agent tool, manifest v2

- **What:**
  - **Manifest.** `"skills": [{ "src": "skills/pr-author" }]`: a directory inside the extension holding `SKILL.md`
    and any files beside it. The skill's name is the directory's base name, which must match `[a-z0-9-]+` and the
    `name:` in `SKILL.md`'s frontmatter (the Agent Skills rule). The whole tree is installed; files added or dropped
    between versions follow it. A manifest that declares `skills` must declare the manifest version that the release
    shipping `skills` introduces, v2 in the recommended case (D4).
  - **Targets.** cg keeps a table from agent tool to project skills directory (`claude` → `.claude/skills/`, `agents`
    → `.agents/skills/`, later others). A workspace setting lists the targets; the default is `claude` only, which
    is what cg integrates with today (D2). Each skill is installed under each listed target as `<dir>/<name>/`.
    Adding a tool later is a table row, not a manifest change.
  - **Ownership.** The installed tree is the extension's. Install and update reuse the three-way comparison, with
    one change: a file the user edited while the extension didn't change it is reported instead of kept silently,
    the way `cg sync` reports drift. An edited file is never overwritten (D3). Today nothing records that case
    (only `pending`, where the extension changed the file too, is surfaced: `ownership.go:27-29`,
    `cmd/session.go:163`), so it needs a record and a lifecycle (D12). Proposed: a `drifted` list in the ledger
    beside `pending`, holding the file's destination and the content id the user's version had; shown in
    `cg extension list` and once in the session-start notice; `cg extension resolve --take` restores the
    extension's version, `--keep` records the user's version as acknowledged so it isn't raised again until the
    file or the extension's version changes; `--merge` doesn't apply (there is no upstream change to merge). When the
    extension later changes the file too, it becomes `pending` as today.
  - **Granularity.** Per file, as today: an existing file at a target that cg didn't write and that differs is
    refused without `--force`; one identical to the extension's is adopted (`copyAdopt`). The ledger's `files` map
    is keyed by each target's path, so a skill installed for two targets has two entries per file; the name
    conflict check runs per target (`wrap` collides only under `claude`). My call, for the spec to confirm.
  - **Modes.** A file keeps the execute bit it has in the extension's tree (git mode `100755` → `0755`), so a skill
    can call its scripts directly (D8).
  - **Conflicts, refused before anything is applied:** two installed extensions shipping the same skill name, or an
    extension shipping a name the framework ships (`wrap`); an existing directory at the target that cg didn't
    write (refused without `--force`, as for copied files). A same-name skill at user scope (`~/.claude/skills/<name>`
    for the `claude` target) can't be prevented: cg warns at install and update, and `cg extension list` shows it,
    since in Claude Code it shadows the extension's copy (D7).
  - **Removal** deletes the skill's files still as cg wrote them and then the directories left empty; edited files
    are kept and listed, as today.
  - **Ledger.** A `skills` entry per skill (name, targets), with its files in the existing `files` map, so
    `OrphanLedger`, `Reverse`, `diff` and `resolve` keep working. A destination that moves from `templates` or
    `notes` to `skills` counts as cg's before the apply and as still shipped after it (`github-digest`'s migration,
    see Hidden constraints).
  - **Docs.** A contribution-point section; `templates` documented as what it is (any workspace path, user-owned
    once copied) and no longer the way to ship a skill; a note for skill authors to refer to their own files
    relative to the skill directory (`${CLAUDE_SKILL_DIR}` in Claude Code) rather than a hard-coded `.claude/...`.
- **Trade-off:** a second ownership rule beside notes and templates (reported vs silent for the edit-only case), and
  a new workspace setting. In return a skill behaves like core's `wrap` and like the extension's code, and cg stays
  agent-neutral (#266, #231).
- **Wrong choice when:** the owner wants skills to be the user's to edit, like notes; then the existing `templates`
  path plus documentation is enough (the "smallest" alternative).
- **Rabbit holes:**
  - Installing into several targets at once and de-duplicating for tools that read two of them: keep the default to
    one target and document the duplicate; don't build per-tool de-duplication.
  - Rewriting `SKILL.md` per target (paths, tool-specific frontmatter): don't; the skill author writes portable
    content.
  - Subagents in the same change (D1).
  - User-scope installs (`~/.claude/skills/`): out of scope; cg writes inside the workspace only.

### Alternative: Claude Code only, `.claude/skills/` fixed (runner-up)

- **What:** the same contribution and ownership, but the install directory is always `.claude/skills/<name>/`; no
  setting, no table.
- **Trade-off:** less code and no new setting; reaches Claude Code, Copilot and Cursor (they read `.claude/skills/`),
  not Codex or Gemini. Moving to a table later means a migration of installed paths.
- **Wrong choice when:** #266's direction (Claude Code-specific parts out of core) is meant to hold for new
  contribution types too.
- **Rabbit holes:** none beyond the recommended option's.
- **Wins instead if:** the owner treats agent neutrality as #266's job and wants the smallest change on the critical
  path; the table can come with #266.

### Alternative: document `templates` as the way to ship a skill (smallest thing that works)

- **What:** no new type. Document that `templates.dest` may be any workspace path, recommend
  `.claude/skills/<name>/...`, and accept the user-owned semantics.
- **Trade-off:** no code, no release coupling. But each file is listed by hand, scripts lose `+x`, a user edit wins
  silently while the extension doesn't change the file, there's no conflict check between extensions, and the
  directory survives removal. It doesn't meet the issue's first need.
- **Wrong choice when:** the skill carries rules the extension relies on (#45's author and maintainer rules), which
  is the case driving this issue.
- **Rabbit holes:** none.

"Do nothing" isn't credible: #45 and #50 wait on it. Shipping the skills as an agent tool's plugin instead (Claude
Code plugins carry `skills/` and `agents/`) isn't either: plugins are per tool and installed through the tool, not
with the extension, and #45 decided the workflow is one extension, "not split across a plugin".

## Shipping: manifest version and release

**What an older cg does with the new field.** cg 1.x and any 2.x without `skills` decode with `json.Unmarshal`,
which drops `contributes.skills`: the extension installs, its skills don't, and nothing says so. Two levers:

1. **`skills` requires a manifest version no older cg accepts** (D4): the first version introduced by the release
   that ships `skills`. That is **v2** if #300 ships in 2.0.0 (whatever #259's Q1 decides: if Q1 keeps v1 for
   `commands`, #300 introduces v2 itself), or in a later release when Q1 keeps v1; it is **v3** if #300 ships later
   and 2.0.0 already accepts v2 (#259's Q1 = v2, or #301 introduces v2), since 2.0.0 would accept v2 and drop
   `skills`, unless 2.0.0 decodes v2 strictly (D6 (a)), in which case v2 suffices. Any older cg then refuses the manifest with
   "unsupported manifest schema version", and the new cg refuses a manifest that declares `skills` under an older
   version, asking for the right one, as #259's S6 does for `commands`.
2. **Future fields fail loudly** (D6). Today's policy, written in the docs, is the opposite: "A cg older than this
   contribution point ignores the key" (`docs/extensions.md:257-259` for `gitignore`, `:282-285` for `launcher`).
   That suits fields an older cg can safely skip; it doesn't suit `skills`, where skipping loses the extension's
   rules. Three ways to make a later field safe:
   - **(a) Strict decoding of v2:** an unknown key in `contributes` or in an entry is refused. Every later field,
     including harmless optional ones, then makes older 2.x refuse the manifest. #259's M1 already decodes
     `contributes` raw to find `subcommands`, so the code is small, but it changes #259's spec and the docs' policy.
   - **(b) A version bump per release that adds a field an older cg must not ignore** (v3, v4, …): no new mechanism,
     keeps the documented policy for harmless fields; costs a version per such release. Only manifests that declare
     the new field move to the new version, so extensions that don't use it keep installing on older cg.
   - **(c) A minimum cg version field** (`"requires-cg": ">=2.1.0"`): the extension states its floor; harmless
     fields stay harmless; a new field and a cg 2.0 that doesn't check it yet still drop silently, so it only helps
     from the release that introduces it.

   Recommended: (b), applied here as D4, because it needs nothing beyond what D4 already does and
   keeps the documented policy; (a) or (c) only if the owner wants a general mechanism.

**Release (D5).** Recommended: **ship #300 in cg 2.0.0** with #259 (and #301). The extensions chain
(cg-extensions#44) needs all three in one release anyway; one manifest version change, one registry schema update
and one migration note for authors instead of two. #300's work starts after #259's M1 (it needs M1's manifest and
ledger shape) and can run beside #259's M2–M4, which touch dispatch, `cg start` and session state rather than the
manifest.

*Alternative: a later release (2.1.0).* Whether it fails safe depends on #259's Q1:
- **Q1 keeps v1** (no v2 in 2.0.0): #300 introduces v2 in 2.1.0, and 2.0.0 refuses those manifests with its existing
  error (`manifest.go:132`). Safe with no further change.
- **Q1 = v2** (2.0.0 accepts v2): a v2 manifest with `skills` installs on 2.0.0 without its skills, silently,
  unless `skills` takes v3 (lever (b)) or 2.0.0 decodes v2 strictly (lever (a)).

## Overlap with #259 (and #301)

| File | #259 (M1) | #300 |
|---|---|---|
| `internal/extension/manifest.go` | `SchemaVersion` 2, `commands`, raw `contributes` decode | `skills` type and validation; the D4 version required for it; strict decode if D6 |
| `internal/extension/ledger.go` | `Commands` replaces `Subcommands` | `Skills` entries |
| `internal/extension/apply.go` | records commands (`:240`) | apply, `Reverse`, `OrphanLedger` for skill trees |
| `internal/extension/ownership.go` | — | the reported edit-only case; modes |
| new `commands.go` / conflict checks in `cmd/extension.go` | cross-extension command conflicts before apply | cross-extension and framework skill-name conflicts, same place and pattern |
| `cmd/extension.go` `list` | commands shown | skills, targets, drift and user-scope shadowing shown |
| `docs/extensions.md`, `EXTENSIONS.md` | contribution point, v2 | contribution point, `templates` wording |
| registry `schema/cg-extension.schema.json` | `manifest: 2`, `commands` | `skills` |

#301 adds fields to hook entries (`manifest.go` `HookContribution`, `settings.go`), so it shares the manifest and the
v2/strictness question with both. Reusing #259's pre-apply conflict check for skill names is the natural shape; the
two should not be implemented in parallel on `manifest.go`, `ledger.go` and `apply.go`.

## Hidden constraints

- **Migration of `github-digest`.** Moving its two files from `templates` to `skills` at the same paths breaks in two
  places as the code stands:
  - **Before the apply:** `ApplyOptions.base` counts a destination as cg's only when the old ledger lists it under
    `Notes` or `Templates` (`apply.go:57-68`), and a skill file wouldn't be found there. The new release changes
    `SKILL.md` (its script path), so the file on disk differs from the new one, `decideCopy` returns `copyRefuse`
    (`ownership.go:120-124`) and `cg extension update` fails with a conflict. `update` has no `--force` (only
    `install` does, `cmd/extension.go:37`), so a user can't get past it.
  - **After the apply:** `OrphanLedger` (`apply.go:337-347`) lists the files as dropped templates; `Reverse` then
    deletes them if unchanged since the old version, or reports them as "kept" if the new version changed them.

  Both the tracked check and the orphan check must count destinations that move between `templates`/`notes` and
  `skills`; the acceptance test covers the whole move (an unedited and an edited `SKILL.md`). The extension also
  needs a release that declares `skills` and refers to its script relative to the skill directory.
- **Reversibility.** A skill contribution is reversed by `cg extension remove`; a workspace that changes its
  targets setting needs installed skills moved (reapply on the setting change, or on the next `cg extension
  update`; D13).
- **Failure mid-operation.** Skill trees go through the existing snapshot and atomic writes (`apply.go:520-560`); a
  tree of several files must roll back as a whole on failure, as copied files do today.
- **Security.** A skill can carry scripts the agent runs, under the agent's own permission prompts, not cg's hook
  trust model. Keeping skills extension-owned and reporting edits keeps what runs equal to the extension's pinned
  source unless the user chose otherwise. A skill's `allowed-tools` frontmatter can widen what the agent runs
  without asking in Claude Code (https://code.claude.com/docs/en/skills); cg doesn't vet it. Worth a line in
  `EXTENSIONS.md` for reviewers of extensions.
- **Worktrees.** Installed skills are committed workspace files; a session worktree sees a newly installed skill
  once it has landed, as the docs already say for git-hook scripts.
- **Who else is affected.** Registry maintainers (schema), `github-digest` users (migration), #266 (a Claude Code
  extension could then carry `/wrap`; slash commands stay uncovered, though Claude Code lets a skill take a
  command's place), every cg 1.x user once extensions move to v2 (they must update cg first).
- **Performance, data, compliance:** nothing found.

## Open decisions

| # | Decision | Recommended answer | Status |
|---|---|---|---|
| D1 | Subagent definitions | Out of #300: no blocker needs them and their formats differ per tool. A new issue for them if wanted; #300's text updated to say so. | agreed |
| D2 | Where skills are installed | A per-tool target table and a workspace setting, default `claude` only (`.claude/skills/`). Runner-up: fixed `.claude/skills/`. | agreed |
| D3 | Ownership of installed skills | Extension-owned: unedited files follow updates; an edited file is never overwritten and is **reported** even when the extension didn't change it; `resolve` settles it. | agreed |
| D4 | Manifest version for `skills` | A manifest declaring `skills` must declare the first manifest version the shipping release introduces: v2 in 2.0.0 (either Q1 outcome) or later with Q1 keeping v1; v3 if later and 2.0.0 already accepts v2 (Q1 = v2 or #301), unless 2.0.0 decodes v2 strictly (D6 (a)). | agreed |
| D5 | Release | In cg 2.0.0 with #259 and #301. A later release is safe as is if #259's Q1 keeps v1; if Q1 = v2, only with D6's (a) or (b). | agreed |
| D6 | How later fields avoid being dropped by an older cg | (b) a manifest version bump when a release adds a field an older cg must not ignore (as D4 does); keep the documented "older cg ignores the key" policy for harmless fields. Alternatives: (a) strict v2 decoding (changes #259's spec and the docs' policy), (c) a minimum-cg field. | agreed |
| D7 | Name conflicts | Refuse a skill name another installed extension or the framework ships, and an unowned directory at the target without `--force`; warn about a same-name user-scope skill. | agreed |
| D8 | File modes | Keep the execute bit from the extension's tree. | agreed |
| D9 | Cross-repo work in the same release | Registry schema accepts `skills`; `github-digest` moves to `skills` and refers to its script relative to the skill directory. | agreed |
| D10 | Size | M, with subagents out (D1). | agreed |
| D11 | The issue's wording | Correct "silently stops following" (it does when only the user changed the file; a later extension change is raised) and the subagent need; the owner decides whether to edit the issue. | agreed |
| D12 | Record and lifecycle of an edit-only change (D3) | A `drifted` ledger list; shown in `cg extension list` and once at session start; `resolve --take` restores, `--keep` acknowledges until the file or the extension's version changes; `--merge` n/a. | agreed |
| D13 | Where the targets setting lives and when a change takes effect (D2) | A `.cg.json` key (e.g. `skills.targets`, default `["claude"]`); a change takes effect on the next `cg extension update`, which moves installed skills. | agreed |

## Review

**Review 1** — `specs/gh-300-extension-skills/analysis.md` @ 93bc011 (uncommitted) · 2026-11-21
- Reviewers: doc-reviewer (builder check, claim check, missed option, hidden constraints, governing rules)
- Tests: n/a (document)
- Findings: 0 blocker, 4 should-fix, 1 nit → fixed: migration also refused before the apply (`ApplyOptions.base`, no `--force` on update); D5's fallback split by #259's Q1; D6 now weighs strict decoding, a version bump per field and a minimum-cg field against the documented "older cg ignores the key" policy; the edit-only "reported" state given a record and lifecycle (D12); plugins weighed in one line. owner's: none; rejected: none; dropped in validation: none
- Builder questions: 5 → 2 minor (my call: per-file granularity with adoption of identical files; `files` keyed per target path, conflict check per target), 3 owner's: drift record (D12), targets setting location and timing (D13), how later fields avoid being dropped (D6)
- Verified true: contribution fields, unknown-key drop, `templates` docs vs validation, `0644` writes, `github-digest` manifest and `SKILL.md` paths, ownership table, pending raised each session, `wrap` in `frameworkClaudeFiles`, `Reverse` files only, `OrphanLedger` behaviour, registry schema closed and `const: 1`, #259 S6/Q1/U5/M1 and the overlap table, issue texts, Claude Code / Codex / Copilot / Cursor skill directories and Claude Code precedence
- Not verified: Gemini CLI, VS Code and subagent doc pages; Cursor forum duplicate reports; the execute-bit need (an assumption in the text)

**Review 2** — `specs/gh-300-extension-skills/analysis.md` @ 6a37f53 (uncommitted; delta from 93bc011) · 2026-11-21
- Reviewers: doc-reviewer (delta: regression check and new problems)
- Tests: n/a (document)
- Findings: 0 blocker, 1 should-fix, 1 nit → fixed: D4 now names the first manifest version the shipping release introduces (v3 if later with Q1 = v2), with the lever-1 text and D6 matched; (b)'s cost corrected (only manifests using the new field move). owner's: none; rejected: none; dropped in validation: none
- Builder questions: none new
- Regression check: all five round-1 findings resolved
- Verified true: `ApplyOptions.base` (`apply.go:57-68`), `copyRefuse` (`ownership.go:120-124`), `--force` on install only and update reapplies even at the same version (`cmd/extension.go:37`, `:745-746`, `:792`), docs quotes (`docs/extensions.md:257`, `:282`), `resolve` flags (`cmd/extension.go:258`)
- Not verified: whether a `drifted` list fits the ledger schema (a spec question)

**Review 3** — `specs/gh-300-extension-skills/analysis.md` @ 222947c (uncommitted; delta from 6a37f53) · 2026-11-21 · **clean**
- Reviewers: doc-reviewer (delta: regression check and new problems)
- Tests: n/a (document)
- Findings: 0 blocker, 0 should-fix, 3 nit → applied as wording after the round: D4's v3 case also covers #301 introducing v2 and gives way to D6 (a); the overlap row says "the D4 version". owner's: none; rejected: none
- Builder questions: none new
- Regression check: both round-2 findings resolved
- Not verified: #301's own manifest-version plan (not analysed yet); code citations not re-checked in this delta (no new code claims)

## Verdict

**proceed:** the recommendation is reviewed (round 3 clean) and the owner agreed D1–D13 as recommended (2026-11-21).
With D5 (ship in 2.0.0) agreed, D4 resolves to `"manifest": 2` whichever way #259's Q1 is decided; the v3 case
applies only to a later release and drops. Next: the spec and plan in this folder, then the agreement comment;
implementation after #259's M1 lands.

## Q→A log

- 2026-11-21 — The owner went through D1–D13 one at a time and agreed each as recommended:
  - D1: subagents out of #300 (a separate issue if wanted).
  - D2: a per-tool target table, default `claude` (`.claude/skills/`).
  - D3: skills are extension-owned; an edited file is never overwritten and always reported.
  - D4: a manifest declaring `skills` declares `"manifest": 2` (follows from D5; works for either outcome of
    #259's Q1).
  - D5: ships in cg 2.0.0 with #259 and #301.
  - D6: lever (b), a version bump only for a field an older cg must not ignore; the documented "older cg ignores
    the key" policy stays for harmless fields; no strict decoding.
  - D7: refuse a name another extension or the framework ships; per-file adoption or refusal (`--force` on
    install) for an unowned directory; warn about a same-name user-scope skill; checked per target.
  - D8: keep the execute bit from the extension's tree.
  - D9: the registry schema and `github-digest` change in the same release window.
  - D10: size M.
  - D11: edit the issue body (the "silently" claim, subagents out, a pointer to this analysis), drafted for the
    owner's go-ahead.
  - D12: a `drifted` ledger list; shown in `cg extension list` and once at session start; `--take` restores,
    `--keep` acknowledges until the file or the extension's version changes; `--merge` n/a.
  - D13: `.cg.json` `skills.targets`, default `["claude"]`; a change takes effect on the next
    `cg extension update`.
