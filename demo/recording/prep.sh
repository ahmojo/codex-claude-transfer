#!/usr/bin/env bash
# Set up a fake-home scenario for a demo tape. Never touches a real ~/.codex or
# ~/.claude — everything lives under ~/cct-rec and ~/projects (demo dirs).
#
#   bash prep.sh <scenario>
#
# Scenarios: base | claude | enc | conflict | mapcwd | git
set -euo pipefail
export PATH="$HOME/.local/bin:$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"

REPO="/mnt/c/Users/faruk/Documents/Codex_sync"
REC="$HOME/cct-rec"
mkdir -p "$REC"
cd "$REC"

# Demo project folders so imported sessions show as [ok], not [missing].
mkdir -p "$HOME/projects/todo-api" "$HOME/projects/weather-cli" \
         "$HOME/projects/recipe-site" "$HOME/projects/todo-api-here"

gen_base() {
  rm -rf "$REC/laptop" "$REC/pc"
  python3 "$REPO/demo/recording/gen_demo_home.py" "$REC" >/dev/null
  cct export --all --codex-home "$REC/laptop/codex-home" \
      -o "$REC/sessions.codexbundle" >/dev/null
}

scenario="${1:?usage: prep.sh <scenario>}"
case "$scenario" in
  base)
    gen_base
    ;;
  claude)
    gen_base
    rm -rf "$REC/claude-home"          # empty Claude home, handoff fills it
    ;;
  enc)
    gen_base
    rm -f "$REC/key.txt" "$REC/secret.codexbundle"*
    age-keygen -o "$REC/key.txt" 2>/dev/null
    ;;
  conflict)
    gen_base
    # Make pc identical to laptop, then change ONE existing line so it is a true
    # both-sides conflict (not a clean prefix that --merge would just append).
    rm -rf "$REC/pc"; cp -r "$REC/laptop" "$REC/pc"
    f=$(find "$REC/pc/codex-home/sessions" -name '*c3d4e5f6*.jsonl' | head -1)
    sed -i 's/into one middleware/into a shared validation module/' "$f"
    rm -f "$REC"/pc/codex-home/sessions/**/*.cct-bak* 2>/dev/null || true
    ;;
  mapcwd)
    gen_base
    rm -rf "$REC/pc2"; mkdir -p "$REC/pc2/codex-home"
    ;;
  zstd)
    gen_base
    # Compress the newest session to .jsonl.zst (as Codex itself does for older
    # rollouts). With zstd installed, cct still recovers its preview/metadata.
    f=$(find "$REC/laptop/codex-home/sessions" -name '*b8c9d0e1*.jsonl' | head -1)
    zstd -q "$f" -o "$f.zst" && rm "$f"
    ;;
  git)
    gen_base
    rm -rf "$REC/gitdemo" "$REC/gitsrc" "$REC/cloned"
    mkdir -p "$REC/gitdemo"
    (
      cd "$REC/gitdemo"
      git init -q --bare bare-remote
      git init -q work
      cd work
      git symbolic-ref HEAD refs/heads/main
      echo "# todo-api — a small REST service" > README.md
      git -c user.email=demo@example.com -c user.name=demo add -A
      git -c user.email=demo@example.com -c user.name=demo commit -qm "initial skeleton"
      git remote add origin "$REC/gitdemo/bare-remote"
      git push -q origin HEAD:main
    )
    mkdir -p "$REC/gitsrc/codex-home"
    python3 - "$REC/gitsrc/codex-home" "$REC/gitdemo/work" <<'PY'
import json, os, sys
home, cwd = sys.argv[1], sys.argv[2]
d = os.path.join(home, "sessions", "2026", "06", "16"); os.makedirs(d, exist_ok=True)
p = os.path.join(d, "rollout-2026-06-16T10-00-00-deadbeef.jsonl")
ts = "2026-06-16T10:00:00Z"
with open(p, "w") as fh:
    fh.write("\n".join([
        json.dumps({"timestamp": ts, "type": "session_meta", "payload": {
            "id": "deadbeef", "timestamp": ts, "cwd": cwd, "originator": "vscode",
            "source": "cli", "model_provider": "openai"}}),
        json.dumps({"timestamp": ts, "type": "event_msg", "payload": {
            "type": "user_message", "message": "Set up the project skeleton and push it."}}),
    ]) + "\n")
os.utime(p, (1718524800, 1718524800))
PY
    ;;
  secrets)
    gen_base
    # Inject a session that contains fake credentials so `cct scan` has something
    # to find. These are well-known EXAMPLE/dummy values, not real secrets.
    python3 - "$REC/laptop/codex-home" "$HOME/projects/todo-api" <<'PY'
import json, os, sys
home, cwd = sys.argv[1], sys.argv[2]
d = os.path.join(home, "sessions", "2026", "06", "16"); os.makedirs(d, exist_ok=True)
p = os.path.join(d, "rollout-2026-06-16T12-00-00-cafe1234.jsonl")
ts = "2026-06-16T12:00:00Z"
msg = ("My deploy script has AKIAIOSFODNN7EXAMPLE and "
       "ANTHROPIC_API_KEY=sk-ant-api03-EXAMPLEKEY1234567890abcd -- can you refactor it?")
with open(p, "w") as fh:
    fh.write("\n".join([
        json.dumps({"timestamp": ts, "type": "session_meta", "payload": {
            "id": "cafe1234", "timestamp": ts, "cwd": cwd, "source": "cli",
            "model_provider": "openai"}}),
        json.dumps({"timestamp": ts, "type": "event_msg", "payload": {
            "type": "user_message", "message": msg}}),
    ]) + "\n")
os.utime(p, (1718539200, 1718539200))
PY
    ;;
  stale)
    gen_base
    # Make one session look imported-with-the-wrong-mtime: its file modification
    # time runs days ahead of its newest content timestamp.
    f=$(find "$REC/laptop/codex-home/sessions" -name '*b2c3d4e5*.jsonl' | head -1)
    touch -d "2026-06-20T23:00:00" "$f"
    ;;
  *)
    echo "unknown scenario: $scenario" >&2; exit 2;;
esac
