# Spec — extensions contribute skills that cg manages (#300)

Implements the decisions in [analysis.md](analysis.md#qa-log): D1, D2, D4–D11, D13 as agreed on 2026-11-21, and D14
(2026-11-22), which replaces D3 and D12 and amends D8. Each scenario below is one behaviour and maps to at least one
test; `plan.md` names the commit that delivers it. The choices at the end were agreed in the spec's triage
(2026-11-22).

Builds on #259's first milestone (manifest v2, the pinned-manifest helper, the pre-apply conflict check in
`cmd/extension.go`); it is written against #259's spec as it stands, and anything that changes there before
agreement is re-checked here.

## The model in one paragraph

A skill an extension ships is **managed by cg**, like a hook: cg writes it, updates it and removes it, and nobody is
expected to edit it in place. A workspace customises a skill through **`SKILL.local.md`**, a file next to `SKILL.md`
that the workspace owns and cg never touches; the skill's own text tells the agent to read it. A direct edit to a
managed file is replaced on the next update, with one plain message; the edited version stays in git history.
Because only committed work is in git history, an update **stops** before replacing a managed file whose change
isn't committed.

## Terms

- **Skill:** a directory in the extension holding `SKILL.md` and any files below it (the Agent Skills format,
  https://agentskills.io/specification). Its **name** is the directory's base name.
- **Target:** an agent tool cg installs skills for, from a built-in table: `claude` → `.claude/skills/`, `agents` →
  `.agents/skills/`. A skill installed for a target lives at `<target dir>/<name>/`.
- **Targets setting:** `.cg.json` `skills.targets`, a list of target names; absent means `["claude"]`.
- **Managed file:** a file the extension ships in a skill, at its workspace-relative path under a target.
- **Base:** the content id (`ContentID`, the LF-normalised git blob id) of the version cg last wrote to a managed
  file, recorded in the ledger's existing `files` map, as for notes and templates.
- **Edited:** a managed file whose content on disk differs from its base, or that is missing.
- **Committed:** `git status` in the checkout cg is applying to reports nothing for the path: no staged, unstaged
  or untracked change, line endings and filters as git applies them (Q10). A path git ignores, a path flagged
  `assume-unchanged` or `skip-worktree` (for which `git status` reports nothing even when it changed), a checkout
  whose `HEAD` has no commit yet, and a workspace that isn't a git repository never count as committed.
- **Local file:** any file in `<target dir>/<name>/` that the extension doesn't ship, `SKILL.local.md` included.
- **Extension being updated:** one of the extensions a `cg extension update` run applies (not to be confused with a
  target).
- **User-scope skill dir:** the target's directory in the home directory: `~/.claude/skills/` for `claude`,
  `~/.agents/skills/` for `agents`.

## Manifest shape

```json
"manifest": 2,
"contributes": {
  "skills": [
    { "src": "skills/github-digest", "executables": ["gh-repo-watch.py"] }
  ]
}
```

`src` is a directory inside the extension; every file below it is installed. `executables` (optional) lists the
files, relative to `src`, that cg installs executable.

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
- **S6** WHEN an `executables` entry is empty, absolute, climbs out with `..`, or is listed twice THEN validation
  fails, naming the entry.

### Reading the skill at install time

These run when cg reads the extension's pinned tree, before anything is written.

- **S7** WHEN `src` isn't a directory, or has no `SKILL.md` at its top THEN install (or update, or `cg init`) fails,
  naming the skill, and the workspace is unchanged.
- **S8** WHEN `SKILL.md` has no YAML frontmatter, or its `name` differs from the directory's base name, or its
  `description` is missing, empty or longer than 1024 characters THEN install fails, naming the skill and the field
  (Q7).
- **S9** WHEN the skill's directory contains a symbolic link THEN install fails, naming the path (Q3).
- **S10** WHEN an `executables` entry names no file in the skill's directory THEN install fails, naming it.
- **S11** WHEN the skill ships a file named `SKILL.local.md` (compared without regard to case) THEN install fails:
  that name is the workspace's.
- **S12** WHEN the skill's directory holds dot-files or nested directories THEN every file in it is installed.
- **S12b** WHEN a skill is installed for the first time (no target records a base for its `SKILL.md`), or an update
  brings a `SKILL.md` whose content differs from every base recorded for it, and finds a skill whose local-file block
  (below) is missing, reworded or of an unknown version THEN it succeeds and prints one warning per skill, naming
  the skill, the extension and its repository: the skill gives this workspace no supported way to customise it;
  report it to the extension's maintainer. An update that leaves `SKILL.md` unchanged (a same-commit re-apply, or a
  new commit that only touches other files or, in a repository holding several extensions, other extensions) and
  `cg init` don't warn, nor does adding a target (S34), so the warning isn't repeated for something the workspace
  can't fix (Q12). A run that fails before its ledger is saved (S42b), and adding a target after `skills.targets` was
  `[]` (no base left), may warn again; accepted.
