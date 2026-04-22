<h1 align="center">Consigliere</h1>

<p align="center">
  <strong>Your trusted advisor for managing knowledge, projects, and ideas — powered by AI.</strong>
</p>

<p align="center">
  <a href="https://github.com/mnemcik/consigliere/releases"><img src="https://img.shields.io/github/v/release/mnemcik/consigliere?style=flat-square&color=e2b714" alt="Release"></a>
  <a href="https://github.com/mnemcik/consigliere/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/mnemcik/consigliere/ci.yml?style=flat-square&label=CI" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/built%20with-Go-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
</p>

---

> 🇮🇹 *In Italian, a **consigliere** (kon-seel-YEH-reh) is a trusted counselor — the person you turn to before making a decision. Consigliere plays that role for your AI-assisted workflow: it organizes your thinking, tracks your projects, remembers what you've learned, and makes sure nothing falls through the cracks.*

---

## 🤔 Why Consigliere?

Most developers accumulate knowledge across dozens of tools — Notion pages, Slack threads, scattered markdown files, browser bookmarks, mental notes. When you sit down with an AI coding assistant, **all that context is invisible to it**.

Consigliere solves this by giving your AI assistant a **structured knowledge base** it can actually read:

- 🧠 **Start a session** and your AI already knows your active projects, open decisions, and areas of responsibility
- 💡 **Capture ideas** on the fly — they flow through a lifecycle from raw thought to active project
- 📝 **Never lose context** — session notes, technical findings, and gotchas are preserved and indexed
- 🗂️ **Stay organized** without overhead — the framework does the filing, you do the thinking

## 📦 Installation

### CLI (recommended)

Download the binary for your platform from [Releases](https://github.com/mnemcik/consigliere/releases) — it's a **single executable, no runtime needed**.

Or build from source:

```bash
go install github.com/mnemcik/consigliere@latest
```

### Claude Code slash commands

`cg init` installs `/cg-init` and `/match-project` into the workspace's `.claude/commands/` directory. No separate plugin install step.

## 🚀 Quick Start

```bash
mkdir my-workspace && cd my-workspace
cg init
git init && git add -A && git commit -m "Initialize workspace"
```

That's it. You now have a complete workspace:

```
my-workspace/
├── 🔧 .cg.json              # Workspace identity
├── 📜 CLAUDE.md              # AI governance rules
├── 👤 PROFILE.md             # Your role and context
├── 🏛️ areas/                 # Domains of knowledge
├── 📁 projects/              # Active work
├── 💡 ideas/                 # Idea backlog
├── 📝 notes/                 # Findings & reference
├── 🔍 insights/              # Work style observations
└── 📋 templates/             # Templates for all items
```

**Next steps:**

1. ✏️ Edit `PROFILE.md` — tell your AI assistant who you are
2. 🏛️ Define your first **area** (a domain you're responsible for)
3. 💡 Create your first **project** or capture an **idea**

## ⚙️ How It Works

### The workspace is your AI's memory

When you open Claude Code in a Consigliere workspace, it reads `CLAUDE.md` and immediately understands the workspace structure, conventions, and how to keep things organized. No setup, no prompting — it just works.

### Everything flows through a lifecycle

```
  💡 Idea                              📁 Project
 ┌─────────────────────┐    ┌──────────────────────────────┐
 │ raw → exploring → ready ──→ defining → in-progress → done │
 └─────────────────────┘    └──────────┬───────────────────┘
                                       │
                            ┌──────────┴───────────┐
                            │  📝 Notes & Decisions  │
                            │  🏛️ Areas (ref hubs)   │
                            └──────────────────────┘
```

**Ideas** are lightweight captures. When they mature, they become **projects** with structured folders. **Areas** are the connective tissue — domains of knowledge that everything links to.

### Your AI keeps things current

The CLAUDE.md rules instruct AI assistants to:
- ✅ Update project files after every session
- 🔄 Propagate information to related areas and projects
- 📝 Capture technical findings as searchable notes
- 🔍 Draft work style observations (you review before they become rules)

## 🛠️ CLI Commands

| Command | Description |
|---|---|
| `cg init` | 🏗️ Bootstrap a new workspace |
| `cg init --force` | 🔄 Re-initialize (preserves CLAUDE.md and PROFILE.md) |
| `cg match <prompt>` | 🔍 Find a project matching your description |
| `cg status` | 📊 Workspace overview |
| `cg version` | ℹ️ Print installed version |

### Examples

```bash
$ cg match "OAuth identity provider"
MATCH: OAuth & Identity Provider Strategy
SLUG: oauth-idp-strategy
PATH: projects/oauth-idp-strategy/
STATUS: In Progress
```

```bash
$ cg status
Consigliere workspace (v1.0.0)

Projects: 11 total, 11 active
Areas:    10
Ideas:    4
Notes:    12
```

## 📚 Core Concepts

### 🏛️ Areas

**Reference hubs** — the single source of truth for a domain's systems, contacts, constraints, and current state. Think: *"Identity & Auth"*, *"API Management"*, *"DevOps & Release"*. Every project, idea, and note links to an area instead of duplicating context.

### 📁 Projects

Each project is a folder with a standard structure:

| File | Purpose |
|---|---|
| `README.md` | 🎯 Current state, goals, scope — the source of truth |
| `decisions.md` | ⚖️ Append-only log with status tracking |
| `todo.md` | ✅ What's next |
| `log.md` | 📓 What happened — session summaries, newest first |

### 💡 Ideas

Lightweight captures: `raw` → `exploring` → `ready` → project (or `parked` / `rejected`). Low friction to capture, structured enough to act on.

### 📝 Notes

Session findings organized by category: tool gotchas, workflow patterns, architecture decisions, research, troubleshooting.

### 🔍 Insights

Draft observations about how you work with AI. Created automatically at session end, but **never applied as rules** until you explicitly promote them. You stay in control.

## 🔧 CLAUDE.md: Framework + Your Rules

The generated `CLAUDE.md` cleanly separates what Consigliere manages from what you customize:

```
<!-- cg:section:start=X -->     ← 🔒 Framework sections (updated by Consigliere)
<!-- user:section:start=X -->   ← ✏️ Your sections (never touched)
```

## 🏗️ Building from Source

```bash
git clone https://github.com/mnemcik/consigliere.git
cd consigliere
make build    # → ./cg binary
make test     # 🧪 Run tests
make lint     # 🔍 Run linters
make check    # ✅ Everything
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development details.

## 📊 Status

**Beta — v1.0.0 shipped, small user base.** The CLI surface (`init`, `match`, `status`, `version`) is stable and covered by tests. Framework conventions (sentinel-delimited sections, `.cg.json`, template set) are in active evolution and may change between minor releases; migrations will be documented in `CHANGELOG.md`.

## 📖 Project docs

- [Contributing](CONTRIBUTING.md) — setup, common tasks, release process, commit conventions
- [Security policy](SECURITY.md) — how to report vulnerabilities privately
- [Code of conduct](CODE_OF_CONDUCT.md) — Contributor Covenant 2.1
- [Changelog](CHANGELOG.md) — Keep a Changelog format, semver

## 📄 License

MIT
