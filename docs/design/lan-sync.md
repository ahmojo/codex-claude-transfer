# Design: LAN sync (`cct sync`)

> **Status: M1–M2 implemented (experimental), behind `--i-understand`.** The manual
> `serve`/`connect` flow, TLS + pairing-code authentication, the private-address
> guard, and bidirectional diff/preview/merge are built and tested
> (`internal/lansync`). **Deferred:** M3 mDNS discovery + remembered peers, and the
> M4 desktop Sync tab / suffix-only transfer. Because this is cct's first feature
> that sends data off-machine, it stays opt-in and clearly labelled.
>
> **A scoped security pass has been run** (pairing MAC, transport, anti-exfil,
> protocol DoS, and the bundle-apply boundary). No Critical/High issues; the
> medium/low findings are fixed: per-phase network deadlines + a `serve` accept
> loop that survives pre-auth DoS; resolve-once-then-dial-the-chosen-IP to close
> the DNS-rebinding window (plus a pre-TLS raw-peer recheck); the pairing code is
> entered at a prompt instead of `--code`; peer hostnames are C0/C1-sanitized; and
> the address guard now covers CGNAT/overlay ranges and IPv4-mapped IPv6. The
> bundle-apply path keeps all of `import`'s checksum/manifest/path-traversal/size
> guarantees. A broader external review is still welcome before the experimental
> label comes off.
>
> **Implementation choices that differ from the original proposal below:**
> - **No PAKE / no mDNS dependency.** Instead of SPAKE2, pairing uses a freshly
>   generated **high-entropy** code (~96 bits) plus an **HMAC confirmation bound to
>   both TLS certificate fingerprints** (channel binding). A LAN man-in-the-middle
>   sees different fingerprints on each leg and cannot forge the confirmation
>   without the code, and the code's entropy makes offline guessing infeasible — so
>   the security goal is met with **stdlib only and zero new dependencies**. (The
>   tradeoff vs. a true PAKE is a longer code; trust-on-first-use remembered peers,
>   which would let a short code be reused, is the deferred M3 work.)
> - **Transfer reuses the existing bundle path verbatim:** each side exports a
>   bundle of exactly the sessions the peer is missing and applies the received one
>   with `import --merge`, so every checksum/manifest/conflict/mtime guarantee is
>   inherited rather than reimplemented.

## The problem

Today the happy path is: `export` on machine A → physically move a `.codexbundle`
→ `import` on machine B, often with `--map-cwd` to fix paths. It works and it is
safe, but it is manual: you pick a project, choose an output path, copy a file,
type the path on the other side, and re-run the agent. For someone who switches
between a desktop and a laptop on the same Wi-Fi several times a day, that is a lot
of friction for "just bring my chats over."

**Goal:** on two machines on the same network, run one command (or click one
button) and have the new/grown sessions flow both ways — no file, no paths, no
USB stick — while keeping every safety property cct already guarantees.

## Hard constraints (these do not bend)

These are the project's non-negotiables; the design must preserve all of them:

1. **No third-party servers, no accounts, no cloud.** Sync is strictly
   **device-to-device** on the user's own network. No relay, no broker, no
   internet egress.
2. **Never write the agent's index/SQLite.** Apply sessions through the exact same
   JSONL import path that exists today; let the agent re-scan.
3. **No silent overwrites.** Reuse the existing conflict model: a session that
   diverged on both sides is reported, never clobbered. `--merge`'s append-only
   logic decides direction per session.
4. **Core stays stdlib-only.** Any new dependency (mDNS, etc.) lives in the
   CLI/sync layer, never in `internal/{bundle,sessions,safety,crypt,...}`.
5. **Honest labeling.** Because data now leaves the machine, every entry point must
   say so plainly, and it must be opt-in (a subcommand the user runs on purpose).

## Reframing the "nothing is uploaded" promise

Right now cct's headline is "no cloud, no server, nothing uploaded." LAN sync does
transmit session bytes over a wire, so the promise has to be restated precisely
rather than broken:

> cct sync talks **only to a device you paired on your own local network**,
> **directly** (peer-to-peer), with the transfer **encrypted and authenticated**.
> There is still no third-party server, no account, and no internet upload — and
> cct will **refuse to connect to a non-private (public) address**.

