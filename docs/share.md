# `cg share` — read-only project sharing

`cg share` renders a workspace project into a copy that can leave the
workspace. The owner writes; recipients only ever read the copy. Nothing is
shared back.

A workspace mixes project content with material that must stay private: the
profile, insights, other projects, and personal notes that projects link to.
Sharing the workspace repo itself would expose all of it, since git hosts have
no per-path read permission. `cg share` therefore exports one project at a
time, keeps only what was meant to leave, and stamps the result as a mirror of
the owner's copy.

The commands are `cg share export` (render + scan), `cg share status` and
`cg share publish`, configured by the `share` block in `.cg.json`.

## Setting up an audience

1. Create a **private** repo for the audience yourself, and give each
   recipient the read role. `cg` never creates repos or grants access, and the
   recipients should not be able to write: `cg` owns the repo's whole tree.
2. Add the audience to the `share` block (below), naming the repo and the
   projects it gets.
3. Run `cg share export <slug> --audience <name> --check` for each project and
   resolve every finding.
4. Run `cg share publish <name>` **yourself, at a terminal**, and review the
   staged copy it points to before confirming.

## Configuration: the `share` block

Sharing is configured in `.cg.json`. The block holds no state: what has been
published is read from each share repo, never stored in the workspace.

```json
"share": {
  "owner": "Ada Owner",
  "denylist": ["acme corp", "project-bluebird"],
  "audiences": {
    "prdcfg-team": {
      "repo": "git@github.com:<org>/<share-repo>.git",
      "branch": "main",
      "authorName": "Ada Owner",
      "authorEmail": "ada@example.com",
      "projects": {
        "product-config-api": {
          "include": ["value-serialization-analysis.md"],
          "acknowledged": [{ "rule": "local-path", "hash": "6c54c54b471a3e54" }]
        },
        "product-config-ui": {}
      }
    }
  }
}
```

| Field | Meaning |
|---|---|
| `owner` | Display name in the mirror header; `--owner` overrides it, git `user.name` is the fallback. |
| `denylist` | Terms that must never leave, matched case-insensitively in every export, with or without an audience. |
| `audiences.<name>` | One set of recipients. Names are lowercase letters, digits, `.`, `_` and `-`. |
| `repo`, `branch` | The share repo cg owns, and the branch published to (default `main`). |
| `authorName`, `authorEmail` | Author of publish commits, e.g. a work identity on a machine whose global git identity is personal. |
| `projects.<slug>.include` | Files beyond the default allowlist, as `--include`. |
| `projects.<slug>.acknowledged` | Findings judged safe, as `--ack`; copy `rule` and `hash` from the report. |

A malformed block is an error for every `cg share` command, so a typo never
silently shares less or more than meant. That covers:

