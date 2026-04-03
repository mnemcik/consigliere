# Consigliere (cg)

A personal workspace management framework for [Claude Code](https://claude.ai/code).

Consigliere gives you a structured, AI-friendly knowledge base for tracking **projects**, **ideas**, **notes**, **areas of responsibility**, and **insights** — all in a single git repository that any AI tool can read.

## Installation

### As a Claude Code plugin

```bash
claude plugin add github:mnemcik/consigliere
```

### CLI tool

Download the binary for your platform from [GitHub Releases](https://github.com/mnemcik/consigliere/releases), or build from source:

```bash
go install github.com/mnemcik/consigliere@latest
```

The binary is a single, self-contained executable with all templates embedded — no runtime or dependencies needed.

## Quick Start

```bash
mkdir my-workspace && cd my-workspace
cg init
```

This creates the full workspace structure:

```
my-workspace/
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
```

Then:
1. Edit `PROFILE.md` with your role and responsibilities
2. Edit the `Purpose` and `Area Categories` sections in `CLAUDE.md`
3. Define your first area in `areas/`
4. Start capturing ideas and creating projects

## CLI Commands

### `cg init [--force]`

Bootstrap a new workspace. Creates directories, templates, index files, and governance files. Safe to run in existing directories — skips files that already exist.

```bash
cg init           # Set up a new workspace
cg init --force   # Re-initialize (preserves CLAUDE.md and PROFILE.md)
```

### `cg match <prompt>`

Match a prompt to an existing project. Returns structured output for programmatic use.

```bash
cg match "OAuth identity provider strategy"
# MATCH: OAuth & Identity Provider Strategy for VIPS
# SLUG: oauth-idp-strategy
# PATH: projects/oauth-idp-strategy/
# STATUS: In Progress
```

### `cg status`

Show a quick workspace overview — project count, areas, ideas, notes.

```bash
cg status
# Consigliere workspace (v1.0.0)
#
# Projects: 11 total, 11 active
# Areas:    10
# Ideas:    4
# Notes:    12
```

### `cg version`

Print the installed version.

## Claude Code Skills

The plugin also provides slash commands that wrap the CLI:

- **`/cg-init`** — runs `cg init`
- **`/match-project`** — runs `cg match` with LLM fallback for fuzzy matching

## Core Concepts

### Areas
Domains of knowledge and responsibility — your systems, services, practices, and platforms. Reference hubs that projects, ideas, and notes link to.

### Projects
Each project is a folder: `README.md` (current state), `decisions.md` (append-only log), `todo.md` (actions), `log.md` (activity history).

### Ideas
Lightweight captures: `raw` → `exploring` → `ready` → project (or `parked`/`rejected`).

### Notes
Session findings by category: tool gotchas, workflow, architecture, process, research, troubleshooting.

### Insights
Draft observations about how you work with AI. Never applied as rules until you promote them to CLAUDE.md.

## CLAUDE.md Sections

The generated CLAUDE.md uses HTML comment markers to separate **framework sections** (`<!-- cg:section:start=X -->`) from **user sections** (`<!-- user:section:start=X -->`). Framework sections can be updated by future versions; user sections are never touched.

## Building

```bash
go build -ldflags "-X github.com/mnemcik/consigliere/cmd.Version=1.0.0" -o cg .
```

Cross-compile:
```bash
GOOS=darwin  GOARCH=arm64 go build -ldflags "-X github.com/mnemcik/consigliere/cmd.Version=1.0.0" -o cg-darwin-arm64 .
GOOS=linux   GOARCH=amd64 go build -ldflags "-X github.com/mnemcik/consigliere/cmd.Version=1.0.0" -o cg-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -ldflags "-X github.com/mnemcik/consigliere/cmd.Version=1.0.0" -o cg-windows-amd64.exe .
```

## License

MIT
