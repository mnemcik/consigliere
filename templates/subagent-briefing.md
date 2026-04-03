# Subagent Briefing Template

Use this template to compose a prompt for dispatching a Claude Code subagent to an external repository. Fill in each section, omitting sections marked optional if not relevant.

---

## Task

What specifically to do. Be concrete: "review X for Y", "add endpoint Z", "fix bug where A happens when B".

## Context

Relevant excerpts from the project file and area file(s). Not the whole file — only sections that matter for this task. Include:
- Problem statement or goal this task serves
- Architecture constraints or decisions that affect the work
- Related issue if applicable

## Repo & Location

- **Repo:** `~/source/{repo-name}`
- **Focus files:** specific files or directories to work in
- **CLAUDE.md:** note if the repo has its own CLAUDE.md the agent should follow

## Constraints

- Read-only or write task?
- Naming conventions, coding standards, or patterns to follow
- Compliance or security requirements from the area file
- What NOT to change

## Output Expectations

What the subagent should produce:
- A structured report (for review tasks)
- A commit with specific changes (for write tasks)
- A PR (for changes that need team review)
- A diff for human review before committing

## Report Back (optional)

What information to return for capture in the project file:
- PR URL, files changed, blockers encountered
- Decisions that need escalation
- New information discovered that affects the project scope

## Severity / Priority Guidance (optional)

If the task involves finding issues, define what counts as critical vs. minor so the agent can prioritize its report.
