---
description: >-
  Match a user prompt to a project in a Consigliere workspace. Reads .cg.json and the
  project index to find the best matching project. Returns the project slug and folder path.
  Use this skill at the start of a session when the user's prompt likely relates to an existing project.
allowed-tools: Bash, Read
argument-hint: "<description of what the user wants to work on>"
---

# /match-project — Project Matcher

This skill invokes the `cg` CLI tool for deterministic project matching, with LLM fallback for fuzzy cases.

## Step 1: Run deterministic match

```bash
cg match $ARGUMENTS
```

If `cg` is not found in PATH, try the workspace-local binary:
```bash
./.cg/bin/cg match $ARGUMENTS
```

## Step 2: Interpret the result

The `cg match` command outputs one of three formats:

### Single match (use directly)
```
MATCH: {project-name}
SLUG: {folder-slug}
PATH: projects/{folder-slug}/
STATUS: {project-status}
```
Return this output as-is.

### Ambiguous (LLM disambiguation)
```
AMBIGUOUS: N candidates
CANDIDATE: name | SLUG: slug | PATH: path | STATUS: status
```
Read the first 10 lines of each candidate's `README.md` and pick the best match based on the user's prompt (`$ARGUMENTS`). Return the result in the single-match format.

### No match (LLM fallback)
```
NO_MATCH: ...
```
Read `.cg.json` to find the project index path, then read the index file. Use your judgment to find a match based on semantic understanding of the prompt. If still no match, return `NO_MATCH` with a reason.

## Important

- Do NOT output any explanation or commentary — only the structured match output.
- Keep the lookup fast and minimal.
