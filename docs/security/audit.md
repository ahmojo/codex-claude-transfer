# Security audit — cct (codex-claude-transfer)

> Scope: a code-level review of the current tree as of the v0.5.0 work, plus a
> design review of the proposed [LAN sync](../design/lan-sync.md) feature. This is
> a self-audit by the maintainer's AI assistant under direction — **not** a
> substitute for an independent professional review, which the network feature in
> particular still needs. Findings include `file:line` references and concrete
> remediations.

## Methodology & threat model

cct moves AI-coding-session files between machines. The data it handles (prompts,
code, terminal output, paths, occasionally secrets and images) is **sensitive**.
The realistic adversaries:

1. **A malicious `.codexbundle`** the user is tricked into importing (the primary
   untrusted input today — bundles are meant to be shared/moved).
2. **A malicious local web page** trying to reach the `cct app` server in the
   user's browser.
3. **A hostile process** on the same machine (largely out of scope — same-user
   processes are trusted by the OS model; noted where relevant).
4. **A network adversary** on the LAN (only relevant to the *proposed* sync
   feature; see Part G).

Two structural properties frame everything below:

- **Bundle checksums give integrity, not authenticity** (see SEC-8). A crafted
  bundle is a *valid* bundle. Safety therefore comes from the **constrained write
  path**, not from trusting bundle contents — and that path is the core of this
  audit.
- **cct shells out** to `git`, `age`, `zstd`, and a browser opener. Each
  subprocess boundary is an attack surface (Part B).

## Findings summary

> **Update — fixes landed.** SEC-1, SEC-2, SEC-3, and SEC-10 are fixed, and the
> git-side of SEC-4 is hardened, in the v0.5.0 work (see the CHANGELOG and the
> per-finding "Status" below). **A more detailed audit will come soon** — this pass
> covered the highest-value surfaces; a follow-up should add fuzzing of the bundle
> parser, an external review of the LAN-sync crypto, and the remaining Low/Info
> items.

| ID | Severity | Area | Status |
|----|----------|------|--------|
| SEC-1 | ~~High~~ **Low** ¹ | `git clone` of a bundle-controlled remote → arg injection + forced outbound fetch; RCE **only** with non-default git config | **Fixed** |
| SEC-2 | Medium | Unbounded `zstd` decompression → memory-exhaustion (decompression bomb) | **Fixed** |
| SEC-3 | Medium | Unbounded ZIP entry/metadata inflation on import & inspect → disk/IO/CPU/memory exhaustion | **Fixed** |
| SEC-4 | Low | Subprocess argument injection via leading-dash inputs (no `--` terminator) | Git fixed; age/zstd pending |
| SEC-5 | Low | External tools resolved via `PATH` (hijack) | Accepted / document |
| SEC-6 | Low | WebUI token carried in the URL query string | Mitigated; optional hardening |
| SEC-7 | Low | Import write follows pre-existing symlinks under the sessions dir | Open (low) |
| SEC-10 | Low | Malicious bundle metadata printed to the terminal without escaping control/ANSI/OSC sequences | **Fixed** |
| SEC-8 | Info | Checksums are integrity, not authenticity | By design; document |
| SEC-9 | Info | WebUI/CLI operate on arbitrary local paths at user privilege | By design |

¹ **Downgraded from High after empirical testing.** See the reconciliation note below.

## Reconciliation with an independent audit (Codex, commit `53342c5`)

A second, independent audit was run. It **corroborated** the import/path-safety
strength, the WebUI design, the integrity-≠-authenticity point (SEC-8), and the
symlink nuance (SEC-7). Differences, in the interest of an honest record:

- **SEC-1 was overstated and is corrected to Low.** I claimed `git clone` allows
  the `ext::` transport by default, making `import --clone` of a malicious bundle a
  default RCE. The independent audit tested it and found modern git blocks it; I
  reproduced this on the host: `git clone "ext::true"` →
  `fatal: transport 'ext' not allowed` (git 2.52). So **`ext::` RCE is not
  reachable with default git config.** What remains real and worth fixing: a forced
  outbound fetch to an attacker URL, leading-dash argument injection, and RCE *only*
  for users who set `protocol.ext.allow=always` or custom remote helpers. The
  remediation below is still warranted as defense-in-depth.