- an audience without a repo or projects;
- a bad audience name, project slug or branch name;
- an empty include entry or an incomplete acknowledgement;
- two audiences on the same repo and branch (each would read the other's projects as removed). URLs are compared case-insensitively; local paths and `file://` URLs are compared exactly, so on a case-insensitive filesystem two spellings of one local path are not detected;
- an audience pointing at the workspace's own repo.

`.cg.json` itself never leaves the workspace: it is not in any project folder.

## `cg share export`

```
cg share export <slug> --out <dir> [--audience <name>] [--include <file>]... [--owner <name>] [--ack <rule:hash>]...
cg share export <slug> --check [--audience <name>] [--include <file>]... [--ack <rule:hash>]...
```

Renders `projects/<slug>/` into `<dir>/<slug>/`. `<dir>` must not exist or must
be empty. `--check` renders and scans without writing anything.

`--audience <name>` renders the project as shared with that audience: the
project must be one it shares, its `include` and `acknowledged` entries are
merged with the flags, and links to the audience's other projects are
rewritten to their published copies instead of being de-linked. Only a
sibling that would itself publish cleanly gets live links: one with no row in
the project index, uncommitted changes, open scan findings or a broken include
or marker is left out, and links to it stay private rather than dangling.
Each sibling is judged with the links it would really get, since those can
change its findings (a rewritten link keeps its `#fragment`), so the set of
publishable siblings is settled by repeatedly dropping the ones that fail.
This errs on the side of privacy: a sibling dropped early stays private even
if it would pass against the final set.

The project folder and the project index must have no uncommitted changes, so
the stamped source commit always describes exactly what was exported.

### What leaves

- **Files are allowlisted.** `README.md`, `decisions.md` and `todo.md` leave by
  default. Anything else, `log.md` included, leaves only when named with
  `--include`. `log.md` is opt-in because it is where meeting notes and names
  accumulate. `resume.md` (the `/wrap pause` cursor) never leaves, in any
  letter case, even when included. Files must be regular files inside the
  project folder, spelled exactly as on disk (a case-insensitive filesystem
  would otherwise let `Readme.md` open `README.md`); symlinks are refused.
- **Sections can be excluded.** Content between these markers, each on its own
  line, is removed:

  ```markdown
  <!-- share:exclude:start -->
  Anything here stays in the workspace.
  <!-- share:exclude:end -->
  ```

  A nested, stray or unclosed marker stops the export instead of guessing.
- **Frontmatter is dropped.** It is a derived copy of the index metadata and
  carries fields such as priority.
- **README's `## Meta` is rebuilt** from the project index, which is the
  authoritative source: Status and Areas, plus Started, Output type and Origin
  from the README. Origin survives only when it is external (a ticket key or
  URL). Priority, Repository and every other field are dropped.
- **`[You]`** is replaced with the owner: `--owner`, else the `share` block's
  `owner`, else git `user.name`.

### Links

| Target | Result |
|---|---|
| A file in this export | kept, rewritten relative to the published layout |
| Anything else in the workspace (areas, notes, ideas, other projects, non-included files) | de-linked: the text stays, the target is dropped, `*(private)*` is appended |
| A URL or a `#fragment` | kept |
| A `file:` URL or a local path | de-linked, like any private target |
| A path escaping the workspace | the export stops |

Inline links, images, angle-bracket targets, titles, a linked image and
reference definitions are handled; a private reference definition is dropped.
Code spans and fenced blocks are left alone. If any `](` outside code still
points at a private path after rewriting, the export stops: an unrecognised
link shape can never carry a private path out.

### Mirror header

Every file starts with a read-only notice naming the owner, followed by

```
> **Authority:** mirror — <owner>'s workspace @ `<commit>` · **Verified:** <commit date>
```

The commit is the last one touching the project folder, and the date is that
commit's date, never the current time. Re-exporting an unchanged project is
byte-identical, even after unrelated commits.

### Confidentiality scan

The rendered output, not the source, is scanned, so anything the earlier steps
removed cannot trip it and anything they missed is still caught. **Any finding
stops the export before anything is written.**

| Rule | Matches |
|---|---|
| `token` | GitHub, GitLab, Slack, AWS (incl. STS), Stripe, Google, npm and `sk-` key formats; Slack webhook URLs; `Bearer <token>`; `Basic <base64 user:pass>` |
| `url-credential` | a password in a URL (`scheme://user:<password>@host`, including an empty user and an `@` inside the password) or in `curl -u user:<password>` |
| `private-key` | `-----BEGIN … PRIVATE KEY-----`, including PGP key blocks |
| `jwt` | three-part `eyJ…` tokens |
| `credential-assignment` | a credential-named key assigned a value (`name=`, `name:`), and `--password <value>`. Names are judged by their segments, split on `_`, `-` and camelCase: `SMTP_PASS`, `PGPASSWORD`, `clientSecret`, `auth_token`, SAS `sig`, and a trailing `key` (`Ocp-Apim-Subscription-Key`) count; `compass`, `assignee`, `authority` and `primary_key_column` do not. A password counts when it is 8+ characters mixing letters and digits, and runs to whitespace or a quote. Other values need 12+ characters mixing letters and digits, and either hex or entropy scaled to their length. Placeholders (`<your-key>`, `${VAR}`, `$VAR`, `%VAR%`, `***`, `...`), URLs, lowercase resource paths and, for a bare `key`, UUIDs never count |
| `local-path` | paths naming a user account: `/Users/<name>/…`, `/home/<name>/…` (also after `file://` or in a `PATH`-style list), `C:\Users\<name>` or `C:/Users/<name>` (any case) |
| `vault-reference` | 1Password references `op://<vault>/<item>…` |
| `placeholder` | leftover template placeholders such as `{Project Title}`, and `[You]` |
| `denylist` | owner-defined terms (customer names, codenames), from the `share` block's `denylist` |

The rules match values, not words: *"send the client_id/secret via API
management"* describes a credential without containing one, and passes.
Conventional paths such as `~/.config` identify neither the owner nor the
machine, and are not flagged; nor is a web route such as `GET /Users/me`.

Reports reach terminals and AI transcripts, so a secret's value is never
printed: a token shows only its public prefix (`ghp_…(40 chars)`), a password
or other value only its length (`***(17 chars)`). Other matches are cut to a
readable length. One value is reported once, even when two rules match it.
File names are scanned too, and a finding in one is reported as line 0.

Resolve each finding by fixing the source, wrapping the passage in exclusion
markers, or, for a false positive, acknowledging it with the `rule:hash` the
report prints:

```
cg share export my-project --check --ack local-path:6c54c54b471a3e54
```

An acknowledgement names one rule and the hash of one trimmed line, so it
covers every identical line in the export, in any file. Editing the line
brings the finding back, and an acknowledgement for one rule never clears
another. A `private-key` finding cannot be acknowledged: only the key's header
line is matched, so acknowledging it would let the key body through.

No pattern can judge whether prose is sensitive, such as a colleague's name in
meeting notes. That is covered by `log.md` being opt-in, and by the owner
reviewing the full staged content before the first publish of each project to
an audience (see `cg share publish`). Later changes to an already-published
project, a newly included file among them, get only a `[y/N]` confirmation, so
review the staged copy yourself when the summary shows added files.

## `cg share publish`

```
cg share publish [<audience>] [--dry-run] [--yes]
```

Publishes every project an audience shares (all audiences, or the one named)
to its share repo. For each audience:

1. **Every shared project is rendered and scanned.** If any cannot be
   exported or has open findings, nothing is published for that audience: a
   share repo is published whole or not at all.
2. **The share branch is cloned** into a temporary directory, or started
   fresh if it does not exist yet (an empty repo, or a new branch).
3. **Safety checks.**
   - A share repo that is this workspace or a fork of it is refused, however
     its URL is spelled, which the configuration check cannot do. Every
     branch and tag of the share repo is checked (commits only, no file
     contents), so it doesn't matter which branch it's on. Two signals: a root
     commit shared with the workspace, or a branch or tag tip the workspace
     already has. A shallow workspace is refused, since its real roots are
     unknown (`git fetch --unshallow` first).
   - A manifest written for another audience is refused.
   - A branch whose tip lacks cg's `Cg-Share-Publish` commit trailer has
     commits cg did not make. If it was published before, publishing stops
     and cg never force-pushes; resolve it in the share repo. If it was never
     published (a repo created with a README, say), its files are listed and
     will be replaced.
4. **The whole tree is reproduced:** a generated `README.md` index,
   `.cg-share.json`, and one `<slug>/` folder per project. A project no longer
   shared is deleted from the tree, but **git history still has it**. If
   something leaked, deleting it is not enough: rewrite the share repo's
   history and review who had access.
5. **A summary** lists each project as new, changed (`+added ~changed
   -removed`) or unchanged, plus removed projects and replaced files. An
   unchanged audience publishes nothing.
6. **Consent.**
   - The first publish of a project to an audience, or one that replaces
     content cg did not write, must be confirmed **at an interactive terminal**
     by typing the audience name. Review the staged copy first. `--yes` is
     refused for it. When Claude drives the session, run it yourself in a
     separate terminal of your own, not through Claude: the prompt needs a
     real terminal on stdin.
   - Later publishes ask `[y/N]`, or accept `--yes`.
   - `--dry-run` stops before committing.
7. **Commit and push.** The commit is authored by the audience's
   `authorName`/`authorEmail`, else this workspace's `user.name`/`user.email`
   (the name falls back to the owner; a missing email is refused), and carries
   the `Cg-Share-Publish` trailer. Your global git hooks and commit signing do
   not run in cg's clone: a hook could drop the trailer (which would lock you
   out of the next publish) or block the push, and signing could prompt. A
   share repo that requires signed commits cannot be published to yet. A
   global gitignore does not affect what is published, and the staged tree is
   checked against the rendered files before anything is committed. Ctrl-C at
   the prompt cancels cleanly and removes the staged copy. The push is
   fast-forward only: if someone published meanwhile, it is rejected, and you
   run `cg share status` and retry.

