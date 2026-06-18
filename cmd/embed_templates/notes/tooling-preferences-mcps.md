# Tooling Preferences — MCPs and Custom Code

## Meta

- **Category:** reference
- **Tags:** `mcp`, `tooling`, `integration`, `policy`
- **Framework note:** shipped by Consigliere and loaded on demand when a task needs external-system integration. Safe to edit for your workspace.

## When to load this note

Any session that needs external-system integration — Slack, Jira, Confluence, GitHub, Gmail, Google Drive/Calendar, Notion, Atlassian, or any other system reached via an MCP or a custom script. Load before proposing a new MCP server, writing a custom integration script, or deciding between existing tools.

## Preference order

1. **Official / first-party MCP tools already installed.** Vendor-managed integrations and any MCP servers already configured for the environment (`~/.claude.json`). Use these for everything they support.
2. **Custom code you fully control** — scripts committed to this workspace, under `projects/<slug>/tools/` or a shared location. Preferred over third-party MCPs for anything the official tools don't cover.
3. **Third-party MCP servers** — avoid unless no other option exists. When genuinely required, pin a specific version, audit the code, and narrow the exposed tool surface to only what is needed.

## Gap handling

When the official MCP is missing a capability, propose a custom script — not a third-party MCP server. Document the gap and the chosen approach in the project's decisions file (`projects/<slug>/decisions.md`) so the trade-off is traceable.

## Why this order

Official tools are maintained and trusted by the vendor; custom code you own is auditable and version-controlled in the workspace; a third-party MCP is opaque code running with your credentials and tool access, so it carries the most supply-chain risk and is the last resort. The same logic applies to extending via plugins/skills rather than standalone scripts — prefer the option whose code you can read and pin.

## Related

- `CLAUDE.md` → **Tooling Preferences — MCPs and Custom Code** (inline trigger + pointer).
- Any workspace `notes/` entries documenting known limitations of specific MCPs — those inform when a custom wrapper is warranted.
