---
name: match-project
description: >-
  Match a user prompt to a project in a Consigliere workspace. Reads .cg.json and the
  project index to find the best matching project. Returns the project slug and folder path.
  Use this skill at the start of a session when the user's prompt likely relates to an existing project.
  Runs as a forked subagent to keep the lookup isolated from the main conversation context.
context: fork
user-invocable: true
allowed-tools: Read, Glob, Grep
argument-hint: "<description of what the user wants to work on>"
---

# /match-project — Project Matcher

Match a user's prompt to an existing project in a Consigliere workspace. This skill runs as an isolated subagent so its file reads do not bloat the main conversation context.

## Step 1: Guard — Verify Consigliere workspace

Read `.cg.json` from the workspace root (current working directory).

- If the file does not exist, respond exactly: `NO_MATCH: Not a Consigliere workspace. Run /cg-init to set one up.` and stop.
- If the file exists but `type` is not `"consigliere"`, respond exactly: `NO_MATCH: .cg.json exists but type is not "consigliere".` and stop.

## Step 2: Read the project index

From `.cg.json`, get the value of `indexes.projects` (default: `projects/TODO.md`). Read that file.

The project index is a markdown table with columns like: `#`, `Project`, `Status`, `Areas`, `Folder`. Parse all rows from the table.

If the index file does not exist or is empty, respond: `NO_MATCH: Project index not found or empty.` and stop.

## Step 3: Match the prompt

The user's prompt is provided in `$ARGUMENTS`. Match it against the project table using these signals (in priority order):

1. **Exact or near-exact project name match** — the prompt contains the project name or a close variant
2. **Area slug match** — the prompt mentions an area slug that appears in the project's Areas column
3. **Keyword overlap** — significant keywords from the prompt appear in the project name or description
4. **Folder name match** — the prompt contains the project's folder slug

### Scoring

- If exactly one project matches clearly, proceed to Step 4.
- If multiple projects match, read the first 10 lines of each candidate's `README.md` (the Problem and Goals sections) to disambiguate. Pick the best match.
- If still ambiguous after reading READMEs, return the top 2-3 candidates (see Step 4 format for multiple matches).

## Step 4: Return the result

### Single match

Respond with exactly this format (no extra text):

```
MATCH: {project-name}
SLUG: {folder-slug}
PATH: projects/{folder-slug}/
STATUS: {project-status}
```

### Multiple candidates (ambiguous)

```
AMBIGUOUS: {number} candidates
CANDIDATE: {name-1} | SLUG: {slug-1} | PATH: projects/{slug-1}/ | STATUS: {status-1}
CANDIDATE: {name-2} | SLUG: {slug-2} | PATH: projects/{slug-2}/ | STATUS: {status-2}
```

### No match

```
NO_MATCH: No project matches the prompt "{first 50 chars of $ARGUMENTS}..."
```

## Important

- Do NOT read full project files beyond the first 10 lines of README.md for disambiguation.
- Do NOT output any explanation, commentary, or markdown formatting — only the structured output above.
- Keep the lookup fast and minimal. The whole point of this skill is to avoid loading unnecessary context.
