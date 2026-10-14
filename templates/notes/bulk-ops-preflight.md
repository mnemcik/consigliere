# Bulk / Destructive Ops — Pre-flight

## Meta

- **Category:** process
- **Tags:** `shared-state`, `safety`, `workflow`, `shell`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand before running a command that touches many entities. Safe to edit for your workspace.

## Summary

Before running a single command (or short loop) that touches **many entities**, state the target set, the exclusion set, a single-sample dry-run, the shell quoting/arity hazard you handled, and wait for explicit approval. This is the iterative-bulk axis; it pairs with **Shared-State Actions Require Per-Action Authorisation** (the merge / push / release axis). For multi-file content splices, it also holds the idempotence check: test whether the edit is already applied before matching its target. The headline trigger stays inline in `CLAUDE.md`; this note holds the pre-flight.

## When to load

About to run a command (or `for`/`xargs` loop) that touches many entities — bulk permission changes, mass invitations, mass repo/resource creation, remote-URL rewrites, multi-file rename/`sed` sweeps, `--paginate` API mutations, anything iterating over a `for X in $(...)`. Threshold is approximate: **more than ~5 entities, or any set you can't visually enumerate before the command runs.**

## Why this exists

The common shape of bulk-op incidents: the **target set was assumed**, not enumerated; the **exclusion set was implicit**, not declared; and **the command worked on one entity**, so it was assumed to work on all. Shell word-splitting on an unquoted `$(...)` expansion, a loose host/path regex, or an off-by-one arity bug then hits entities you never intended to touch — sometimes irreversibly.

## The pre-flight

Before executing the bulk op, state in the same response:

1. **Target set** — the exact list (or count + selector + first 3 / last 3 samples) of entities the command will touch. For pagination-driven sources, run the `--dry-run` / `--limit 1` equivalent first and show the live count.
2. **Exclusion set** — entities you are deliberately **not** touching, and why. Default exclusions to surface even if unmentioned: org owners / admins (permission changes); protected branches (`main`, release branches); archived / read-only repos; bots / service accounts that look like users; default-branch refs (tag/branch rewrites); anything the user previously asked to leave alone.
3. **Single-sample dry-run** — run the exact command on **one** target and show the output (or diff/effect). For irreversible ops (delete, force-push, mass-message, permission downgrade), this is mandatory.
4. **Quoting / arity check** — for shell loops, name the splitting hazard you handled (`IFS=$'\n'`, `"$var"` quoting, `read -r`, `mapfile`, a `--jq` filter instead of `jq` piped to a loop). For scripts taking arguments, confirm the positional / flag semantics.
5. **Approval gate** — wait for explicit `go` / `run it`. Silence is not approval (per the Shared-State Actions rule).

Only then run the full batch.

## Tooling tells

- A paginated API listing piped to `xargs` or `while read` — high risk; quote and validate.
- `for X in $(...)` splits differently across shells (notably bash vs zsh) — test on the actual shell, and prefer a quoted array / `mapfile` / `--jq`.
- Mass-edit verbs — repo/membership edits, `git remote set-url`, `find -exec`, `sed -i` over a glob — all qualify as bulk-op vectors.
- A `--dry-run` flag in your own scripts is cheap insurance even for one-shot scripts.

## Re-runnable content edits — test the post-state first

A multi-file splice (a `sed -i` sweep, a scripted find-and-replace, a loop of text edits) is usually written so it can be re-run safely. **An exactly-once match check does not make it safe** when the target text is contained in its own replacement. For such an edit, check whether the result is already present, not just that the target matches once.

The exposed shape:

- inserting a section **above** an existing heading, where that heading is the anchor;
- appending rows to a table that you also matched on to find it;
- adding a line after an existing line you matched on.

After the first run, the target still occurs exactly once, now inside the text just inserted. So a check like `count(old) == 1` passes on a re-run, and the block is applied again. Nothing downstream catches the duplicate: a repeated section parses fine and reads plausibly.

The order that works:

1. **Is the whole replacement block already present?** Then the edit is already applied. Report the no-op and stop. Do not look at the target. Test for the full, distinctive block, not a short token that may appear elsewhere.
2. **Otherwise**, assert that the target occurs exactly once, then replace it.

Checking only the target count is what fails. A plain substitution whose target does not survive in its replacement (`s/old-name/new-name/`) is not exposed: after one run the target is gone, so the count check is enough, and a post-state check could wrongly skip a file where the new text already occurs for another reason.

**Prefer stable delimiters to body text.** Replacing everything between two fixed markers (such as the `cg:section` sentinels in `CLAUDE.md`) is idempotent by construction, because the match never depends on the content being replaced. A one-off `sed -i` or string replace has no such structure, so it needs the explicit check above.

**Re-read before re-running.** A script that was safe when it was written stops being safe once its own output is in the file it edits.

## Reversibility note

If the op is **irreversible** (message sent, invite sent, file deleted with no VCS, key rotated, role downgraded with an audit-log gap), the steps above are non-negotiable. If it's reversible (a commit on a feature branch, config you can re-apply), the user may shortcut steps 3–4 by saying "just run it" — note in your response that you're skipping under their direction.

## Related

- `CLAUDE.md` → **Shared-State Actions Require Per-Action Authorisation** — the merge / push / release axis. This note covers the iterative-bulk axis.
- `CLAUDE.md` → **Apply Uncontroversial Review Findings** — applying a batch of review findings is itself a bulk op.
