---
description: >-
  Bootstrap a new Consigliere workspace in the current directory. Creates the folder structure,
  templates, index files, .cg.json, CLAUDE.md, and PROFILE.md. Use when the user wants
  to set up a new personal workspace or knowledge base.
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

If `cg` is not found in PATH, try the workspace-local binary:

```bash
./.cg/bin/cg init $ARGUMENTS
```

If the binary is not found at all, inform the user they need to install Consigliere:
- Download from: https://github.com/mnemcik/consigliere/releases
- Or build from source: `go install github.com/mnemcik/consigliere@latest`

Report the output of the command to the user.
