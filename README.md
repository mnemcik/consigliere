# Consigliere (cg)

A personal workspace management framework for [Claude Code](https://claude.ai/code).

Consigliere gives you a structured, AI-friendly knowledge base for tracking **projects**, **ideas**, **notes**, **areas of responsibility**, and **insights** — all in a single git repository that any AI tool can read.

## Installation

```bash
claude plugin add github:mnemcik/consigliere
```

## Quick Start

Open Claude Code in an empty directory (or an existing repo) and run:

```
/cg-init
```

This creates the full workspace structure:

```
your-workspace/
├── .cg.json                # Workspace identity & config
├── CLAUDE.md               # AI governance rules (framework + your customizations)
├── PROFILE.md              # Your role and context
├── areas/                  # Domains of knowledge (reference hubs)
│   └── INDEX.md
├── projects/               # Active work (each project = a folder)
│   └── TODO.md
├── ideas/                  # Idea backlog (lightweight captures)
│   └── BACKLOG.md
├── notes/                  # Session findings and reference material
│   └── INDEX.md
├── insights/               # Draft observations about your work style
│   └── DRAFTS.md
└── templates/              # Templates for all item types
    ├── idea.md
    ├── note.md
    ├── insight.md
    ├── area.md
    ├── subagent-briefing.md
    └── project/
        ├── README.md
        ├── decisions.md
        ├── todo.md
        ├── log.md
        └── references.md
```

Then:
1. Edit `PROFILE.md` with your role and responsibilities
2. Edit the `Purpose` and `Area Categories` sections in `CLAUDE.md`
3. Define your first area in `areas/`
4. Start capturing ideas and creating projects

## Skills

### `/cg-init`

Bootstraps a new Consigliere workspace. Creates directories, templates, index files, and governance files. Safe to run in existing directories — it skips files that already exist.

```
/cg-init           # Set up a new workspace
/cg-init --force   # Re-initialize (preserves CLAUDE.md and PROFILE.md)
```

### `/match-project`

Matches a prompt to an existing project. Runs as an isolated subagent so the lookup doesn't bloat your conversation context. Returns the project slug and path.

```
/match-project OAuth identity provider strategy
# → MATCH: OAuth & Identity Provider Strategy
#   SLUG: oauth-idp-strategy
#   PATH: projects/oauth-idp-strategy/
```

## Core Concepts

### Areas

Areas are domains of knowledge and responsibility — your systems, services, practices, and platforms. They serve as **reference hubs** that projects, ideas, and notes link to instead of duplicating context.

### Projects

Each project is a folder with a standard structure: `README.md` (current state), `decisions.md` (append-only log), `todo.md` (actions), and `log.md` (activity history).

### Ideas

Lightweight captures that flow through statuses: `raw` → `exploring` → `ready` → project (or `parked`/`rejected`).

### Notes

Session findings organized by category: tool gotchas, workflow patterns, architecture decisions, research, troubleshooting.

### Insights

Draft observations about how you work with AI. Created automatically at session end, but **never applied as rules** until you explicitly promote them to CLAUDE.md.

## CLAUDE.md Sections

The generated CLAUDE.md uses HTML comment markers to separate **framework sections** (managed by Consigliere) from **user sections** (yours to customize):

- **Framework sections** (`<!-- cg:section:start=X -->`) — workspace rules, project structure, session-end behavior. Updated by `/cg-update` (coming in v1.1).
- **User sections** (`<!-- user:section:start=X -->`) — your purpose, area categories, git workflow, custom conventions. Never touched by Consigliere.

## Versioning

`.cg.json` records the Consigliere version used to initialize the workspace. Future releases will include a `/cg-update` skill to update framework sections in your CLAUDE.md without touching your customizations.

## License

MIT