That last clause is a concrete, enforceable anti-exfiltration rule (see Security).

## User experience

Two commands, mirroring `serve`/`connect`, plus a zero-config front door.

```
# Zero-config: discover peers on the LAN, pick one, sync both ways.
cct sync

# Explicit roles (no discovery needed):
cct sync serve                 # this machine waits for a peer; prints a pairing code
cct sync connect <host>        # connect to a peer; complete pairing with the code

# Scoping (same filters as export):
cct sync --project .           # only this project's sessions
cct sync --tool claude         # Claude Code sessions instead of Codex
cct sync --pull-only           # receive only; don't send mine
```

Flow for `cct sync` (the intended default):

1. Both machines run `cct sync`. Each advertises itself on the LAN (mDNS).
2. Machine A shows a list of discovered peers ("MacBook-Air", "desktop"). User
   picks one.
3. **Pairing (first time only):** a 6-digit code is shown on B; A asks for it (or
   both show it and the user confirms it matches). This authenticates the peer and
   derives the channel key. The peer's identity is remembered for next time, so
   subsequent syncs skip the code (trust-on-first-use with an explicit first
   confirmation).
4. **Preview:** A and B exchange session manifests and show a dry-run summary —
   "12 to send, 3 to receive, 1 grew on both sides (conflict)" — before anything
   is written. This is the same preview discipline as `import`.
5. **Apply:** on confirm, sessions transfer and are written through the safe import
   path. The user is reminded to restart the agent.

A desktop **Sync tab** in `cct app` would wrap the same flow: a list of discovered
peers, a pairing-code field, a preview, and a Sync button.

## Architecture

Reuse as much of the existing core as possible; add a thin network layer.

```
                    cct sync (CLI/TUI/WebUI layer — may use deps)
                              │
        ┌─────────────────────┴─────────────────────┐
        │ discovery (mDNS)   pairing (code→key)      │
        │ transport (TLS)    sync protocol            │
        └─────────────────────┬─────────────────────┘
                              │ uses, unchanged:
   internal/sessions (scan)  internal/bundle (manifest, merge classify, safe write)
   internal/safety (atomic, path guards)   internal/codexhome/claudehome
```

### Discovery — mDNS / DNS-SD

Advertise a service like `_cct-sync._tcp` with the hostname and a public-key
fingerprint in the TXT record. A Go zeroconf library (e.g. `grandcat/zeroconf` or
`hashicorp/mdns`) handles browse/announce. This is the one meaningful new
dependency; it stays in the sync layer. Manual `connect <host:port>` works without
mDNS for networks where multicast is blocked.

### Pairing & transport security

