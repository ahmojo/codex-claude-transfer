#!/usr/bin/env bash
# Exercise a *packaged* cct binary end-to-end against throwaway fake homes, so a
# broken artifact blocks the release. The release workflow runs it on every
# platform it can execute natively (Linux, macOS, Windows).
#
# Sections:
#   1. portable core   — version, doctor, export, diff, import, undo, skill
#   2. cwd round trip  — import --map-cwd-here, then find the session again with
#                        --project . This only passes when the path cct recorded
#                        is the path it resolves "." to, which is where macOS's
#                        /var -> /private/var symlink and Windows drive letters
#                        bite.
#   3. POSIX extras    — unicode project paths, a project under $TMPDIR, file
#                        permissions, relocate + undo.
#
# The portable parts use only relative paths (never a leading-slash path) so the
# Windows binary under Git Bash gets the same, un-mangled arguments as on Linux.
# Sections that need a real absolute path run on POSIX only.
#
# One rule this script learned the hard way: never write `cct ... | grep -q`.
# `grep -q` exits at the first match, so the producer can die of SIGPIPE, and
# with `pipefail` that turns a successful match into a failed pipeline —
# intermittently, depending on how much output cct had already written. Every
# check below writes to a file first and greps the file.
set -euo pipefail

BIN="${1:-}"
if [ -z "$BIN" ] || [ ! -e "$BIN" ]; then
  echo "usage: smoke-artifact.sh <path-to-cct-binary>" >&2
  exit 2
fi
chmod +x "$BIN" 2>/dev/null || true
# A bare filename must be run as ./name; a path with a slash runs as-is.
case "$BIN" in */*) ;; *) BIN="./$BIN" ;; esac
# Absolute form, for the sections that change directory. Used on POSIX only,
# where an absolute path is not rewritten on its way into the binary.
BINABS="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
ROOT="$PWD"

posix=1
case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) posix=0 ;; esac

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
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# write_claude_transcript <home> <cwd> <id> — a minimal Claude Code transcript in
# the folder Claude derives from the cwd (every character outside [A-Za-z0-9-]
# becomes a dash).
write_claude_transcript() {
  local home="$1" cwd="$2" id="$3" encoded escaped
  encoded="$(encode_cwd "$cwd")"
  mkdir -p "$home/projects/$encoded"
  # The cwd is embedded in JSON, so a backslash has to be escaped.
  escaped="$(printf '%s' "$cwd" | sed 's/\\/\\\\/g')"
  cat > "$home/projects/$encoded/$id.jsonl" <<EOF
{"type":"user","uuid":"$id","sessionId":"$id","cwd":"$escaped","timestamp":"2026-06-13T10:00:00Z","message":{"role":"user","content":"artifact smoke test"}}
EOF
}

encode_cwd() { printf '%s' "$1" | LC_ALL=C sed 's/[^A-Za-z0-9-]/-/g'; }

echo "== version =="
run "$BIN" version

echo "== doctor --json =="
run "$BIN" doctor --json --codex-home "$src" > /dev/null

echo "== export =="
run "$BIN" export --all --codex-home "$src" -o "$bundle" > /dev/null
test -f "$bundle" || fail "no bundle written"

echo "== diff (read-only) =="
run "$BIN" diff "$bundle" --codex-home "$dst" > /dev/null
test ! -e "$sess" || fail "diff wrote a file (must be read-only)"

echo "== import =="
run "$BIN" import "$bundle" --codex-home "$dst" > /dev/null
test -f "$sess" || fail "import did not create the session"

echo "== undo =="
run "$BIN" undo --codex-home "$dst" > /dev/null
test ! -e "$sess" || fail "undo did not remove the imported session"

echo "== skill install / init / show =="
claude="$work/claude"
run "$BIN" skill install --claude-home "$claude" > /dev/null
skillfile="$claude/skills/cct-session-sync/SKILL.md"
test -f "$skillfile" || fail "skill install did not write $skillfile"
grep -q "cct-session-sync" "$skillfile" || fail "the installed skill has no frontmatter name"
# A second install must be a no-op, not an error.
run "$BIN" skill install --claude-home "$claude" > /dev/null

proj="$work/proj"
mkdir -p "$proj"
run "$BIN" skill init --project "$proj" --repo "https://example.invalid/sessions.git" > /dev/null
test -f "$proj/.cct/sessions.json" || fail "skill init wrote no reference"
test -f "$proj/.cct/README.md" || fail "skill init wrote no README"
run "$BIN" skill show --project "$proj" --json > "$work/show.json"
grep -q "example.invalid" "$work/show.json" || fail "skill show did not report the store repo"
# A reference naming a command-executing transport must stop the command.
sed 's#https://example.invalid/sessions.git#ext::sh -c evil#' "$proj/.cct/sessions.json" > "$work/ref.json"
mv "$work/ref.json" "$proj/.cct/sessions.json"
if "$BIN" skill show --project "$proj" > /dev/null 2>&1; then
  fail "skill show accepted a remote-helper transport"
fi

echo "== cwd round trip (import --map-cwd-here, then find it again) =="
rtproj="$work/rt-proj"
mkdir -p "$rtproj"
write_claude_transcript "$work/rt-src" "/original/project" "bbbb2222-3333-4444-5555-666677778888"
run "$BIN" export --all --tool claude --claude-home "$work/rt-src" -o "$work/rt.codexbundle" > /dev/null
(
  cd "$rtproj"
  ../../"$BIN" import ../rt.codexbundle --claude-home ../rt-claude --merge --map-cwd-here > /dev/null
  ../../"$BIN" export --tool claude --claude-home ../rt-claude --project . -o ../rt-back.codexbundle > /dev/null
)
run "$BIN" inspect "$work/rt-back.codexbundle" > "$work/rt-back.txt"
grep -q "bbbb2222" "$work/rt-back.txt" \
  || fail "a session imported with --map-cwd-here is not found again by --project ."

if [ "$posix" -eq 0 ]; then
  rm -rf "$work"
  echo "ARTIFACT SMOKE OK (portable sections; POSIX extras skipped on Windows)"
  exit 0
fi

# round_trip <label> <project dir> <claude home> <out bundle>
# Import the reference bundle while standing in the project directory, then ask
# for that project by name. Both halves resolve the current directory
# independently, so this fails whenever the path cct records is not the path it
# resolves "." to. On failure it prints what each layer thought the path was —
# the shell's, the kernel's, and the folder name cct derived — because that is
# the only way to tell a normalization problem from a symlink problem.
round_trip() {
  local label="$1" projdir="$2" home="$3" out="$4" log
  log="$ROOT/$work/roundtrip.log"
  mkdir -p "$projdir"
  (
    cd "$projdir"
    "$BINABS" import "$ROOT/$work/rt.codexbundle" --claude-home "$home" \
      --merge --map-cwd-here
    "$BINABS" export --tool claude --claude-home "$home" --project . -o "$out"
  ) > "$log" 2>&1
  "$BIN" inspect "$out" > "$ROOT/$work/inspect.txt"
  if grep -q "bbbb2222" "$ROOT/$work/inspect.txt"; then
    return 0
  fi
  {
    echo "--- $label diagnostics ---"
    echo "shell path:"
    printf '%s' "$projdir" | od -c | head -4
    echo "kernel path:"
    ( cd "$projdir" && pwd -P | od -c | head -4 )
    echo "project folders cct wrote:"
    ls "$home/projects" || true
    echo "recorded cwd:"
    grep -o '"cwd":"[^"]*"' "$home"/projects/*/*.jsonl | head -4 || true
    echo "import/export output:"
    cat "$log" || true
  } >&2
  fail "$label did not round trip"
}

