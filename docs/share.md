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

Status: `cg share export` (render + scan) is available. Publishing to a share
repo per audience (`cg share publish`, `cg share status`, and the `share` block
in `.cg.json`) is planned.

## `cg share export`

```
cg share export <slug> --out <dir> [--include <file>]... [--owner <name>] [--ack <rule:hash>]...
cg share export <slug> --check [--include <file>]... [--ack <rule:hash>]...
```

Renders `projects/<slug>/` into `<dir>/<slug>/`. `<dir>` must not exist or must
be empty. `--check` renders and scans without writing anything.

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
| `token` | GitHub, Slack, AWS and `sk-` API key formats; Slack webhook URLs |
| `private-key` | `-----BEGIN … PRIVATE KEY-----` |
| `jwt` | three-part `eyJ…` tokens |
| `credential-assignment` | `secret=`, `api_key:`, `password=` and similar, when the value is high-entropy and not a placeholder such as `<your-key>` or `${VAR}` |
| `local-path` | paths naming a user account: `/Users/<name>`, `/home/<name>`, `C:\Users\<name>` |
| `vault-reference` | 1Password references `op://<vault>/<item>…` |
| `placeholder` | leftover template placeholders such as `{Project Title}`, and `[You]` |
| `denylist` | owner-defined terms (customer names, codenames); configured with the planned `share` block |

The rules match values, not words: *"send the client_id/secret via API
management"* describes a credential without containing one, and passes.
Conventional paths such as `~/.config` identify neither the owner nor the
machine, and are not flagged. Secret matches are masked in the report.

Resolve each finding by fixing the source, wrapping the passage in exclusion
markers, or, for a false positive, acknowledging it with the `rule:hash` the
report prints:

```
cg share export my-project --check --ack local-path:6c54c54b471a3e54
```

An acknowledgement names one rule and one line. Editing the line brings the
finding back.

No pattern can judge whether prose is sensitive, such as a colleague's name in
meeting notes. That is covered by `log.md` being opt-in and, once publishing
lands, by requiring the owner to review the full content on the first publish
to each audience.
