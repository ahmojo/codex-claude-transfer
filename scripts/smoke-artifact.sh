#!/usr/bin/env bash
# Exercise a *packaged* cct binary end-to-end: version -> doctor -> export ->
# diff -> import -> undo, against throwaway fake homes. Run by the release
# workflow against each built artifact so a broken binary blocks the release.
#
# It uses only relative paths (never a leading-slash path) so the Windows binary
# under Git Bash gets the same, un-mangled arguments as on Linux, and it does not
# change directory so the caller-supplied (relative) binary path stays valid.
set -euo pipefail

BIN="${1:-}"
if [ -z "$BIN" ] || [ ! -e "$BIN" ]; then
  echo "usage: smoke-artifact.sh <path-to-cct-binary>" >&2
  exit 2
fi
chmod +x "$BIN" 2>/dev/null || true
# A bare filename must be run as ./name; a path with a slash runs as-is.
case "$BIN" in */*) ;; *) BIN="./$BIN" ;; esac

work="smoke-work"
rm -rf "$work"
export CCT_CONFIG_DIR="$work/cfg"
src="$work/src"
dst="$work/dst"
uuid="aaaa1111-2222-3333-4444-555566667777"
day="$src/sessions/2026/06/13"
mkdir -p "$day"
cat > "$day/rollout-2026-06-13T18-22-01-$uuid.jsonl" <<EOF
{"timestamp":"x","type":"session_meta","payload":{"id":"$uuid","cwd":"/proj/smoke","source":"cli"}}
{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"artifact smoke test"}}
EOF

sess="$dst/sessions/2026/06/13/rollout-2026-06-13T18-22-01-$uuid.jsonl"
bundle="$work/smoke.codexbundle"
run() { echo "+ $*"; "$@"; }

echo "== version =="
run "$BIN" version

echo "== doctor --json =="
run "$BIN" doctor --json --codex-home "$src" >/dev/null

echo "== export =="
run "$BIN" export --all --codex-home "$src" -o "$bundle" >/dev/null
test -f "$bundle" || { echo "FAIL: no bundle written" >&2; exit 1; }

echo "== diff (read-only) =="
run "$BIN" diff "$bundle" --codex-home "$dst" >/dev/null
test ! -e "$sess" || { echo "FAIL: diff wrote a file (must be read-only)" >&2; exit 1; }

echo "== import =="
run "$BIN" import "$bundle" --codex-home "$dst" >/dev/null
test -f "$sess" || { echo "FAIL: import did not create the session" >&2; exit 1; }

echo "== undo =="
run "$BIN" undo --codex-home "$dst" >/dev/null
test ! -e "$sess" || { echo "FAIL: undo did not remove the imported session" >&2; exit 1; }

rm -rf "$work"
echo "ARTIFACT SMOKE OK"