- **S12c** WHEN cg looks for the block THEN it takes the first line, outside fenced code blocks (CommonMark fences: three or more backticks or tildes,
  indented at most three spaces, closed by a fence of the same character at least as long; an unclosed fence runs to
  the end of the file), that reads exactly
  `<!-- cg:skill-local:start=<version> -->` (leading and trailing whitespace ignored) and the next line that reads
  `<!-- cg:skill-local:end=<version> -->` with the same version. The text between them, with carriage returns
  dropped, every run of spaces, tabs and newlines turned into one space, and the ends trimmed, must equal cg's text
  for that version. Outcomes: no such pair, or a start without its end → missing; a version cg doesn't know →
  unknown version; a text that differs → reworded; the block's text without its markers → missing; further pairs
  after the first → ignored. Tested once per outcome, plus a re-wrapped block (passes), a CRLF file (passes) and a
  block quoted inside a code fence (not counted).

The local-file block is defined in cg's code as an ordered list of versions with their texts, newest last, and quoted in `EXTENSIONS.md` and the skill
template (X3). Version `v1`:

```markdown
<!-- cg:skill-local:start=v1 -->
If a file named `SKILL.local.md` exists in this skill's directory, read it as well: its rules add to this skill's,
and replace them where the two conflict.
<!-- cg:skill-local:end=v1 -->
```

Rewording it later adds `v2`; cg keeps accepting every version it has shipped, so published skills don't start
warning. The marker form follows the framework's own `<!-- cg:section:start=ID -->` markers.

### Install

- **S13** WHEN an extension with a skill is installed and `skills.targets` is absent THEN every file of the skill is
  written under `.claude/skills/<name>/` at the same relative path, and nothing is written under `.agents/skills/`.
- **S14** WHEN `skills.targets` is `["claude", "agents"]` THEN the skill is written under both directories.
- **S15** WHEN `skills.targets` is `[]` THEN no skill is written, the install otherwise succeeds, and
  `cg extension list` shows the skills as not installed (no targets) (Q5). `[]` stays `[]` when cg saves `.cg.json`;
  only an absent key means `["claude"]`.
- **S16** WHEN `skills.targets` names a target cg doesn't know THEN install, update and `cg init` fail, naming it and
  the known targets.
- **S16b** WHEN any directory on the way from the workspace root to a managed file (the target directory,
  `<target dir>/<name>/`, or a directory inside the skill), or a directory cg is about to remove as empty, resolves
  through a symbolic link to a place outside the workspace THEN install, update, `cg init` and remove fail before
  writing or deleting anything, naming it, so cg never writes or deletes outside the workspace (U5). The check runs
  per managed path, on the longest part of it that exists, where "exists" is decided without following links
  (`Lstat`), so a link that points nowhere is refused, not skipped; directories below that part are created by cg,
  so they can't be links. A first install, where neither the target directory nor `<name>/` exists yet, passes.
- **S17** WHEN a file is listed in `executables` THEN it is written `0755`; every other managed file `0644`. On
  Windows modes are written as the platform allows (Q4).
