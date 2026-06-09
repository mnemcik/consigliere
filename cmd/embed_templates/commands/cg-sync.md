---
description: >-
  Reconcile a Consigliere workspace's framework content (CLAUDE.md sections and framework
  notes) with the installed cg version. Runs `cg sync`, then drives the judgment calls the
  deterministic command can't: reviewing the user's own rules against incoming framework
  rules for semantic contradictions before applying. Use when the user wants to upgrade
  their workspace content after updating the cg binary, or asks to "sync the workspace",
  "pull framework updates", or "run cg sync".
allowed-tools: Bash, Read, Edit
argument-hint: ""
---

# /cg-sync — Workspace Content Sync

This skill upgrades a Consigliere workspace's **framework-managed content** in place after
the `cg` binary has been updated. The deterministic `cg sync` command handles the mechanical
classification and the safe writes; this skill handles the judgment `cg sync` can't: deciding
whether an incoming framework rule **semantically contradicts** a rule the user wrote in their
own `user:section`, and walking the user through drift.

`cg sync` is the **content** side of an upgrade. Updating the binary itself is `cg update`
(separate). If the binary is stale, update it first, then run this skill.

## Step 1: Classify (dry run, writes nothing)

```bash
cg sync
```

If `cg` is not on PATH, try the workspace-local binary `./.cg/bin/cg sync`.

This prints each managed artifact grouped by status:

- **updatable** — framework changed it; the user never edited it. Safe to apply.
- **new** — the framework adds it. Safe to apply.
- **drifted** — the user edited a framework artifact. **Never auto-applied.** Needs a decision.
- **removed** — the framework no longer ships it. Left in place; flag for the user.
- **missing** — recorded but gone from disk. Flag for the user.

If the report says everything is up to date, stop — there is nothing to sync.

## Step 2: Review incoming framework rules against the user's own rules

Before applying, read the **incoming** framework content for the `updatable` and `new`
sections (from this `cg` version's template) and compare it against the user's own rules in
their `user:section` blocks in `CLAUDE.md`. You are looking for **semantic contradictions** —
a new or changed framework rule that tells Claude to do the opposite of something the user has
deliberately written for themselves (not mere overlap; an actual conflict).

For each contradiction you find, surface it to the user concisely:

- the framework rule (what it now says),
- the user rule it conflicts with (quote the `user:section` line),
- the consequence of applying it,
- a recommendation (usually: apply the framework update, and adjust the user's `user:section`
  to keep their override explicit — since `user:section` always wins and is never synced).

Do **not** silently apply over a contradiction. If there are none, say so and move on.

## Step 3: Apply the safe changes

Once the user is comfortable, apply:

```bash
cg sync --apply
```

This updates the untouched (`updatable`) framework sections and notes, inserts the `new`
ones, updates the manifest, and bumps the recorded framework version. It **never** touches a
`drifted` artifact.

## Step 4: Resolve drift

For each **drifted** artifact (a framework section the user edited), `cg sync --apply` left it
alone. Walk the user through it one at a time:

1. Show the user's current on-disk version and the framework's new version (read both; the
   framework version is in this `cg` build's embedded template).
2. Offer three outcomes:
   - **keep mine** — leave the user's edit; do nothing.
   - **take theirs** — replace with the framework version (the user can re-apply their tweak
     into a `user:section` afterward so it survives future syncs).
   - **reconcile** — merge the intent by hand with `Edit`, then the user owns the result.
3. Remind the user: the durable fix for "I keep editing a framework section" is to move the
   override into a `user:section` block, which `cg sync` never touches.

## Step 5: Confirm

Re-run `cg sync`. Everything except deliberately-kept drift should now report up to date.
Summarize what was applied, what was kept, and any contradictions the user should follow up on.
