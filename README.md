# Consigliere

**Your trusted advisor for managing knowledge, projects, and ideas — powered by AI.**

> *In Italian, a **consigliere** (kon-seel-YEH-reh) is a trusted counselor — the person you turn to before making a decision. In the world of AI-assisted development, Consigliere plays that role: it organizes your thinking, tracks your projects, remembers what you've learned, and makes sure nothing falls through the cracks.*

Consigliere (`cg`) is a CLI tool and [Claude Code](https://claude.ai/code) plugin that turns a plain git repository into a structured, AI-friendly workspace. It gives you a system for tracking **projects**, **ideas**, **notes**, **areas of responsibility**, and **insights** — all in markdown files that any AI tool can read and reason about.

## Why Consigliere?

Most developers accumulate knowledge across dozens of tools — Notion pages, Slack threads, scattered markdown files, browser bookmarks, mental notes. When you sit down with an AI coding assistant, all that context is invisible to it.

Consigliere solves this by giving your AI assistant a **structured knowledge base** it can actually read:

- **Start a session** and your AI already knows your active projects, open decisions, and areas of responsibility
- **Capture ideas** on the fly — they flow through a lifecycle from raw thought to active project
- **Never lose context** — session notes, technical findings, and gotchas are preserved and indexed
- **Stay organized** without overhead — the framework does the filing, you do the thinking

## Installation

### CLI (recommended)

Download the binary for your platform from [Releases](https://github.com/mnemcik/consigliere/releases) — it's a single executable, no runtime needed.

Or build from source:

```bash
go install github.com/mnemcik/consigliere@latest
```

### Claude Code plugin

```bash
claude plugin add github:mnemcik/consigliere
```

This gives you `/cg-init` and `/match-project` slash commands in addition to the CLI.

## Quick Start

```bash
mkdir my-workspace && cd my-workspace
cg init
git init && git add -A && git commit -m "Initialize workspace"
```

That's it. You now have:

```
my-workspace/
├── .cg.json                # Workspace identity
├── CLAUDE.md               # AI governance rules (framework + your customizations)
├── PROFILE.md              # Your role and context
├── areas/                  # Domains of knowledge (reference hubs)
├── projects/               # Active work (each project = a folder)
├── ideas/                  # Idea backlog
├── notes/                  # Findings and reference material
├── insights/               # Observations about your work style
└── templates/              # Templates for all item types
```

**Next steps:**

1. Edit `PROFILE.md` — tell your AI assistant who you are and what you do
2. Define your first **area** (a domain you're responsible for)
3. Create your first **project** or capture an **idea**

## How It Works

### The workspace is your AI's memory

When you open Claude Code (or any AI tool) in a Consigliere workspace, it reads `CLAUDE.md` and immediately understands:
- The workspace structure and conventions
- How to create projects, capture ideas, and take notes
- When and how to propagate information across related items
- What to do at the end of a session (capture findings, draft insights)

### Everything flows through a lifecycle

```
Idea (raw → exploring → ready) → Project (defining → in-progress → done)
                                        ↕
                               Notes, Decisions, Logs
                                        ↕
                                  Areas (reference hubs)
```

**Ideas** are lightweight captures — a sentence or two. When they mature, they become **projects** with structured folders. **Areas** are the connective tissue — domains of knowledge that projects, ideas, and notes link to instead of duplicating context.

### Your AI assistant keeps things current

The CLAUDE.md governance rules instruct AI assistants to:
- Update project files after every session
- Propagate new information to related areas and projects
- Capture technical findings as searchable notes
- Draft observations about your work style (which you review before they become rules)

## CLI Commands

| Command | Description |
|---|---|
| `cg init` | Bootstrap a new workspace |
| `cg init --force` | Re-initialize (preserves your CLAUDE.md and PROFILE.md) |
| `cg match <prompt>` | Find a project matching your description |
| `cg status` | Workspace overview — counts of projects, areas, ideas, notes |
| `cg version` | Print installed version |

### Examples

```bash
$ cg match "OAuth identity provider"
MATCH: OAuth & Identity Provider Strategy
SLUG: oauth-idp-strategy
PATH: projects/oauth-idp-strategy/
STATUS: In Progress

$ cg status
Consigliere workspace (v1.0.0)

Projects: 11 total, 11 active
Areas:    10
Ideas:    4
Notes:    12
```

## Core Concepts

### Areas

Areas are **reference hubs** — the single source of truth for a domain's systems, contacts, constraints, and current state. Think of them as the pillars of your knowledge: *"Identity & Auth"*, *"API Management"*, *"DevOps & Release"*. Every project, idea, and note links back to an area instead of duplicating context.

### Projects

Each project is a folder with a standard structure:

| File | Purpose |
|---|---|
| `README.md` | Current state, goals, scope — the source of truth |
| `decisions.md` | Append-only log with status tracking (active/superseded/reversed) |
| `todo.md` | What's next — the first place to look when picking up a project |
| `log.md` | What happened — session summaries, newest first |

### Ideas

Lightweight captures that flow through statuses: `raw` → `exploring` → `ready` → project (or `parked` / `rejected`). Low friction to capture, structured enough to act on.

### Notes

Session findings organized by category: tool gotchas, workflow patterns, architecture decisions, research, troubleshooting. Indexed and searchable.

### Insights

Draft observations about how you work with AI — prompting patterns, preferences, collaboration style. Created automatically at session end, but **never applied as rules** until you explicitly promote them. You stay in control.

## CLAUDE.md: Framework + Your Rules

The generated `CLAUDE.md` cleanly separates what Consigliere manages from what you customize:

- **Framework sections** (`<!-- cg:section:start=X -->`) — workspace rules, project structure, session-end behavior. Updated safely by future versions of Consigliere.
- **User sections** (`<!-- user:section:start=X -->`) — your purpose, area categories, git workflow, custom conventions. Never touched by Consigliere.

## Building from Source

```bash
git clone https://github.com/mnemcik/consigliere.git
cd consigliere
make build    # → ./cg binary
make test     # Run tests
make lint     # Run linters
make check    # Everything: fmt, tidy, lint, test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development details.

## License

MIT