- **S18** WHEN the ledger is saved after an install THEN it has one `skills` entry per skill: its name, the targets
  it was installed for, and the workspace-relative path of each managed file; each file's base is in the ledger's
  `files` map. A skill's files are found from that list, never by path prefix. An extension that ships only skills
  has a non-empty `files` map, so it is never read as a ledger from before content ids.

### Conflicts, refused before anything is applied

- **S19** WHEN an extension being installed ships a skill whose name another installed extension ships, for a
  target both install it for THEN install fails, naming the skill, both extensions and the target; files,
  `.cg.json` and ledgers are unchanged.
- **S20** WHEN it ships a skill whose name is a framework skill or slash command (today `wrap`, `cg-sync`, `cg-init`,
  `match-project`, from `frameworkClaudeFiles`), the targets include `claude`, and the extension doesn't already
  ship that skill for `claude` THEN install, update and a target change (S34) fail the same way, naming the
  framework (Q8). An extension **already ships a skill for `claude`** when its manifest, read and validated at its
  recorded commit, declares the skill and its ledger records `claude` among that skill's targets; an extension whose
  tree can't be read ships nothing, as #259's S9 says for commands. So adding `claude` to the targets for a reserved
  name is refused.
- **S20b** WHEN an extension already ships, for `claude`, a skill whose name is reserved (typically a framework skill
  or command a later cg added) THEN update, a target change and `cg init` don't fail: they print a warning that the
  skill hides the framework's skill or command of that name in Claude Code, naming both, and `cg extension list`
  flags it, as #259's S9 does for a command whose namespace a later cg made a built-in. A skill moved to another
  extension in one update isn't already shipped by the receiving one.
- **S20c** WHEN `cg init` restores an extension at its recorded commit and one of its skills, for a target that
  includes `claude`, has a reserved name THEN it only warns as in S20b, whatever the ledger records, since it
  restores what `.cg.json` already records, as #259's S9 does for every command `cg init` restores. `cg init` never
  refuses on a reserved name.
- **S21** WHEN the same name exists only under a target the other side doesn't install for (for example `wrap` with
  targets `["agents"]`) THEN it isn't a conflict.
- **S22** WHEN `cg extension update` (one extension or all) brings versions whose skill names conflict, with each other
  or with an extension not being updated THEN the update fails before any target is applied. When the conflict is
  with an installed extension that isn't being updated, the message adds: if a newer version of that extension drops
  the skill, update it first. It is printed as a condition: cg doesn't fetch the other extension to check.
- **S23** WHEN `cg init` re-installs extensions THEN the skill-name check runs over the whole set before the first is
  applied, as #259's command check does, and on any conflict nothing is applied. `cg init` re-installs only
  extensions whose tree is missing on this machine, as today.
- **S24** WHEN an extension's note or template destination lies inside an installed skill's directory
  (`<target dir>/<name>/`), or a skill would be installed where another extension's note or template is THEN
  install and update fail, naming both.
- **S25** WHEN a file exists at a managed file's path that cg has no record of writing and it equals the
  extension's content THEN it is adopted (recorded, mode set per S17).
- **S25b** WHEN such a file differs from the extension's content THEN install refuses, naming it, unless `--force`,
  which overwrites it; `update` and `cg init`, which have no `--force`, refuse the same way, saying to move the file
  aside and run the command again.
- **S26** WHEN a directory of the same name exists in the target's user-scope skill dir THEN install and update
  succeed and print a warning that the agent tool may use that skill instead, naming both paths;
  `cg extension list` shows it too.

### Update

- **S27** WHEN a managed file is not edited and the new version changes its content or its `executables` entry THEN
  it is rewritten with the new content and mode.
- **S27b** WHEN a managed file already holds the new version's content (or is absent and the new version drops it),
  whatever its base THEN it is adopted: recorded, mode set, no message and no stop. This comes before the edit
  checks of S30 and S31, so a run cut short after writing, or a user who applied the new version by hand, never
  meets a refusal for it.
