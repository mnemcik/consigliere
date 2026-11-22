# Spec — extensions contribute skills they own (#300)

Implements the decisions in [analysis.md](analysis.md#qa-log) (D1–D13, agreed 2026-11-21). Each scenario below is one
behaviour and maps to at least one test; `plan.md` names the commit that delivers it. Q1–Q9 at the end are the
choices this spec makes beyond the decisions, for the owner's agreement.

Builds on #259's first milestone (manifest v2, the pinned-manifest helper, the pre-apply conflict check in
`cmd/extension.go`); it is written against #259's spec as it stands, and anything that changes there before
agreement is re-checked here.

## Terms

- **Skill:** a directory in the extension holding `SKILL.md` and any files below it (the Agent Skills format,
  https://agentskills.io/specification). Its **name** is the directory's base name.
- **Target:** an agent tool cg installs skills for, from a built-in table: `claude` → `.claude/skills/`, `agents` →
  `.agents/skills/`. A skill installed for a target lives at `<target dir>/<name>/`.
- **Targets setting:** `.cg.json` `skills.targets`, a list of target names; absent means `["claude"]`.
- **Skill file:** one file of an installed skill, at its workspace-relative path under a target.
- **Base:** the content id (`ContentID`, the LF-normalised git blob id) of the version cg last wrote to a skill
  file, recorded in the ledger's `files` map.
- **Drifted:** a skill file whose content on disk differs from its base while the extension's version equals the
  base (the user changed it, the extension didn't). Deleting a file counts as a change.
- **Pending:** as today: the user and the extension both changed the file since the base.
- **User-scope skill dir:** the target's directory in the home directory: `~/.claude/skills/` for `claude`,
  `~/.agents/skills/` for `agents`.

## Manifest shape

```json
"manifest": 2,
"contributes": {
  "skills": [
    { "src": "skills/github-digest" }
  ]
}
```

`src` is a directory inside the extension. Everything below it is installed; nothing is listed file by file.

## Scenarios

### Manifest

- **S1** WHEN a v2 manifest declares `skills` entries whose `src` is a relative path inside the extension THEN it
  validates.
- **S2** WHEN a `skills` entry's `src` is empty, absolute, or climbs out with `..` THEN validation fails, naming the
  entry's index.
- **S3** WHEN the base name of `src` breaks the Agent Skills name rule (1–64 characters of `[a-z0-9-]`, no leading
  or trailing hyphen, no `--`) THEN validation fails, naming the entry and the rule.
- **S4** WHEN one manifest declares two skills with the same name THEN validation fails, naming both indexes.
- **S5** WHEN a manifest declares `skills` with `"manifest": 1` THEN validation fails, asking for `"manifest": 2`.
  A v1 manifest without `skills` (and without what #259 refuses) still validates.

### Reading the skill at install time

These run when cg reads the extension's pinned tree, before anything is written.

- **S6** WHEN `src` isn't a directory, or has no `SKILL.md` at its top THEN install (or update, or `cg init`) fails,
  naming the skill, and the workspace is unchanged.
- **S7** WHEN `SKILL.md` has no YAML frontmatter, or its `name` differs from the directory's base name, or its
  `description` is missing, empty or longer than 1024 characters THEN install fails, naming the skill and the field
  (Q7).
- **S8** WHEN the skill's tree in the extension's git tree contains a symlink (mode `120000`) or a submodule link
  (`160000`) THEN install fails, naming the path, on every platform (Q3).
- **S8b** WHEN the skill's tree holds dot-files or nested directories THEN every file the git tree has is installed;
  nothing outside the git tree is.

### Install

- **S9** WHEN an extension with a skill is installed and `skills.targets` is absent THEN every file of the skill's
  tree, subdirectories included, is written under `.claude/skills/<name>/` at the same relative path, and nothing is
  written under `.agents/skills/`.
- **S10** WHEN `skills.targets` is `["claude", "agents"]` THEN the skill is written under both directories, and the
  ledger records each file once per target path.
- **S11** WHEN `skills.targets` is `[]` THEN no skill is written, the install otherwise succeeds, and
  `cg extension list` shows the skills as not installed (no targets) (Q5).
- **S12** WHEN `skills.targets` names a target cg doesn't know THEN install and update fail, naming it and the known
  targets.
- **S13** WHEN a file in the skill is executable in the extension's git tree (mode `100755`) THEN it is written
  `0755`; every other skill file is written `0644` (Q4). A skill file's mode is part of what cg compares: the
  extension's version is its content **and** its execute bit.
- **S13a** WHEN a skill file on disk has the extension's content but not its execute bit (an adopted file, a file
  copied by a previous version as a template, or a workspace committed from Windows) THEN install, update and
  `cg init` set the mode without rewriting the content, and the file counts as cg's.
- **S13b** WHEN `cg extension resolve --take` restores a skill file THEN it is written with the extension's mode.
- **S13c** WHEN a skill file's content is cg's but its mode differs from the extension's THEN `cg extension list`
  reports it as a mode to fix, and the next `cg extension update` fixes it; it is not drift. On Windows, which has
  no execute bit, modes are written as the platform allows and never compared, so nothing is reported there.
- **S14** WHEN the ledger is saved after an install THEN it has one `skills` entry per skill (its name, the targets
  it was installed for, and the workspace-relative path of every file it installed), and every skill file is in
  `files` with its base. A skill's files are found from that list, never by path prefix.

### Conflicts, refused before anything is applied

- **S15** WHEN an extension being installed ships a skill whose name another installed extension ships, for a
  target both install it for THEN install fails, naming the skill, both extensions and the target; files,
  `.cg.json` and ledgers are unchanged.
- **S16** WHEN it ships a skill whose name a framework `.claude/` skill has (today `wrap`) and the targets include
  `claude` THEN install fails the same way, naming the framework.
- **S17** WHEN the same name exists only under a target the other side doesn't install for (for example `wrap` with
  targets `["agents"]`) THEN it isn't a conflict.
- **S18** WHEN `cg extension update` (one target or all) brings versions whose skill names conflict, with each
  other or with an extension not being updated THEN the update fails before any target is applied. When the
  conflict is with an installed extension that isn't being updated, the message adds: if a newer version of that
  extension drops the skill, update it first. It is printed as a condition: cg doesn't fetch the other extension to
  check.
- **S18b** WHEN one `cg extension update` would move skills between installed extensions (an extension's new
  version adds a skill another installed extension's new version drops) and the moves can be ordered (no cycle:
  one move A→B, or a chain A→B→C) THEN it fails before any target is applied, naming each skill and both of its
  extensions, and printing the `cg extension update <name>` commands to run: one per extension taking part in a
  move, droppers first, then a plain `cg extension update` for the others (Q9). Run in that order, each dropper's update removes the skill's unedited files and keeps and lists edited ones
  (S23, S24), and each gainer's update installs the skill, refusing any kept edited file as unowned (S20) with the
  advice to move it aside.
- **S18d** WHEN the moves form a cycle (a swap between two extensions, or a ring over three or more) THEN it fails
  the same way, naming the skills and extensions in the cycle, saying no order of updates can apply it, and
  naming the ways out: wait for a release of one of them that doesn't take part in the cycle, or remove one of the
  extensions, which also drops its notes, templates and pending state from cg's records. Edited files the removal
  keeps on disk stay in the way of the next update or install that writes to their paths: an update refuses them
  until they are moved aside (S20); an install can instead overwrite them with `--force`.
- **S18e** WHEN the advice of S18b is followed, command by command THEN each command either succeeds or fails only
  per S20, naming a kept edited file, and succeeds when run again after that file is moved aside (the S20 message
  says to run it again before the next command, since a later gainer in a chain would otherwise meet the skill
  still installed and fail per S18b); at the end every
  skill is installed by its new owner, and no file is owned by two ledgers or by none, apart from the files moved
  aside. Tested with no edited file, with one, and with a chain whose middle extension hits the refusal.
- **S18c** WHEN an extension's note or template destination lies inside a directory where an installed skill lives
  (`<target dir>/<name>/`), or a skill would be installed over another extension's note or template THEN install
  and update fail, naming both.
- **S19** WHEN `cg init` re-installs extensions THEN the skill-name check runs over the whole set before the first is
  applied, as #259's command check does, and on any conflict nothing is applied.
- **S20** WHEN a file exists at a skill file's path that cg has no record of writing and it equals the extension's
  content THEN it is adopted (recorded, content not rewritten, mode set per S13a); WHEN it differs THEN install
  refuses, naming it, unless `--force`, which overwrites it. `update` and `cg init` have no `--force`: they refuse
  the same way, and the message says to move the file aside and run the command again.
- **S21** WHEN a directory of the same name exists in the target's user-scope skill dir THEN install and update
  succeed and print a warning that the agent tool may use that skill instead, naming both paths;
  `cg extension list` shows it too.

### Update

- **S22** WHEN a skill file is unchanged on disk since its base and the new version changes it THEN it is updated.
- **S23** WHEN the new version adds a file to the skill THEN it is written; WHEN it drops a file THEN the file is
  deleted if it still equals its base, and kept and listed if not.
- **S24** WHEN the new version drops a whole skill THEN its files are removed as in S23, and directories left empty
  under `<target dir>/<name>/` are removed, including `<name>/` itself.
- **S25** WHEN a skill file differs from its base and the new version doesn't change it THEN it is kept, and the
  update's output lists it as drifted with the `cg extension resolve` options. Nothing is recorded for it: drift is
  computed from disk (Q1).
- **S26** WHEN a skill file differs from its base and the new version changes it THEN it is kept and recorded as
  pending, as notes are today; `cg extension diff`, `resolve --merge|--keep|--take` work on it unchanged.
- **S27** WHEN a drifted file's extension version changes in a later update THEN it becomes pending, and any
  acknowledgement recorded for it (S32) goes.

### Drift: shown, acknowledged, restored

- **S28** WHEN `cg extension list` runs (text and `--json`) THEN each extension shows its skills, the targets each is
  installed for, and the number of drifted and pending skill files; drift is computed from the files on disk at that
  moment, so an edit made after the last update shows (Q1).
- **S29** WHEN `cg extension pending` runs THEN drifted skill files are listed in their own group after the pending
  ones, with the options that apply (Q6).
- **S30** WHEN a session starts (`cg session pull-latest`) and a skill file has drifted (computed from disk, Q1) and
  that drift hasn't been shown on this machine and isn't acknowledged THEN a one-line notice names the files and
  `cg extension pending`; the next session start on the same machine doesn't repeat it, unless the file changes
  again (Q2). When another file drifts later, the notice names only the files not shown yet. The check reads the
  main checkout (where the session-start hook runs), so an edit in a session worktree shows once it has landed.
- **S31** WHEN `cg extension resolve <ext> <file> --take` runs on a drifted file THEN the extension's version is
  written back and any acknowledgement recorded for it goes.
- **S32** WHEN `cg extension resolve <ext> <file> --keep` runs on a drifted file THEN the file is left as it is and
  the drift is recorded as acknowledged for that content; it isn't listed by S29 or raised by S30 again until the
  file's content changes again or the extension ships a new version of it (S27).
- **S33** WHEN `cg extension resolve <ext> <file> --merge` runs on a drifted file THEN it is refused, saying the
  extension has no change to merge and naming `--keep` and `--take`.
- **S34** WHEN a drifted file was deleted and `--take` runs THEN the file is restored.
- **S35** WHEN pending notes or templates exist alongside drifted skill files THEN the session-start notice keeps
  raising the pending ones every session, as today (U1), and the drifted ones per S30.

### Targets setting changes

- **S36** WHEN `skills.targets` changes and `cg extension update` runs (with or without a new version) THEN each
  skill is installed under the targets now listed, and its files under a target no longer listed are removed as in
  S23 and S24 (edited ones kept and listed). An unowned, differing file at a newly listed target is refused as in
  S20.

### Remove

- **S37** WHEN `cg extension remove` runs THEN every skill file still equal to its base is deleted, directories
  left empty under each `<target dir>/<name>/` are removed, edited files are kept and listed, and the ledger with its
  acknowledgements goes.

### Moving files between `templates` and `skills`

- **S38** WHEN an update moves files from the old version's `templates` (or `notes`) to a skill in the new version
  at the same workspace paths, and the files are unedited THEN the update succeeds without `--force`, the files are
  updated to the new version, none is deleted afterwards (the orphan check counts skill files), and the ledger lists
  them under the skill, not under `templates`.
- **S39** WHEN the same move happens and the user edited one of the files and the new version changes it THEN it is
  kept and recorded as pending, not refused and not deleted.
- **S40** WHEN the same move happens and the user edited one of the files and the new version doesn't change it
  THEN it is kept and reported as drifted.
- **S41** WHEN an update moves files the other way (from a skill to `templates` at the same paths) THEN they are
  treated as cg's before the apply (the tracked check reads the old ledger's skill files) and as still shipped after
  it, so the update neither refuses nor deletes them.

