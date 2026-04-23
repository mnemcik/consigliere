#!/usr/bin/env bash
#
# install.sh — install or upgrade `cg` (Consigliere) from GitHub Releases.
#
# Usage (one-liner):
#   curl -fsSL https://raw.githubusercontent.com/mnemcik/consigliere/main/install.sh | bash
#
# Or clone + run:
#   ./install.sh [--tag v1.2.3] [--force] [--dir <path>]
#
# Environment overrides:
#   CG_INSTALL_TAG   Pin a specific tag (same as --tag).
#   CG_INSTALL_DIR   Install directory (default: ~/.local/bin).
#
# The script:
#   1. Detects OS + arch and resolves the matching GoReleaser artefact.
#   2. Downloads the archive and `checksums.txt` from the release.
#   3. Verifies the SHA-256 checksum.
#   4. Extracts the `cg` binary into $CG_INSTALL_DIR (default ~/.local/bin).
#   5. Confirms before overwriting an existing install (skip with --force).
#   6. Records install state at ${XDG_CONFIG_HOME:-$HOME/.config}/consigliere/installed.json.
#   7. Warns if the install dir isn't on PATH.

set -euo pipefail

REPO="mnemcik/consigliere"
STATE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/consigliere"
STATE_FILE="$STATE_DIR/installed.json"
DEFAULT_INSTALL_DIR="$HOME/.local/bin"

# ----- logging helpers -----
if [[ -t 1 ]]; then
  C_BOLD=$'\033[1m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'; C_DIM=$'\033[2m'; C_OFF=$'\033[0m'
else
  C_BOLD=""; C_GREEN=""; C_YELLOW=""; C_RED=""; C_DIM=""; C_OFF=""
fi

log()  { printf '%s%s%s\n' "${C_BOLD}" "$*" "${C_OFF}"; }
info() { printf '  %s\n' "$*"; }
ok()   { printf '%s✓%s %s\n' "${C_GREEN}" "${C_OFF}" "$*"; }
warn() { printf '%s⚠%s %s\n' "${C_YELLOW}" "${C_OFF}" "$*" >&2; }
fail() { printf '%s✗%s %s\n' "${C_RED}" "${C_OFF}" "$*" >&2; exit 1; }

# Under `curl … | bash`, stdin is the script pipe, so [[ -t 0 ]] is false.
# /dev/tty is the controlling terminal — readable when run interactively,
# unavailable in headless CI, which is the behaviour we want.
is_tty() { [[ -t 1 ]] && [[ -r /dev/tty ]]; }

confirm() {
  local prompt="$1"
  if ! is_tty; then return 1; fi
  local reply
  read -r -p "$prompt [y/N] " reply </dev/tty
  [[ "${reply:-}" =~ ^[Yy]([Ee][Ss])?$ ]]
}

# ----- argument parsing -----
TAG="${CG_INSTALL_TAG:-}"
INSTALL_DIR="${CG_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
FORCE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ $# -ge 2 ]] || fail "--tag requires an argument"
      TAG="$2"
      shift 2
      ;;
    --dir)
      [[ $# -ge 2 ]] || fail "--dir requires an argument"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --force)
      FORCE=1
      shift
      ;;
    --help|-h)
      cat <<'HELP'
install.sh — install or upgrade `cg` (Consigliere) from GitHub Releases.

Usage (one-liner):
  curl -fsSL https://raw.githubusercontent.com/mnemcik/consigliere/main/install.sh | bash

Or clone + run:
  ./install.sh [--tag v1.2.3] [--force] [--dir <path>]

Flags:
  --tag <version>   Pin a specific release tag (default: latest).
  --dir <path>      Install directory (default: ~/.local/bin).
  --force           Overwrite an existing install without prompting.
  --help, -h        Show this help.

Environment overrides:
  CG_INSTALL_TAG    Pin a specific tag (same as --tag).
  CG_INSTALL_DIR    Install directory (same as --dir).
HELP
      exit 0
      ;;
    *)
      fail "Unknown argument: $1 (try --help)"
      ;;
  esac
done

# ----- 1. prerequisites -----
log "Checking prerequisites"

command -v curl >/dev/null 2>&1 || fail "\`curl\` not found. Install it first (e.g. \`brew install curl\` or your distro's package manager)."
command -v tar  >/dev/null 2>&1 || fail "\`tar\` not found. It should be present on every Unix-like system — check your \$PATH."

# Pick whichever SHA-256 tool is available; macOS ships `shasum`, Linux ships `sha256sum`.
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD=(shasum -a 256)
else
  fail "No SHA-256 tool found (\`sha256sum\` or \`shasum\`). Install coreutils."
fi
ok "curl, tar, and SHA-256 tools are available"

