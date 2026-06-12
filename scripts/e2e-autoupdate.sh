#!/usr/bin/env bash
#
# End-to-end test for cg's binary auto-update subsystem (internal/autoupdate).
#
# Drives the REAL code paths — discovery, download, SHA-256 verification,
# atomic self-replace, the detached background worker, and the major-version
# gate — without touching GitHub or cutting a release. It builds
# GoReleaser-shaped release archives for a couple of fake versions, serves them
# (plus a configurable /releases/latest) from a throwaway local HTTP server, and
# points a freshly-built `cg` at it via the CONSIGLIERE_* overrides that exist
# for exactly this purpose. Everything lives in a temp dir and is cleaned up.
#
# Requirements: go, python3, and sha256sum or shasum.
# Usage: scripts/e2e-autoupdate.sh   (run from anywhere in the repo)
# Exit code: number of failed assertions (0 = all passed).
set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="github.com/mnemcik/consigliere/cmd"
PORT="${CG_E2E_PORT:-8771}"
TEST_REPO="test/consigliere"
OS="$(go env GOOS)"; ARCH="$(go env GOARCH)"

WORK="$(mktemp -d)"
SRV="$WORK/releases"; INSTALL="$WORK/bin"; CFG="$WORK/config"
mkdir -p "$SRV" "$INSTALL" "$CFG/consigliere"