This is the crux — sessions are sensitive and a LAN is only semi-trusted. A
detailed review of this design (and of the pre-existing findings that become
*remotely triggerable* once a peer supplies data) is in
[the security audit, Part G](../security/audit.md#part-g--lan-sync-design-review-the-requested-deep-dive).
In short: SEC-1/2/3 there must be fixed **before** this feature ships.

- **Channel:** TLS (stdlib `crypto/tls`) with self-signed certs generated per
  machine. Trust is **not** delegated to a CA; instead the pairing code pins the
  peer's cert fingerprint.
- **Pairing:** a short numeric code shown on one side. Use a PAKE (password-
  authenticated key exchange, e.g. SPAKE2) so a 6-digit code safely establishes a
  shared secret without being brute-forceable by an eavesdropper. The code both
  authenticates the peer and binds the TLS fingerprints, defeating
  man-in-the-middle on the LAN.
- **Known peers:** after first pairing, store the peer's fingerprint + a label in a
  config file (e.g. `~/.config/cct/peers.json` — **never** inside the agent home).
  Later syncs verify the pinned fingerprint and skip the code.
- **Anti-exfiltration guard:** refuse to `connect`/`serve` on anything that is not
  a private/link-local address (RFC1918 / fe80:: / etc.) unless the user passes an
  explicit `--allow-public` override. This makes "only your local network" an
  enforced rule, not just a promise. (Open question: how to treat VPN/Tailscale
  ranges — see below.)
- **Reuse from today:** the per-launch token + Host-header checks from `cct app`
  are the same defensive instincts; the sync server applies them too.

### Sync protocol (per session, append-only aware)

1. **Manifest exchange.** Each peer sends, for every in-scope session:
   `{threadID, relPath, sha256, sizeBytes, mtime, compressed}`. This is essentially
   the existing bundle manifest, streamed instead of zipped.
2. **Diff.** For each session, classify the relationship exactly like
   `classifyGrowth` does today:
   - present on one side only → transfer it;
   - one side is a byte-prefix of the other → the longer side is authoritative
     (append-only growth), transfer that direction;
   - byte-identical → skip;
   - diverged → **conflict**, reported, resolved with the same options as import
     (`--replace-with-backup` / `--import-as-copy` / skip).
3. **Transfer.** Pull the needed session bytes. v1: whole file (simplest, reuses
   the import write path verbatim). Later optimization: send only the appended
   **byte-suffix** for prefix-extends, since the files are append-only — much less
   data for long sessions.
4. **Apply.** Write through `internal/safety` atomic copy + the import conflict
   logic. Never touch SQLite. Compressed `.jsonl.zst` handled as in import.
5. **cwd remapping.** Same gotcha as today: if the project lives at a different
   path on each machine, sessions can land "hidden." Sync can carry the recorded
   cwds and offer the same interactive remap the `ui` import wizard already does.

Because steps 2–4 are literally the merge/import logic that already exists and is
tested, the new surface area is mostly discovery + pairing + a request/response
protocol over TLS.

## What this deliberately is NOT

- Not a background daemon. `cct sync` runs, does its thing, and exits. No
  always-on process, no auto-sync-on-change (that could come later, explicitly,
  but it is not the goal and not the default).
- Not internet sync. The public-address guard is intended to make remote use
  awkward-on-purpose; for "across the internet" the honest answer stays "export a
  bundle and move it yourself," optionally encrypted with age.
- Not a replacement for bundles. Files remain the durable, offline, auditable path.

## Phasing

- **M1 — manual transport (spike). ✅ DONE.** `cct sync serve` / `cct sync connect
  host:port`, TLS + pairing code, whole-file (bundle) transfer, reuse
  `import --merge` to apply. No discovery. Proves the transport + safety model.
- **M2 — bidirectional + preview. ✅ DONE.** Two-way diff, `--dry-run` summary,
  `--pull-only`/`--push-only`, conflicts reported (never overwritten). The
  private-address guard (`--allow-public` to override) ships here too.
- **M3 — mDNS discovery + remembered peers. ⏳ deferred.** The zero-config
  `cct sync` front door and `~/.config/cct/peers.json` (trust-on-first-use). This is
  the piece that would justify a shorter pairing code.
- **M4 — polish. ⏳ deferred.** Suffix-only transfer optimization; the desktop
  **Sync tab**; cwd-remap prompts.

Ship M1–M2 behind an **experimental** label (a visible "this sends sessions over
your network" banner; possibly gated behind `cct sync --i-understand` or an env
flag for the first releases).

## Open questions / risks

- **Security review is mandatory.** This is cct's first network feature and it
  moves sensitive data; the pairing/PAKE/TLS-pinning design needs a real audit
  before release.
- **VPN / overlay networks (Tailscale, ZeroTier).** Their addresses look private
  and are genuinely the user's own network, but blur "local." Decide whether to
  allow by default or require `--allow-public`.
- **Firewall prompts.** Binding a listener triggers OS firewall dialogs on
  Windows/macOS; the UX must explain why.
- **mDNS reliability.** Multicast is blocked on many corporate/guest networks;
  manual `connect` is the required fallback.
- **Trust-on-first-use.** First pairing is the vulnerable moment; showing and
  confirming the code on *both* screens is safer than entering it on one.
- **Dependency budget.** mDNS + (optionally) a PAKE library are new deps. Confirm
  they can be confined to the sync/CLI layer with the core staying stdlib-only.
- **Clock/mtime skew** between machines must not be load-bearing — rely on byte
  prefixes and hashes, not timestamps, for correctness (mtime is a hint only).