echo "== unicode project path =="
round_trip "a unicode project path" "$ROOT/$work/pröjekt-日本語" \
  "$ROOT/$work/uni-claude" "$ROOT/$work/uni-back.codexbundle"

echo "== project under \$TMPDIR (a symlinked path on macOS) =="
tmpbase="$(mktemp -d)"
trap 'rm -rf "$tmpbase"' EXIT
round_trip "a project under \$TMPDIR" "$tmpbase/proj" \
  "$tmpbase/claude" "$tmpbase/back.codexbundle"

echo "== file permissions =="
test -x "$BIN" || fail "the packaged binary is not executable"
mode() { ls -l "$1" | cut -c1-10; }
case "$(mode "$bundle")" in
  ?????w* | ????????w*) fail "the bundle is group- or world-writable: $(mode "$bundle")" ;;
esac
run "$BIN" import "$bundle" --codex-home "$dst" > /dev/null
test -f "$sess" || fail "re-import did not create the session"
case "$(mode "$sess")" in
  ?????w* | ????????w*) fail "an imported session is group- or world-writable: $(mode "$sess")" ;;
esac
test -r "$sess" || fail "an imported session is not readable"
test -f "$skillfile" && test ! -x "$skillfile" || fail "the installed skill should not be executable"

echo "== relocate + undo =="
relid="cccc3333-4444-5555-6666-777788889999"
oldproj="$ROOT/$work/old-project"
newproj="$ROOT/$work/new-project"
mkdir -p "$oldproj" "$newproj"
relhome="$work/rel-claude"
write_claude_transcript "$relhome" "$oldproj" "$relid"
oldfile="$relhome/projects/$(encode_cwd "$oldproj")/$relid.jsonl"
newfile="$relhome/projects/$(encode_cwd "$newproj")/$relid.jsonl"
run "$BIN" relocate "$oldproj" "$newproj" --tool claude --claude-home "$relhome" --dry-run > /dev/null
test -f "$oldfile" || fail "--dry-run moved the transcript"
run "$BIN" relocate "$oldproj" "$newproj" --tool claude --claude-home "$relhome" > /dev/null
test -f "$newfile" || fail "relocate did not write the transcript under the new project"
test ! -f "$oldfile" || fail "relocate left the original transcript in place"
grep -q "$newproj" "$newfile" || fail "relocate did not rewrite the recorded cwd"
run "$BIN" undo > /dev/null
test -f "$oldfile" || fail "undo did not restore the original transcript"
test ! -f "$newfile" || fail "undo did not remove the relocated copy"

rm -rf "$work" "$tmpbase"
trap - EXIT
echo "ARTIFACT SMOKE OK"
