#!/usr/bin/env bash
# Consigliere hook wrapper (SessionStart) — delegates to the cg binary.
# `cg init --force` rewrites this file, so keep any custom logic minimal.
if ! command -v cg >/dev/null 2>&1; then
  exit 0 # cg not installed — skip the refresh
fi
exec cg session pull-latest