- **S28** WHEN the new version adds a file to the skill THEN it is written; WHEN it drops a file THEN the file is
  deleted, subject to S31.
- **S29** WHEN the new version drops a whole skill THEN its managed files are deleted, subject to S31, and every
  directory left empty under `<target dir>/<name>/` is removed, including `<name>/` itself; local files and the
  directories holding them stay, and the output names each local file left behind (S33b).
- **S30** WHEN a managed file is edited and committed THEN the update writes the extension's version over it (or
  deletes it, when dropped), and its output says, once per file, in plain words: your change to `<path>` was
  replaced by the extension's version; it is in git history; to customise this skill, put your additions in
  `<target dir>/<name>/SKILL.local.md`. For a managed file that was missing and whose path `HEAD` doesn't have,
  the message says it was restored instead, without the history claim.
- **S31** WHEN a managed file is edited and not committed THEN an install, update or remove run stops before writing anything, for any
  extension being updated: the check runs in the pre-apply pass over all of them, naming each such file and saying:
  commit or discard your change to `<path>` first, then run the command again. For a path git ignores, the message
  says instead that git can't keep the change, and to move it to `SKILL.local.md` or discard it.
- **S32** WHEN a managed file's content equals its base and only its mode differs from what S17 asks THEN the
  update sets the mode, without a message.
- **S33** WHEN a local file exists in a skill's directory THEN no install, update, `cg init` or remove writes or
  deletes it.
- **S33b** WHEN a skill's directory is left behind holding local files (the skill was dropped, the extension
  removed, or the target is no longer listed) THEN the output names each local file and where it is, and for a
  target change, the directory where the skill now lives, so the user can move a `SKILL.local.md` there. cg doesn't
  move it.

### Targets setting changes

- **S34** WHEN `skills.targets` changes and `cg extension update` runs (with or without a new version) THEN each
  skill is installed under the targets now listed, and its managed files under a target no longer listed are
  deleted as in S29, subject to S30 and S31, with S33b's message.
- **S34b** WHEN a newly listed target already holds an unowned, differing file at a managed path THEN the update
  refuses it as in S25b.

### Remove

- **S35** WHEN `cg extension remove` runs THEN every managed file is deleted (an edited and committed one with the
  S30 message; an edited, uncommitted one stops the remove as in S31; an absent one is fine, S27b), directories left
  empty under each `<target dir>/<name>/` are removed, local files stay with S33b's message, and the ledger goes.

### `cg init` on a fresh clone

- **S36** WHEN `cg init` re-installs an extension at its pinned commit (its tree was missing on this machine) and a
  managed file is edited (committed or not) THEN `cg init` leaves it as it is and prints, once per file: `<path>`
  differs from the extension's version; the next `cg extension update` replaces it (or asks you to commit first).
  Unedited and missing managed files are written as usual. `cg init` never stops because of an edited skill file
  (Q11). A kept file isn't adopted: its base in the ledger stays the extension's content id, so the next update
  still sees it as edited (S30, S31).
- **S36b** WHEN `cg init` is about to re-install extensions THEN, before the first fetch and for every extension it
  is about to re-install (not those whose tree is present and are skipped), it runs
  the checks that need no fetched tree: unknown targets in `.cg.json` (S16, whatever the ledgers say), and S16b on
  each target directory and on the skill directories the ledgers record. If one fails, it stops before fetching
  anything, so after the fix a plain `cg init` run again re-installs normally. Tested as stop, fix, `cg init` again,
  skills written.
- **S36c** WHEN `cg init` stops on any error after its first tree is fetched (for example S7–S11 on a tree first read
  by this cg, S16b inside a skill directory no ledger records, S23, S25b, or another extension's fetch or path
  error) THEN the message names the cause and lists every pinned extension fetched but not applied in this run, not
  only the one that failed (legacy entries left out, since they are retried), and says for them: running `cg init`
  again won't retry them, because their trees are now on this machine; `cg extension update <name>` re-applies one
  but moves it to its latest version and records the new commit. Entries without a recorded commit (legacy) are
  retried by a plain `cg init`, as today. Tested with two extensions where one conflicts, and with A fetched and B's fetch failing.

