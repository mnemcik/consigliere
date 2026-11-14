## Summary

What this PR changes in the framework.

## Origin

- **Workspace:** which workspace the change comes from
- **Source:** the file or rule there (e.g. a drifted `notes/<name>.md`), and the incident or reason behind it
- **Why upstream:** why every workspace benefits, not only this one

## Impact on workspaces

- **On `cg sync`:** what changes for a workspace that never edited the affected files (updated automatically)
- **Drift:** which workspaces edited the same files and will see them as drifted, if known
- **Behaviour:** how Claude acts differently after the change, and in which situations

## Test plan

- [ ] `make check` passes locally (lint + test + fmt + tidy)
- [ ] Opened as a draft with the `upstream-proposal` label; `impact-reviewed` is added by the maintainer only

See [CONTRIBUTING.md](../../CONTRIBUTING.md#upstream-proposals) for what the label and the gate do.
