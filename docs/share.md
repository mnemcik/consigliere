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

Status: `cg share export` (render + scan), the `share` block in `.cg.json`
and `cg share status` are available. Publishing to the share repo
(`cg share publish`) is planned; until then a share repo is only read.

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

A malformed block (an audience without a repo or projects, a bad name, an
incomplete acknowledgement) is an error for every `cg share` command, so a
typo never silently shares less or more than meant. `.cg.json` itself never
leaves the workspace: it is not in any project folder.


```
cg share export <slug> --out <dir> [--include <file>]... [--owner <name>] [--ack <rule:hash>]...
cg share export <slug> --check [--include <file>]... [--ack <rule:hash>]...
```

Renders `projects/<slug>/` into `<dir>/<slug>/`. `<dir>` must not exist or must
be empty. `--check` renders and scans without writing anything.

`--audience <name>` renders the project as shared with that audience: the
project must be one it shares, its `include` and `acknowledged` entries are
merged with the flags, and links to the audience's other projects are
rewritten to their published copies instead of being de-linked.

The project folder and the project index must have no uncommitted changes, so
the stamped source commit always describes exactly what was exported.

### What leaves

- **Files are allowlisted.** `README.md`, `decisions.md` and `todo.md` leave by
  default. Anything else, `log.md` included, leaves only when named with
  `--include`. `log.md` is opt-in because it is where meeting notes and names
  accumulate. `resume.md` (the `/wrap pause` cursor) never leaves, even when
  included. Files must be regular files inside the project folder; symlinks are
  refused.
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
- **`[You]`** is replaced with the owner: `--owner`, else git `user.name`.

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
| `denylist` | owner-defined terms (customer names, codenames); configured with the planned `share` block |

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
meeting notes. That is covered by `log.md` being opt-in and, once publishing
lands, by requiring the owner to review the full content on the first publish
to each audience.

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
| `unknown` | the share repo could not be read (the error is shown) |

Staleness compares a hash of the rendered content, not only the source
commit, so an edit that lives outside the project folder (a status change in
`projects/TODO.md`) still counts. An empty share repo, or one without the
branch, reads as nothing published.