## `cg share status`

```
cg share status [<audience>]
```

Read-only. For every audience (or the one named), renders and scans each
shared project and compares it with the share repo's manifest
(`.cg-share.json` on the audience's branch):

| State | Meaning |
|---|---|
| `up to date` | the published copy matches what would be published now |
| `stale` | the next publish would change it |
| `not published` | the audience shares it, the share repo does not have it yet |
| `blocked` | open scan findings; run `cg share export <slug> --audience <name> --check` |
| `error` | the export cannot run, e.g. uncommitted changes in the project |
| `removed` | published, but no longer in the config; the next publish deletes it |
| `unknown` | the share repo could not be read (the error is shown); a project that is `blocked` or `error` shows that instead |

Staleness compares a hash of the rendered content, not only the source
commit, so an edit that lives outside the project folder (a status change in
`projects/TODO.md`) still counts. An empty share repo, or one without the
branch, reads as nothing published. Only the branch tip is fetched, git never
prompts (credential prompts are disabled, and ssh runs in batch mode unless
you have set your own `GIT_SSH_COMMAND`, `GIT_SSH` or `core.sshCommand`), and
each read times out after 60 seconds: an unreachable or unauthorised repo
reads as `unknown` with the error shown. Credentials embedded in a repo URL
are redacted from all output. A manifest written for a different audience is
not compared against.

## When a publish is refused

| Message | What it means | What to do |
|---|---|---|
| `uncommitted changes under …` | the project folder or index has uncommitted edits, so the stamp would lie | commit them, then publish |
| `… open finding(s)` | the scan found something in a shared project | `cg share export <slug> --audience <name> --check`, then fix, exclude or `--ack` |
| `… a separate repo` | the share repo shares history with this workspace | point the audience at a new, separate repo |
| `… shallow clone …` | the workspace's real history is unknown | `git fetch --unshallow`, then publish |
| `… commits cg did not make …` | someone changed the share repo after the last publish | resolve it in the share repo; cg never force-pushes |
| `… another audience` | the branch holds a different audience's copy | give each audience its own repo or branch |
| `… was rejected …` | someone published between cg's read and push | `cg share status`, then publish again |
| `a first publish must be confirmed at an interactive terminal` | the first publish needs a person at a real terminal | run `cg share publish <name>` yourself, in your own terminal |
| `--yes is refused for a first publish` | a first publish always needs the typed confirmation | run it without `--yes`, in your own terminal |
| `not an interactive terminal; pass --yes to confirm this republish` | a republish needs a confirmation | confirm at a terminal, or pass `--yes`, and only on the owner's explicit go-ahead |
| `no author email for the publish commit` | no `authorEmail` and no workspace `user.email` | set `authorEmail` on the audience, or git `user.email` in the workspace |
| `… newer than this cg understands` | the share repo was published by a newer cg | update cg |
| `… has no row in the project index` | the shared project is not in `projects/TODO.md` | add it to the index, or remove it from the audience |
