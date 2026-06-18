#!/usr/bin/env bash
# Render all cct demo GIFs with VHS inside WSL.
#
# This uses ONLY fake demo homes (under ~/cct-rec and ~/projects) — it never
# touches a real ~/.codex or ~/.claude. Run from anywhere; paths are absolute.
#
#   wsl -d Ubuntu -- bash -lc 'bash /mnt/c/Users/faruk/Documents/Codex_sync/demo/record.sh'
#
# Outputs ~/cct-rec/*.gif (also copied into the repo's demo/ folder).
set -euo pipefail

export PATH="$HOME/.local/bin:$HOME/.local/opt/go/bin:$HOME/go/bin:$PATH"

REPO="/mnt/c/Users/faruk/Documents/Codex_sync"
REC="$HOME/cct-rec"
mkdir -p "$REC"

# --- 1. Make VHS's headless Chromium find its NSS libs (no sudo) -------------
LIBDIR="$HOME/.local/chrome-libs/usr/lib/x86_64-linux-gnu"
if [ ! -f "$LIBDIR/libnss3.so" ]; then
  echo "Fetching chrome NSS libs locally (no sudo)…"
  tmp="$(mktemp -d)"
  ( cd "$tmp" && apt-get download libnss3 libnspr4 >/dev/null 2>&1 \
      && mkdir -p "$HOME/.local/chrome-libs" \
      && for d in *.deb; do dpkg-deb -x "$d" "$HOME/.local/chrome-libs"; done )
  rm -rf "$tmp"
fi
export LD_LIBRARY_PATH="$LIBDIR:${LD_LIBRARY_PATH:-}"

# --- 2. Ensure age is available (encryption tape), no sudo ------------------
if ! command -v age >/dev/null; then
  echo "Installing age…"
  go install filippo.io/age/cmd/age@latest
  go install filippo.io/age/cmd/age-keygen@latest
fi

# --- 3. Build a fresh linux cct from the current source ---------------------
echo "Building cct…"
( cd "$REPO" && go build -o "$HOME/.local/bin/cct" ./cmd/cct )

# --- 4. Copy tapes + prep in with unix line endings ------------------------
for f in prep.sh \
         01-overview.tape 02-sync.tape 03-claude-handoff.tape 04-encryption.tape \
         05-conflicts-remap.tape 06-export-filters.tape 07-git-handoff.tape 08-cli-ui.tape; do
  tr -d '\r' < "$REPO/demo/$f" > "$REC/$f"
done
tr -d '\r' < "$REPO/demo/09-compressed.tape" > "$REC/09-compressed.tape"

cd "$REC"

# --- 5. Render each tape after preparing its scenario ----------------------
# tape : scenario
render() {
  local tape="$1" scenario="$2"
  echo "Recording $tape (scenario: $scenario)…"
  bash "$REC/prep.sh" "$scenario"
  vhs "$REC/$tape"
}

render 01-overview.tape        base
render 02-sync.tape            base
render 03-claude-handoff.tape  claude
render 04-encryption.tape      enc
render 05-conflicts-remap.tape conflict
render 06-export-filters.tape  base
render 07-git-handoff.tape     git
render 08-cli-ui.tape          base
render 09-compressed.tape      zstd

# --- 6. Copy the GIFs back into the repo -----------------------------------
cp "$REC"/*.gif "$REPO/demo/"
echo
echo "Done:"
ls -la "$REC"/*.gif
