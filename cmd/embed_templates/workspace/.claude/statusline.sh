#!/usr/bin/env bash
# Consigliere status-line wrapper — delegates to the cg binary.
# `cg init --force` rewrites this file, so keep any custom logic minimal.
if ! command -v cg >/dev/null 2>&1; then
  exit 0 # cg not installed — render nothing
fi
exec cg session statusline