- **SEC-3 extends beyond session entries.** The independent audit noted that
  `internal/bundle/inspect.go:83` reads `manifest.json` / `checksums.json` with
  unbounded `io.ReadAll`, so a giant *deflated metadata file* can OOM `cct inspect`
  before any session entry is examined. Folded into SEC-3.
- **SEC-10 is new — I missed it.** Bundle metadata (preview, cwd, git remote,
  warnings) is printed to the terminal unescaped, allowing ANSI/OSC injection
  (screen spoofing, OSC 52 clipboard writes) during the exact `inspect`/dry-run
  *review* step the user relies on to judge a bundle. See below.
- **The LAN-sync design could not be reviewed independently** — `docs/design/lan-sync.md`
  is uncommitted, so it was absent at commit `53342c5`. To get a second opinion on
  that design, commit it (or hand the file over) and re-run.

Net: one of my findings was too severe (now fixed), and the independent pass added
one real finding (SEC-10) plus a useful extension (SEC-3/inspect). Both audits
agree on the priority order below.

---

## Part A — Import path & path traversal  ✅ strong

The most important attack surface, and the best-defended. A malicious bundle
cannot write outside the agent home or write non-session files:

- **Path canonicalization** (`internal/safety/paths.go:28`): `CleanRelPath`
  rejects absolute paths, backslashes, drive/volume (`:`), empty/`.`/`..`
  segments, and any non-canonical form. **Zip-slip is blocked here.**
- **Verify-before-write** (`internal/bundle/import.go:174`): `verifyBundle`
  validates every entry path *and* confirms each SHA-256 against `checksums.json`
  **before any file is written** (`import.go:655`). A corrupt/truncated bundle
  aborts with nothing written.
- **Allowlisted destinations** (`import.go:198`): only entries matching the
  rollout/transcript shapes (`sessionEntryRe`, `claudeEntryRe` in `paths.go:17`)
  are written; everything else is skipped with a warning. So even a *safe-but-
  unexpected* path (`foo/bar.txt`) is never written.
- **Defense-in-depth containment** (`safety/paths.go:66`): `DestPath` re-checks
  that the joined path stays within the home root.
- **No overwrites** (`import.go` plan loop): an existing-but-different file is a
  conflict and is skipped unless the user opts into `--replace-with-backup` /
  `--import-as-copy`; writes are atomic (`safety/atomic.go`).
- **Never touches the agent index** (no SQLite / `~/.claude.json` writes).

This layered design is correct. The remaining import-side risks are resource
limits, not traversal — see SEC-3.

---

## Part B — Subprocess execution (git / age / zstd / browser)

All subprocesses use `exec.Command(name, args...)` with an argv slice, so there is
**no shell and no shell-injection** — good. The issues are *argument* and
*transport* injection, plus `PATH` resolution.

### SEC-1 (Low — corrected from High) — `git clone` of an attacker-controlled remote

`internal/git/git.go:72` `Clone(remote, dir, commit)` runs
`git clone <remote> <dir>` then `git checkout <commit>`. The `remote` and
`commit` come from the **bundle manifest** (`manifest.Git`), which is fully
attacker-controlled in a malicious bundle, and reach git via
`internal/cli/commands.go:841` and the GUI at `internal/webui/handlers.go:490`.

I originally rated this High on the assumption that git's `ext::` transport
(`ext::sh -c '…'` → arbitrary command) runs by default for a user-invoked clone.
**That is wrong on modern git.** Tested on the host: `git clone "ext::true"` →
`fatal: transport 'ext' not allowed` (git 2.52). So this is **not** default RCE.

What remains, and is still worth fixing:
- **Forced outbound fetch.** A malicious bundle can make `--clone` connect to an
  attacker-chosen URL (SSRF-ish, info-leak of the fact you imported, etc.).
- **RCE for permissive configs.** Users with `protocol.ext.allow=always` or custom
  remote helpers *are* exposed to the `ext::` command-execution path.
- **Argument injection.** A `remote`/`commit` beginning with `-` is misparsed as a
  git flag (no `--` terminator, no validation).

It requires user opt-in (`--clone`) on an untrusted bundle, and becomes more
relevant under LAN sync (a peer would supply git metadata) — so the hardening
below is defense-in-depth, not an emergency.

**Remediation:**
- Set `GIT_PROTOCOL_FROM_USER=0` in the clone's environment (makes git apply the
  restricted protocol policy, blocking `ext`/`file`).