### Skills moving between extensions

- **S37** WHEN one `cg extension update` would move skills between installed extensions (an extension's new version
  adds a skill another installed extension's new version drops) and the moves can be ordered (one move A→B, or a
  chain A→B→C) THEN it fails before any target is applied, naming each skill and both of its extensions, and
  printing the commands to run: `cg extension update <name>` for each extension taking part in a move, droppers
  first, then a plain `cg extension update` for the others (Q9).
- **S38** WHEN the moves form a cycle (a swap between two extensions, or a ring over three or more) THEN it fails
  the same way, naming the skills and extensions in the cycle, saying no order of updates can apply it, and naming
  the ways out: wait for a release of one of them that doesn't take part in the cycle, or remove one of the
  extensions (which also drops its notes, templates and pending state from cg's records) and install it again
  after the other is updated. (S37 and S38 count extensions being updated, not targets.)
- **S39** WHEN the commands S37 prints are run in order THEN each succeeds (or stops per S31 and succeeds when run
  again after the change is committed or discarded), and at the end every skill is installed by its new owner, with
  no managed file recorded in two ledgers or in none. Tested with a single move, a chain, and an uncommitted edit.

### Moving files between `templates` and `skills`

- **S40** WHEN an update moves files from the old version's `templates` (or `notes`) to a skill in the new version
  at the same workspace paths THEN the update neither refuses them as unowned nor deletes them afterwards: the
  tracked check and the orphan check count skill files. Unedited files get the new version; edited ones follow S30
  and S31 (a skill's files are managed, unlike templates). A pending record the old ledger had for such a file is
  dropped; the S30 or S31 message covers it.
- **S41** WHEN an update moves files the other way (from a skill to `templates` at the same paths) THEN they are
  treated as cg's before the apply (their bases are in `files`) and as still shipped after it: an unedited file whose
  content changes is updated, not raised as pending, and from then on they follow the templates' rules.

### Failure mid-operation

- **S42** WHEN writing or deleting a managed file fails part-way through an install or update THEN every file that
  run wrote or deleted is restored to what it was before (content and mode), directories that run created are
  removed, and the ledger and `.cg.json` are unchanged.
- **S42b** WHEN the run fails after its files are written but before the ledger is saved THEN the next run adopts
  those files (S27b) and completes, without asking the user to commit anything.

### Unchanged

- **U1** Notes and templates keep today's ownership: an edit-only change stays silent; a change on both sides is
  pending and raised every session.
- **U2** A v1 manifest without `skills` installs, updates and is removed as today.
- **U3** The framework's own `.claude/` files, `wrap` included, stay with `cg init` and `cg sync`.
- **U4** Hook wrappers and git-hook scripts are always cg's, as today.
- **U5** cg never writes to the user-scope skill dirs.
- **U6** `cg extension pending`, `diff` and `resolve` work on notes and templates as today; managed skill files never
  appear in them.

## Cross-repo acceptance (outside this PR, released together)

- **X1** mnemcik/cg-extensions-registry: the manifest schema accepts `skills`
  (`[{ "src": string, "executables": [string] }]`) for `manifest: 2`, together with #259's change to the same schema.
- **X2** mnemcik/cg-extensions `github-digest`: `"manifest": 2`; `skills: [{ "src": "skills/github-digest",
  "executables": ["gh-repo-watch.py"] }]` replacing the two `templates` entries into `.claude/skills/github-digest/`
  (the folder moves from `templates/skill/`, so its base name is the skill's name; the `github-digest-target.md`
  template stays a template); `SKILL.md` refers to its script relative to the skill directory (`${CLAUDE_SKILL_DIR}`
  in Claude Code) and calls it directly, keeping its note to run it through `python`/`py` on Windows; it carries the local-file block (S12b); version bumped. End to end,
  `cg extension update github-digest` in a workspace that has the old version takes the move per S40.
- **X3** `docs/extensions.md` and `EXTENSIONS.md`:
  - the contribution point and the model (managed, replaced with a message, uncommitted changes stop the update);
  - the **authoring rule**: every skill's `SKILL.md` carries the local-file block (S12b) near its top, word for word;
  - a **skill template**, `docs/templates/extension-skill/SKILL.md` (frontmatter with `name: extension-skill` and a
    `description`, the newest local-file block, a placeholder body), which `EXTENSIONS.md` tells authors to copy,
    renaming the folder and `name` together (S8); a test fails when the template's block isn't the newest version
    in cg's code (Q12);
  - authors have no check before publishing beyond installing the extension locally (`cg extension install <path>`),
    which runs every check here; recorded, not added in this PR;
  - skill authors refer to their own files relative to the skill directory;
  - reviewers check a skill's `allowed-tools` frontmatter, which in Claude Code lets the agent run those tools
    without asking;
  - `templates` described as any workspace path, yours once copied, and no longer the way to ship a skill.

## Choices (agreed in triage, 2026-11-22)

- **Q3 — symbolic links in a skill's directory are refused** (S9). A link could make cg write or the agent read
  outside the skill; Agent Skills doesn't use them. cg checks the checked-out tree; where a checkout turns a link
  into a plain file (Windows without symlink support), that file holds only the link's text, so nothing from
  outside the extension is copied; the platform difference (refused on macOS and Linux, a harmless text file on
  such a Windows checkout) is accepted, since reading modes from git was dropped with Q4.
- **Q4 — executables are listed in the manifest** (`executables`, S6, S10, S17), not read from file modes: the same
  on every platform, and visible to whoever reviews the manifest, as `hooks` and `git-hooks` scripts are.
- **Q5 — `skills.targets: []` is allowed** and installs no skills (S15).
- **Q7 — `SKILL.md` frontmatter is checked for the two fields Agent Skills requires:** `name` (equal to the
  directory name) and a `description` of 1–1024 characters (S3, S8).
- **Q8 — reserved names come from `frameworkClaudeFiles`** and are refused only for a skill an extension doesn't
  already ship; one it already ships warns (S20, S20b), matching #259's S9: its skills and its slash commands, since in Claude Code
  a skill takes precedence over a command of the same name (https://code.claude.com/docs/en/skills); `claude`
  target only (S20).
- **Q9 — skills moving between extensions take one update per extension, droppers first;** cg prints the order and
  refuses cycles (S37–S39).
- **Q10 — "committed" means `git status` reports nothing for the path** in the checkout cg applies to (Terms, S30,
  S31): staged-only changes count as uncommitted, since they aren't in history yet; git applies line endings and
  filters itself, so a CRLF or filtered file that is committed counts as committed. Ignored paths, an unborn `HEAD`
  and a non-repository never count. *Alternative:* treat staged changes as safe; one less refusal, but a reset
  would lose them.
- **Q11 — `cg init` leaves edited skill files alone and says so** (S36); the next `cg extension update` replaces
  them (or stops for an uncommitted one). A fresh clone already holds the committed skill files, and `cg init`
  decides what to re-install from whether the extension's tree is in the machine-wide store, which several
  workspaces share; a `cg init` that could stop on an edit could then neither be re-run (the fetched tree makes it
  skip the extension) nor clean up (deleting the tree could pull it from under another workspace). *Alternative:*
  `cg init` replaces committed edits; every machine runs the extension's version at once, but only with a change to
  how `cg init` decides what to re-install, which belongs with #259's whole-set check for `cg init` (its S11) and #306, where
  the same question arises.
- **Q12 — one local-file block, worded once in cg's code and versioned,** carried word for word by every skill: a
  skill template that a test keeps on the newest version, and a warning naming the extension's maintainer at install
  and at a version-changing update when a skill lacks the block or rewords it (S12b, S12c, X3). cg doesn't insert
  it; no scaffold command and no pre-publish check for now. *Alternative:*
  cg appends the line when it installs a skill; every skill then supports customising, but cg would be changing the
  files it installs, and the installed file would differ from the extension's.
