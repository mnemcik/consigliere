## Summary

Brief description of what this PR does.

## Changes

- ...

## Impact on workspaces

- **On `cg sync`:** what changes for a workspace that never edited the affected files (updated automatically), or "none" for a binary-only change
- **Drift:** which edited files will show as drifted, if any
- **Behaviour:** how `cg` or Claude acts differently after the change, and in which situations

## Test plan

- [ ] `make check` passes locally (lint + test + fmt + tidy)
- [ ] Tested manually with `cg <command>`

The description becomes the squash commit message: keep it current after each push, and keep private details (people, companies, internal systems, incidents) out of it.

See [CONTRIBUTING.md](https://github.com/mnemcik/consigliere/blob/main/CONTRIBUTING.md) for development setup, PR title conventions, and [how a PR gets to merge](https://github.com/mnemcik/consigliere/blob/main/CONTRIBUTING.md#how-a-pr-gets-to-merge), including what the impact review checks.