- Belt-and-suspenders: `git -c protocol.ext.allow=never -c protocol.file.allow=never clone …`.
- Allowlist the URL scheme (`https://`, `http://`, `ssh://`, `git://`,
  `user@host:path`); reject `ext::`, `fd::`, `file://`, and anything starting with `-`.
- Add a `--` terminator before positionals; validate `commit` as a hex SHA / safe
  ref (reject leading `-`).

### SEC-4 (Low) — missing `--` terminator elsewhere

`crypt/age.go:53,63` and `zstdcli/zstd.go:44` pass user/bundle-influenced paths and
age recipients as the last positionals without a `--` separator. A path or
recipient beginning with `-` could be read as a flag by `age`/`zstd`. Local-only
and low impact today, but cheap to harden: insert `--` before positional file
arguments, and reject recipient strings starting with `-`.

### SEC-5 (Low, accepted) — `PATH`-based resolution

`git`, `age`, `zstd`, and the browser openers (`webui/server.go:183`) are resolved
by name via `PATH`. A hostile entry earlier on `PATH` would be executed. This is
the standard trade-off for "reuse the system tool"; same-user `PATH` control is
already game-over generally. Recommend documenting it and, optionally, resolving
each tool to an absolute path once via `LookPath` and reusing it.

---

## Part C — Decompression & resource limits

### SEC-2 (Medium) — unbounded `zstd` decompression bomb

`internal/zstdcli/zstd.go:74` `Decompress` (and `runPipe` at `:87`) reads **all**
of `zstd -dc` stdout into memory with no cap. It is called on bundle-supplied
compressed bytes in:
- `bundle/mergesync.go:108-114` (merge compares decompressed contents),
- `bundle/import.go` `remapCompressed` (`--map-cwd` on `.jsonl.zst`),
- `bundle/export.go` `addStrippedSessionToZip` (the new `--strip-images`).

A ~KB `.jsonl.zst` can inflate to many GB, exhausting memory. (Note: the
*metadata* scan path `DecompressHead` at `:40` is correctly bounded by
`io.LimitReader` — only the full-decompress path is unbounded.)

**Remediation:** cap `runPipe`/`Decompress` output with an `io.LimitReader` at a
generous-but-finite ceiling (e.g. 256 MiB) and return an error when exceeded;
optionally cross-check against the manifest's `SizeBytes`.

### SEC-3 (Medium) — unbounded ZIP entry inflation on import

`verifyBundle` (`import.go:670`) hashes every entry and `copyEntry`
(`import.go:760`) streams it to disk, both without a size ceiling and without
enforcing the manifest's `SizeBytes`. A ZIP with a high-ratio deflate entry (a
"zip bomb") inflates to fill the disk / burn IO during import — a local DoS.
Hashing is constant-memory (streamed), so this is disk/time, not OOM.

**Remediation:** enforce a maximum per-entry uncompressed size (and/or a bundle
total), and verify the actual inflated size matches `manifest.Sessions[].SizeBytes`
before committing the write. Also bound the metadata reads in
`inspect.go:83` (manifest/checksums) with `io.LimitReader`.

---

## Part C2 — Terminal injection via untrusted metadata (SEC-10, Low)

*Added from the independent audit; I missed this.* The `inspect` / dry-run / import
output prints manifest-derived strings — session **preview**, **cwd**, **git
remote**, bundle **paths**, and **warnings** — directly to the terminal, with no
control-character/escape sanitization:

- `internal/cli/render.go:190` (preview / bundle path), `:198` (cwd), `:290`
  (warnings with entry names), `:300`–`:320` (git remote, incl. a suggested
  `git clone …` line);
- `internal/cli/ui.go:420`, `:543` (cwd / git remote in the interactive UI).

A malicious bundle can embed ANSI/OSC sequences in those fields — clear the
screen, spoof a fake "looks safe" summary, inject a clickable terminal hyperlink,
or write the clipboard via **OSC 52** on terminals that honor it. No code
execution by itself, but it subverts the **exact `inspect`/preview step the user
relies on to decide whether a bundle is safe** — which makes it more than cosmetic.

**Remediation:** route all untrusted bundle metadata through a `safeTerminal(s)`
helper that strips/escapes C0/C1 controls and ANSI/OSC sequences before printing.
For copy-paste commands, don't embed untrusted strings in shell-looking text;
print structured instructions or safely-quoted arguments. (The WebUI already
HTML-escapes these values — this gap is CLI/TUI-only.)

