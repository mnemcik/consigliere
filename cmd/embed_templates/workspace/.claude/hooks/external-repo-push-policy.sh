#!/usr/bin/env bash
# Consigliere hook wrapper (PreToolUse, Bash) — delegates to the cg binary.
# Allows/denies `git push` by the target repo's declared push policy.
# `cg init --force` rewrites this file, so keep any custom logic minimal.
if ! command -v cg >/dev/null 2>&1; then
  exit 0 # cg not installed — don't gate the command
fi
exec cg push-policy gate