### Failure mid-operation

- **S42** WHEN writing a skill file fails part-way through an install or update THEN every file written by that run
  is rolled back to what it was before (content and mode), directories that run created are removed, and the ledger
  and `.cg.json` are unchanged.

### Unchanged

- **U1** Notes and templates keep today's ownership: an edit-only change stays silent; pending is raised every
  session.
- **U2** A v1 manifest without `skills` installs, updates and is removed as today.
- **U3** The framework's own `.claude/` files, `wrap` included, stay with `cg init` and `cg sync`.
- **U4** Hook wrappers and git-hook scripts are always cg's, as today.
- **U5** cg never writes to the user-scope skill dirs.

## Cross-repo acceptance (outside this PR, released together)

- **X1** mnemcik/cg-extensions-registry: the manifest schema accepts `skills` (`[{ "src": string }]`) for
  `manifest: 2`, together with #259's change to the same schema.
- **X2** mnemcik/cg-extensions `github-digest`: `"manifest": 2`; `skills: [{ "src": "skills/github-digest" }]` replacing the
  two `templates` entries into `.claude/skills/github-digest/` (the folder moves from `templates/skill/`, so its base
  name is the skill's name; the `github-digest-target.md` template stays a template);
  `SKILL.md` refers to its script relative to the skill directory (`${CLAUDE_SKILL_DIR}` in Claude Code); the script
  committed executable and called directly; version bumped. End to end, `cg extension update github-digest` in a
  workspace that has the old version takes the move per S38.
- **X3** `docs/extensions.md` and `EXTENSIONS.md`: the contribution point; `templates` described as any workspace
  path, user-owned once copied, and no longer the way to ship a skill; skill authors refer to their own files
  relative to the skill directory; reviewers check a skill's `allowed-tools` frontmatter, which in Claude Code lets
  the agent run those tools without asking.

## Choices this spec makes (for agreement)

- **Q1 — drift is computed from disk when it's shown,** by `cg extension list`, `cg extension pending` and the
  session-start check, comparing each skill file's content id with its base. The ledger's drifted records hold only
  acknowledgements (S32). *Alternative:* drift found only at install and update, as pending is; simpler, but an edit
  made after the last update stays invisible until the next one, which is the silent case D3 rules out.
- **Q2 — "once per machine"** is a record in the git common dir (`<git-common-dir>/cg/drift-shown.json`, keyed by
  extension, file and content id), not in the committed ledger: a session start must not change committed files,
  and every worktree of the clone shares it. *Alternative:* raise drift at every session start, as pending; D12
  chose once.
- **Q3 — symlinks and submodule links in a skill's tree are refused,** detected from the git tree's modes so the
  result doesn't depend on `core.symlinks`. A symlink could make cg write or the agent read outside the skill; Agent
  Skills doesn't use them. *Alternative:* copy the link target's content.
- **Q4 — the execute bit comes from the git tree** at the pinned commit (`git ls-tree`, the same call as Q3), not
  from the checked-out file's mode, so it's the same on every platform; it is part of a skill file's identity (S13);
  on Windows the mode is written as the platform allows, and a workspace committed there is fixed on the next
  install, update or `cg init` on another platform (S13a).
- **Q5 — `skills.targets: []` is allowed** and installs no skills, for a workspace that uses no agent tool cg knows
  (for example one that declined the Claude Code integration, #238).
- **Q6 — drifted files are settled through the existing `pending`, `diff` and `resolve` commands**, not new ones;
  `diff` on a drifted file shows the user's change against the extension's version.
- **Q7 — `SKILL.md` frontmatter is checked for `name` (equal to the directory name) and a `description` of 1–1024
  characters,** the two fields the Agent Skills spec requires (https://agentskills.io/specification); nothing else
  in it is checked.
- **Q8 — the framework's names come from `frameworkClaudeFiles`:** the entries under `skills/` and under
  `commands/` (`cg-init`, `cg-sync`, `match-project` today), because in Claude Code a skill takes precedence over a
  slash command of the same name (https://code.claude.com/docs/en/skills), so an extension skill named `cg-sync`
  would hide the framework's command. Checked for the `claude` target only. A name cg adds later is covered without
  a second list.
- **Q9 — a skill moving between extensions takes one update per extension, droppers first** (S18b), and cg refuses
  a cycle of moves it can't order (S18d). Each step is an ordinary update that succeeds or fails as a whole; an
  edited file a dropper keeps has to be moved aside before the gainer installs it. *Alternative:* hand the files over
  within one update; it needs ownership kept across two extensions' ledgers while `cg extension update` saves after
  each extension, so a failure between them leaves files owned by neither, and a swap has no order. Moves are rare
  enough that two commands cost less.