---

## Part D — Desktop WebUI (`cct app`)  ✅ solid

`internal/webui/server.go` is a careful design:

- **Loopback-only bind** to `127.0.0.1` (`:74`) — never a routable address.
- **Per-launch 192-bit token** (`:174` `randomToken`, 24 random bytes) required on
  every `/api` call, compared in constant time (`subtleCompare`, `:163`).
- **`Host`-header allowlist** on every route (`localHost`, `:153`) — mitigates
  **DNS-rebinding**.
- **CSRF is mitigated structurally:** the token is a *custom request header*
  (`X-Cct-Token`), which forces a CORS preflight that a cross-origin page cannot
  satisfy (no `Access-Control-Allow-Origin` is set), and the page can't read the
  token. A malicious local page therefore cannot drive the API.
- `ReadHeaderTimeout` set (`:95`) — basic slowloris guard.

Notes / minor:

- **SEC-6 (Low):** the token is delivered in the URL query string (`:81`), so it
  lands in browser history and could leak via a `Referer` header. The SPA is fully
  self-contained (embedded assets, no external requests), so referer-leak risk is
  low in practice. Optional hardening: on first load, set the token as a
  `SameSite=Strict`, `HttpOnly`-not-needed cookie and drop it from the URL; or at
  least document the "treat the URL as a secret" expectation.
- Consider adding `ReadTimeout`/`WriteTimeout`/`IdleTimeout` to the `http.Server`.
- **SEC-9 (Info, by design):** the API performs the same powerful filesystem
  operations as the CLI on arbitrary user-supplied paths (read any bundle, write a
  bundle anywhere, clone, decrypt). This is intended parity, gated by the token.
  It means token compromise == full user-privilege file access; keep the token
  secret and the bind loopback-only. This matters a lot more for LAN sync (Part G).

---

## Part E — Cryptography, secrets & integrity

- **age integration** (`crypt/age.go`) correctly delegates crypto to a vetted
  external tool rather than rolling its own. Passphrase mode connects the real TTY
  (`:97`); the passphrase is never placed on a command line or in env. Good.
- Decryption writes plaintext to a **temp file** (in the CLI/WebUI
  `resolveBundlePath`). Confirm it uses `os.MkdirTemp` (0700) and always cleans up
  — briefly-on-disk plaintext is an accepted, documented trade-off.
- **SEC-8 (Info) — integrity ≠ authenticity.** `checksums.json` lives *inside* the
  bundle; an attacker who crafts a bundle simply recomputes matching hashes. So
  "checksum verified" means "not corrupted in transit," **not** "from someone you
  trust." The only authenticity/confidentiality mechanism is opt-in age encryption
  (and even that is recipient encryption, not a signature). This is fine given the
  constrained write path — but it must be stated plainly, and it is the reason LAN
  sync's trust must come from the **paired channel**, never from the payload.

---

## Part F — Image stripping (`--strip-images`, new in this cycle)  ✅ low-risk

`internal/bundle/stripimages.go` parses each JSONL line with `encoding/json` into
`json.RawMessage` trees and rewrites only image payloads. Review notes:

- Pure stdlib parsing; rewrites only matched image strings/objects, preserving all
  other bytes. No traversal or write outside the bundle (it only shapes export
  content).
- Resource use is bounded by line size; the same SEC-2 cap should cover the
  decompress step it performs for `.jsonl.zst`.
- It is **lossy and not merge-friendly** (now flagged at runtime and in docs) —
  a correctness/UX caveat, not a security issue.

---

## Part G — LAN sync design review (the requested deep-dive)

The [proposal](../design/lan-sync.md) is the first feature that sends sessions off
the machine, so its security model is load-bearing. Assessment of the proposed
pieces:

**TLS with pairing-pinned fingerprints — sound, with caveats.** Self-signed certs
plus fingerprint pinning (rather than a CA) is the right shape for ad-hoc
device-to-device. The pin must be bound to the pairing exchange (below), or it is
just unauthenticated TLS.