SRVPID=""
cleanup() {
  if [ -n "$SRVPID" ]; then
    kill "$SRVPID" 2>/dev/null
    wait "$SRVPID" 2>/dev/null # absorb the job-control "Terminated" notice
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

# SHA-256 tool (Linux: sha256sum, macOS: shasum -a 256).
if command -v sha256sum >/dev/null 2>&1; then SHA() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1;   then SHA() { shasum -a 256 "$@"; }
else echo "need sha256sum or shasum" >&2; exit 2; fi

pass=0; fail=0
ok()  { echo "  ✅ $1"; pass=$((pass+1)); }
bad() { echo "  ❌ $1"; fail=$((fail+1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1 — [$2]"; fi; }

build_release() { # $1 = version without leading v
  local ver="$1" stage; stage="$(mktemp -d)"
  ( cd "$REPO_ROOT" && go build -ldflags "-X $PKG.Version=$ver" -o "$stage/cg" . )
  ( cd "$stage" && tar -czf "$SRV/consigliere_${ver}_${OS}_${ARCH}.tar.gz" cg )
  rm -rf "$stage"
}
regen_checksums() { ( cd "$SRV" && SHA *.tar.gz | sed 's| \*| |' > checksums.txt ); }
set_latest() { echo "$1" > "$SRV/LATEST"; }

mark_installed_sh() {
  cat > "$CFG/consigliere/installed.json" <<JSON
{"version":"1.0.0","tag":"v1.0.0","method":"install.sh","os":"$OS","arch":"$ARCH","path":"$INSTALL/cg","installedAt":"2026-01-01T00:00:00Z"}
JSON
}
reset_old() { # rebuild a v1.0.0 install + clean state; $1=selfmanaged|plain
  ( cd "$REPO_ROOT" && go build -ldflags "-X $PKG.Version=1.0.0" -o "$INSTALL/cg" . )
  rm -f "$CFG/consigliere/"{last-update-check,autoupdate.lock,updated.json,major-available.json,major-ignored.json,autoupdate.log}
  if [ "${1:-}" = "selfmanaged" ]; then mark_installed_sh; else rm -f "$CFG/consigliere/installed.json"; fi
}

# Run cg with the harness env (local server stands in for GitHub).
cg() {
  XDG_CONFIG_HOME="$CFG" \
  CONSIGLIERE_AUTO_UPDATE_REPO="$TEST_REPO" \
  CONSIGLIERE_GITHUB_API_BASE="http://127.0.0.1:$PORT" \
  CONSIGLIERE_GITHUB_DOWNLOAD_BASE="http://127.0.0.1:$PORT" \
  "$@"
}
ver_of() { "$INSTALL/cg" version 2>/dev/null | sed -E 's/^cg version //'; }

# --- the local GitHub stand-in --------------------------------------------
cat > "$WORK/server.py" <<'PY'
import http.server, json, os, sys
ROOT = sys.argv[1]; PORT = int(sys.argv[2])
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.endswith("/releases/latest"):
            with open(os.path.join(ROOT, "LATEST")) as f: tag = f.read().strip()
            b = json.dumps({"tag_name": tag}).encode()
            self.send_response(200); self.send_header("Content-Type","application/json")
            self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b); return
        fp = os.path.join(ROOT, os.path.basename(self.path))
        if os.path.isfile(fp):
            with open(fp,"rb") as f: d = f.read()
            self.send_response(200); self.send_header("Content-Length", str(len(d))); self.end_headers(); self.wfile.write(d); return
        self.send_response(404); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", PORT), H).serve_forever()
PY

echo "Building release artifacts ($OS/$ARCH)…"
build_release 1.1.0
build_release 2.0.0
regen_checksums
python3 "$WORK/server.py" "$SRV" "$PORT" >"$WORK/server.log" 2>&1 &
SRVPID=$!
sleep 1
echo

echo "1) cg update check — newer release available"
reset_old selfmanaged; set_latest v2.0.0
out="$(cg "$INSTALL/cg" update check 2>&1)"
check "reports latest v2.0.0"        '[[ "$out" == *"v2.0.0"* ]]'
check "says an upgrade is available" '[[ "$out" == *"newer version is available"* ]]'

echo "2) cg update upgrade — self-replace (v1.0.0 → v2.0.0)"
out="$(cg "$INSTALL/cg" update upgrade 2>&1)"
check "upgrade reports success"   '[[ "$out" == *"Upgraded to v2.0.0"* ]]'
check "binary is now v2.0.0"      '[[ "$(ver_of)" == "2.0.0" ]]'
check "installed.json refreshed"  'grep -q "\"version\": \"2.0.0\"" "$CFG/consigliere/installed.json"'

echo "3) checksum mismatch — upgrade refuses, binary untouched"
reset_old selfmanaged; set_latest v2.0.0
sed -i.bak -E 's/^[0-9a-f]{64}( .*2\.0\.0.*)/0000000000000000000000000000000000000000000000000000000000000000\1/' "$SRV/checksums.txt"
out="$(cg "$INSTALL/cg" update upgrade 2>&1)"; rc=$?
check "upgrade exits non-zero"  '[[ $rc -ne 0 ]]'
check "error mentions checksum" '[[ "$out" == *"SHA-256"* || "$out" == *"checksum"* ]]'
check "binary still v1.0.0"     '[[ "$(ver_of)" == "1.0.0" ]]'
mv "$SRV/checksums.txt.bak" "$SRV/checksums.txt"

echo "4) background worker — minor bump auto-installs (v1.0.0 → v1.1.0)"
reset_old selfmanaged; set_latest v1.1.0
cg env CONSIGLIERE_AUTO_UPDATE_WORKER=1 "$INSTALL/cg" >/dev/null 2>&1
check "worker installed v1.1.0" '[[ "$(ver_of)" == "1.1.0" ]]'
check "updated.json written"    '[[ -f "$CFG/consigliere/updated.json" ]]'
note="$(XDG_CONFIG_HOME=$CFG CONSIGLIERE_AUTO_UPDATE=0 "$INSTALL/cg" status 2>&1)"
check "prints 'updated to v1.1.0'" '[[ "$note" == *"updated to v1.1.0"* ]]'
check "updated.json cleared"       '[[ ! -f "$CFG/consigliere/updated.json" ]]'

echo "5) background worker — MAJOR bump is gated (no auto-install)"
reset_old selfmanaged; set_latest v2.0.0
cg env CONSIGLIERE_AUTO_UPDATE_WORKER=1 "$INSTALL/cg" >/dev/null 2>&1
check "binary still v1.0.0"            '[[ "$(ver_of)" == "1.0.0" ]]'
check "major-available.json written"  '[[ -f "$CFG/consigliere/major-available.json" ]]'
check "no updated.json"                '[[ ! -f "$CFG/consigliere/updated.json" ]]'
maj="$(XDG_CONFIG_HOME=$CFG CONSIGLIERE_AUTO_UPDATE=0 "$INSTALL/cg" status 2>&1)"
check "major notice on normal cmd"     '[[ "$maj" == *"major release"* && "$maj" == *"v2.0.0"* ]]'

echo "6) snooze / ignore --major"
snz="$(XDG_CONFIG_HOME=$CFG "$INSTALL/cg" update snooze --major 2>&1)"
check "snooze reports 7 days"       '[[ "$snz" == *"7 days"* ]]'
silent="$(XDG_CONFIG_HOME=$CFG CONSIGLIERE_AUTO_UPDATE=0 "$INSTALL/cg" status 2>&1)"
check "notice silent while snoozed" '[[ "$silent" != *"major release"* ]]'
ign="$(XDG_CONFIG_HOME=$CFG "$INSTALL/cg" update ignore --major 2>&1)"
check "ignore confirms"             '[[ "$ign" == *"Ignored v2.0.0"* ]]'
check "marker cleared by ignore"    '[[ ! -f "$CFG/consigliere/major-available.json" ]]'
check "ignore list recorded"        'grep -q "v2.0.0" "$CFG/consigliere/major-ignored.json"'

echo "7) CONSIGLIERE_NO_UPDATE_NOTICE silences the major notice"
reset_old selfmanaged; set_latest v2.0.0
cg env CONSIGLIERE_AUTO_UPDATE_WORKER=1 "$INSTALL/cg" >/dev/null 2>&1
q="$(XDG_CONFIG_HOME=$CFG CONSIGLIERE_AUTO_UPDATE=0 CONSIGLIERE_NO_UPDATE_NOTICE=1 "$INSTALL/cg" status 2>&1)"
check "no notice when silenced"     '[[ "$q" != *"major release"* ]]'

echo "8) not install.sh-managed — upgrade refuses to self-replace"
reset_old plain
out="$(cg "$INSTALL/cg" update upgrade 2>&1)"
check "binary untouched"   '[[ "$(ver_of)" == "1.0.0" ]]'
check "points to installer" '[[ "$out" == *"install.sh"* ]]'

echo "9) dev build skips update entirely"
( cd "$REPO_ROOT" && go build -o "$INSTALL/cg" . )
out="$(cg "$INSTALL/cg" update check 2>&1)"
check "dev build skips check" '[[ "$out" == *"development build"* ]]'

echo
echo "================= RESULT: $pass passed, $fail failed ================="
exit "$fail"
