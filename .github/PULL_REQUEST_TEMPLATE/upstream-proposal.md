## Summary

What this PR changes in the framework.

## Origin

- **Workspace:** which kind of workspace the change comes from ("a personal workspace"; no names)
- **Source:** the file or rule there (e.g. a drifted `notes/<name>.md`), and the incident or reason behind it
- **Why upstream:** why every workspace benefits, not only this one

## Impact on workspaces

- **On `cg sync`:** what changes for a workspace that never edited the affected files (updated automatically)
- **Drift:** which workspaces edited the same files and will see them as drifted, if known
- **Behaviour:** how Claude acts differently after the change, and in which situations

## Test plan

- [ ] `make check` passes locally (lint + test + fmt + tidy)
- [ ] Opened as a draft with the `upstream-proposal` label; the `impact-review` status is set by the maintainer only

The description becomes the squash commit message: keep it current after each push, and keep private details (people, companies, internal systems, incidents) out of it.

See [CONTRIBUTING.md](https://github.com/mnemcik/consigliere/blob/main/CONTRIBUTING.md#upstream-proposals) for the label, [How a PR gets to merge](https://github.com/mnemcik/consigliere/blob/main/CONTRIBUTING.md#how-a-pr-gets-to-merge) for what the impact review checks, and [Impact review](https://github.com/mnemcik/consigliere/blob/main/CONTRIBUTING.md#impact-review) for the required check.