**PAKE for the short code — correct primitive, do NOT hand-roll.** A password-
authenticated key exchange (e.g. SPAKE2/CPace) is exactly what makes a 6-digit
code safe against an eavesdropper and an active MITM. But implementing PAKE (or
any of the crypto) in-house is a footgun: use a vetted library, confined to the
sync/CLI layer (core stays stdlib-only). The pairing must bind the PAKE-derived
key to the TLS channel (channel binding / fingerprint confirmation) so a MITM
who relays TLS still fails the code check.

**Trust-on-first-use — first pairing is the vulnerable moment.** Mitigate by
displaying the code (or fingerprint words) on **both** screens and having the user
confirm they match, out-of-band — not entering a code shown on only one side.
Persist the peer's pin afterward (`~/.config/cct/peers.json`, never inside an
agent home).

**Anti-exfiltration private-IP guard — keep it, but decide the VPN policy.**
Refusing non-RFC1918/link-local peers is a strong, enforceable "local network
only." Open question: Tailscale/ZeroTier (CGNAT 100.64/10, fd7a::) look private and
*are* the user's network but blur "local" — default-deny with an explicit
`--allow-public`/`--allow-overlay` override is the safer call.

**Pre-existing findings become remotely triggerable — fix them first.** Under LAN
sync a *peer* supplies compressed session bytes and (potentially) git metadata, so:
- **SEC-2 (decompression bomb)** becomes a remote OOM DoS → the size caps are now
  **mandatory before** sync ships.
- **SEC-1 (`git clone` injection)** must never be reachable from sync: sync should
  **not** auto-clone; if code-fetch is offered, the SEC-1 remediation must already
  be in place and the action must stay an explicit, separate user opt-in.
- **SEC-3 (zip/entry size)** applies to streamed session transfers too — bound
  per-session and total transfer sizes.

**Additional sync-specific requirements:**
- A paired peer can presumably read *all* in-scope sessions — scope transfers
  (e.g. per-project) and make the peer's capabilities explicit; a compromised
  paired device means session disclosure.
- Reuse the existing import write path verbatim (Part A) so traversal/overwrite
  guarantees carry over — do **not** invent a second writer.
- Bind a listener only on the chosen LAN interface, on explicit `sync` invocation,
  with the same token/`Host` discipline as `cct app`; expect OS firewall prompts.
- Rate-limit/authenticate the discovery responder so mDNS can't be used to probe
  or amplify.

**Verdict:** the design is defensible and reuses the safe core well, but it is
correctly self-flagged as experimental. It must not ship without (a) the SEC-1/2/3
fixes, (b) a real external security review of the pairing/PAKE/TLS binding, and
(c) the "this sends your sessions over the network" labeling already called for in
the proposal.

---

## Part H — Supply chain

- The **core** packages are standard-library only; third-party deps
  (`charmbracelet/huh`, `mattn/go-isatty` and their transitive set) are confined to
  the CLI/TUI layer — a good blast-radius boundary. LAN sync's mDNS/PAKE deps must
  respect the same boundary.
- `go.sum` pins module hashes. Recommend enabling Dependabot/`govulncheck` in CI if
  not already, and pinning the GitHub Actions used in the release workflow by SHA.
- Release binaries are built `CGO_ENABLED=0 -trimpath`; consider checksums and
  (optionally) signing/SLSA provenance for releases so downloaders can verify.

---

## Prioritized recommendations

*(Reordered after reconciliation — SEC-1 dropped from the top once it was corrected
to Low.)*

1. **Fix SEC-2 and SEC-3** (decompression/entry/metadata size caps) — the only
   Medium-severity items, good hygiene now and **mandatory before** any network
   feature (a hostile peer would otherwise trigger them remotely).
2. **Fix SEC-10** (terminal escaping of untrusted metadata) — Low, but it
   undermines the inspect/preview review the whole safety story leans on; small
   patch (one `safeTerminal` helper).
3. **Harden SEC-1 and SEC-4** (git remote scheme allowlist + `GIT_PROTOCOL_FROM_USER=0`
   + `--` terminators + commit/recipient validation) — defense-in-depth; cheap.
4. Decide and document **SEC-5/6** (PATH resolution; URL-token expectation).
5. For **LAN sync**: do not implement until 1 and 3 are done and the pairing crypto
   has an independent review; keep it opt-in and clearly labeled. Commit the design
   doc so it can actually be reviewed (it was absent at the audited commit).

*Items 1–3 are concrete, isolated patches the maintainer can land independently of
the LAN-sync work.*
