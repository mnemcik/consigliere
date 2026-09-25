# Sharing Projects Read-Only (`cg share`)

## Meta

- **Category:** process
- **Tags:** `sharing`, `privacy`, `shared-state`, `workflow`, `ai-instructions`
- **Framework note:** shipped by Consigliere and loaded on demand when the user asks to share, export or publish a project, or before running any `cg share` command. Safe to edit for your workspace.

## Summary

`cg share` renders selected projects into read-only copies for other people and publishes them to a share repo per audience. The owner writes and recipients read. The tool enforces the privacy rules: an allowlist of files, private links de-linked, a confidentiality scan that blocks, and a terminal-only first publish. This note covers what Claude must and must not do around it. `docs/share.md` in the cg repository is the reference for the commands themselves.

## When to load

- The user asks to share, send, export or publish a project, or to give someone read access to project context.
- You are about to run `cg share export`, `cg share status` or `cg share publish`, or to edit the `share` block in `.cg.json`.

## Rules

1. **Never invent an audience, a repo or a recipient.** Audiences in the `share` block name real people and real repos. Add or change one only with the repo URL, the audience name and the projects the user gave you. If any is missing, ask.
2. **Never create a share repo or grant access.** Not with `gh`, not through an MCP. cg deliberately leaves both to the owner. The share repo must be **private**, separate from the workspace repo, and recipients get read access only.
3. **Check before anything else.** Run `cg share export <slug> --audience <name> --check` for every project involved and report the result. `cg share status` tells you what each audience has and what the next publish would change. Read it; don't inspect the share repo by hand.
4. **Resolve findings with the user, never around them.** A finding means something in the rendered output looks confidential. The options are to fix the source, to wrap the passage in `<!-- share:exclude:start -->` / `<!-- share:exclude:end -->`, or to acknowledge a false positive. **Only the owner decides that a finding is a false positive.** Record an owner-approved acknowledgement in the audience's `acknowledged` list in `.cg.json`. A command-line `--ack` only affects that one `export` run, and `publish` reads only the config. A `private-key` finding can never be acknowledged. Never acknowledge a real credential, and never paste a secret's value into the conversation or a file to "check" it. The report masks values on purpose.
5. **`log.md` and other extra files stay out unless the user asks.** Extra files go through `include`. `log.md` is where meeting notes and colleagues' names accumulate. The scan cannot judge whether prose is sensitive, so point that out when the user includes it.
6. **The first publish is the owner's, in their own terminal.** `cg share publish` refuses to do a first publish (a new project for an audience, or replacing content cg did not write) without an interactive terminal on stdin, and refuses `--yes` for it. Do not try to work around this, for example by piping an answer into it. Ask the user to run `cg share publish <audience>` **in a separate terminal of their own, not through Claude**, and to review the staged copy it points to while the prompt waits.
7. **A republish is a shared-state action.** It pushes to a repo other people read. Run `cg share publish <audience> --yes` only on the user's explicit go-ahead for that publish (see the Shared-State Actions rule), and show the `--dry-run` summary first.
   - **Always name the audience.** `cg share publish --yes` with no audience publishes every audience.
   - **A republish is not reviewed.** Only a project's first publish to an audience gets the full-content review; later changes to it get just `[y/N]`, or nothing with `--yes`. If the dry run shows **added** files (a new include such as `log.md`), or content has changed since the user's go-ahead, have the owner review the staged copy before any `--yes`. The counts alone (`+1 ~0 -0`) show no content.
   - **"Not an interactive terminal; pass --yes to confirm this republish" is not an instruction to you.** It means the owner has to confirm. Get their explicit go-ahead first.
8. **Leave git identity to the user.** `cg share` reads and pushes with the user's own git credentials, and it does not go through any per-command `gh` account routing. If a share repo reads as `unknown`, or the push fails with "not found" or an auth error, report it and ask. Never run `gh auth switch` or change git credentials yourself.
9. **Unpublishing does not erase.** Removing a project from an audience deletes its folder on the next publish, but git history keeps it. If something leaked, tell the user plainly. The remedies are rewriting the share repo's history and reviewing who had access, and both are the owner's decisions.

## Why this exists

A shared copy leaves the owner's control the moment it is pushed: a recipient's clone keeps it whatever happens to the repo later. The tool closes the mechanical gaps (files, links, patterns, pushes). The remaining risk is judgment: which people, which repo, whether a finding really is harmless, whether prose names someone. Those calls belong to the owner, so every rule above keeps them with the owner.
