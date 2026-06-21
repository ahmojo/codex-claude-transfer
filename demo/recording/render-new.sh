#!/usr/bin/env bash
# Render only the v0.8.0-era tapes (search, secrets, markdown, repair-times, sync).
# Uses fake demo homes only; never touches a real ~/.codex or ~/.claude.
set -euo pipefail
export PATH="$HOME/.local/bin:$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"

REPO="/mnt/c/Users/faruk/Documents/Codex_sync"
REC="$HOME/cct-rec"
mkdir -p "$REC"

LIBDIR="$HOME/.local/chrome-libs/usr/lib/x86_64-linux-gnu"
export LD_LIBRARY_PATH="$LIBDIR:${LD_LIBRARY_PATH:-}"

echo "Building cct…"
( cd "$REPO" && go build -o "$HOME/.local/bin/cct" ./cmd/cct )

for f in prep.sh 11-search.tape 12-secrets.tape 13-markdown.tape 14-repair-times.tape 15-sync.tape; do
  tr -d '\r' < "$REPO/demo/recording/$f" > "$REC/$f"
done
cd "$REC"

render() { echo "== $1 ($2) =="; bash "$REC/prep.sh" "$2"; vhs "$REC/$1"; }
render 11-search.tape       base
render 12-secrets.tape      secrets
render 13-markdown.tape     base
render 14-repair-times.tape stale
render 15-sync.tape         base

cp "$REC"/11-search.gif "$REC"/12-secrets.gif "$REC"/13-markdown.gif \
   "$REC"/14-repair-times.gif "$REC"/15-sync.gif "$REPO/demo/clips/"
echo "Done:"; ls -la "$REC"/1[1-5]-*.gif