# ----- 2. detect OS + arch -----
uname_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)  echo "linux" ;;
    darwin) echo "darwin" ;;
    *)      fail "Unsupported OS: $os. This script supports Linux and macOS. For Windows, download the zip from the Releases page manually." ;;
  esac
}

uname_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) fail "Unsupported architecture: $arch. Consigliere ships amd64 and arm64 builds." ;;
  esac
}

OS="$(uname_os)"
ARCH="$(uname_arch)"
ok "Detected platform: ${OS}/${ARCH}"

# ----- 3. resolve tag -----
log "Resolving release"

if [[ -z "$TAG" ]]; then
  # Public repo — no auth needed. The `/releases/latest` endpoint returns the newest
  # non-prerelease; it 404s if the repo has no releases yet.
  TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1 || true)"
  [[ -n "$TAG" ]] || fail "Could not resolve the latest release from $REPO. Pin a specific tag with --tag."
fi
ok "Target tag: $TAG"

# Strip leading 'v' for the artefact filename (GoReleaser's default {{ .Version }} is the tag without 'v').
VERSION="${TAG#v}"
ARCHIVE="consigliere_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"

# ----- 4. download archive + checksums -----
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/cg-install.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

info "Downloading $ARCHIVE"
curl -fsSL -o "$TMP_DIR/$ARCHIVE"      "$BASE_URL/$ARCHIVE" \
  || fail "Failed to download $ARCHIVE from $BASE_URL. The release may not include this OS/arch combo."

info "Downloading checksums.txt"
curl -fsSL -o "$TMP_DIR/checksums.txt" "$BASE_URL/checksums.txt" \
  || fail "Failed to download checksums.txt from $BASE_URL."

ok "Downloaded archive and checksums"

# ----- 5. verify checksum -----
EXPECTED="$(grep " $ARCHIVE$" "$TMP_DIR/checksums.txt" | awk '{print $1}' || true)"
[[ -n "$EXPECTED" ]] || fail "checksums.txt does not list $ARCHIVE. Release may be malformed."

ACTUAL="$("${SHA_CMD[@]}" "$TMP_DIR/$ARCHIVE" | awk '{print $1}')"
if [[ "$EXPECTED" != "$ACTUAL" ]]; then
  fail "SHA-256 mismatch for $ARCHIVE.
    expected: $EXPECTED
    actual:   $ACTUAL"
fi
ok "SHA-256 verified"

# ----- 6. extract -----
tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR" cg || fail "Archive did not contain a \`cg\` binary at the top level."
[[ -x "$TMP_DIR/cg" ]] || chmod +x "$TMP_DIR/cg"

# ----- 7. install dir + confirm overwrite -----
TARGET="$INSTALL_DIR/cg"

mkdir -p "$INSTALL_DIR" || fail "Could not create $INSTALL_DIR. Pass --dir <path> to install somewhere else."

if [[ -e "$TARGET" && $FORCE -eq 0 ]]; then
  EXISTING_VERSION="$("$TARGET" version 2>/dev/null | head -n1 | sed -E 's/^cg version //' || echo "unknown")"
  warn "$TARGET already exists ($EXISTING_VERSION)."
  if ! confirm "Overwrite with $TAG?"; then
    fail "Aborted by user. Re-run with --force to skip this prompt."
  fi
fi

mv -f "$TMP_DIR/cg" "$TARGET"
chmod +x "$TARGET"
ok "Installed $TARGET"

# ----- 8. verify -----
# `cg version` prints `cg version X.Y.Z` (full line). Strip the prefix so
# $INSTALLED_VERSION holds just the version token, both for the log line
# below and for the state file's `version` field.
INSTALLED_VERSION="$("$TARGET" version 2>/dev/null | head -n1 | sed -E 's/^cg version //' || echo "$TAG")"
ok "cg $INSTALLED_VERSION is ready"

# ----- 9. state file -----
mkdir -p "$STATE_DIR"
INSTALLED_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
cat > "$STATE_FILE" <<JSON
{
  "version": "$INSTALLED_VERSION",
  "tag": "$TAG",
  "method": "install.sh",
  "os": "$OS",
  "arch": "$ARCH",
  "path": "$TARGET",
  "installedAt": "$INSTALLED_AT"
}
JSON
ok "Recorded install state at $STATE_FILE"

# ----- 10. PATH hygiene -----
case ":$PATH:" in
  *":$INSTALL_DIR:"*) :;;
  *) warn "\$PATH does not include $INSTALL_DIR — add it to your shell rc so \`cg\` is reachable:
    export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

log "Done"
cat <<EOF
${C_DIM}  Next steps:${C_OFF}
    cg --help
    cg init
EOF
