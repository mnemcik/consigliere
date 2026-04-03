---
name: cg-init
description: >-
  Bootstrap a new Consigliere workspace in the current directory. Creates the folder structure,
  templates, index files, .cg.json, CLAUDE.md, and PROFILE.md. Use when the user wants
  to set up a new personal workspace or knowledge base, or says "init cg", "create workspace",
  "set up consigliere", or "init consigliere".
user-invocable: true
allowed-tools: Bash
argument-hint: "[--force]"
---

# /cg-init — Workspace Bootstrapper

This skill invokes the `cg` CLI tool to bootstrap a Consigliere workspace.

## Instructions

Run the following command:

```bash
cg init $ARGUMENTS
```

If `cg` is not found in PATH, try finding it in the plugin directory:

```bash
# Find the cg binary bundled with the plugin
find ~/.claude/plugins -name "cg" -type f 2>/dev/null | head -1
```

If the binary is not found at all, inform the user they need to install Consigliere:
- Download from: https://github.com/mnemcik/consigliere/releases
- Or build from source: `go build -o cg .` in the plugin directory

Report the output of the command to the user.
