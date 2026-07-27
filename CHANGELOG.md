# Changelog

All notable changes to codex-claude-transfer are documented here.

## [1.6.0] - 2026-07-27

### Added
- **`cct relocate --tool claude` relocates a Claude Code project.** Claude records
  a project both in each transcript's `cwd` and in the `projects/<encoded-cwd>/`
  folder holding it, so relocation now moves the transcripts too: every remapped
  transcript is written under the folder encoding `NEW` first, and each original
  is backed up and removed only after all destination writes succeed — so a
  session id is never present twice. It supports the same session-only and
  `--move-project` modes as Codex, plus `--claude-home`. A destination that is
  already taken, a transcript that changed mid-relocation, an unsafe path overlap,
  or a cross-filesystem project move stops the command before anything is
  written; a failure afterward restores the removed originals, deletes the
  copies, and rolls the project rename back. `~/.claude.json` is never touched.
  Closes [#13](https://github.com/ahmojo/codex-claude-transfer/issues/13).
- **`cct undo` reverses a relocation's removals.** The undo journal (now version
  2; version 1 journals stay reversible) records transcripts a relocation
  removed, alongside the copies it created. Undo restores each original before
  deleting its copy, and if an original cannot be restored — missing or tampered
  backup, or something occupying its path again — the copy is deliberately kept,
  so a session is never left with no copy on disk.

## [1.5.0] - 2026-07-26

### Security / hardening
- **Reconcile fallback commands now validate untrusted values before rendering.**
  Bundle-controlled `session_meta.id` values must be canonical UUIDs before cct
  sends them to Codex or includes them in CLI/browser copy-paste guidance; the
  native reconcile entry point independently rejects the whole request if any
  ID is invalid. Fallback commands are also suppressed when `CODEX_HOME` cannot
  be represented byte-for-byte across supported shells (including paths with
  consecutive backslashes); restart guidance remains available.

### Added
- **`cct relocate` safely moves Codex sessions with their project.** The new
  command rewrites recorded project paths through cct's existing checked
  export/import path, supports dry-run previews and archived sessions, and can
  optionally rename the project directory with `--move-project`. It validates
  both the preview and real import result, keeps session backups and an undo
  journal, and rolls back the sessions and project move if relocation is
  incomplete. Claude Code relocation remains tracked separately in
  [#13](https://github.com/ahmojo/codex-claude-transfer/issues/13).
- **Opt-in `cct import --reconcile` for Codex discovery.** After a native Codex
  import changes rollout files, cct now capability-probes a short-lived Codex
  app-server, checks the state-backed thread list, asks Codex to `thread/read`
  missing exact IDs, and verifies the result. Protocol/startup/verification
  failures never invalidate the completed file import and print restart plus,
  when safely representable, exact `cct resume <thread-id> --run` fallbacks. cct
  still never writes Codex SQLite or `session_index.jsonl` directly. A synthetic
  live-import regression test
  captures the previously observed file-present/list-missing state and verifies
  native read-repair with Codex 0.144.6. The terminal wizard (`cct ui`) and
  browser app (`cct app`) expose the same opt-in flow; the browser reports
  verification details or safe fallbacks without turning a reconcile failure
  into an import failure.
- **GitHub releases now include a deterministic Go module-cache archive.** The
  release workflow packages the dependencies needed for Gentoo source builds,
  includes the archive in release checksums and provenance attestations, and
  uploads it alongside the prebuilt binaries.

## [1.3.1] - 2026-07-22

### Security / hardening
- **`cct undo` is now fail-closed against crashes and tampering.** A corrupt,
  incomplete, manipulated, or version-mismatched import journal always results in
  changing nothing: `undo` validates the newest journal (structure, version, and
  that every path stays inside the recorded agent home) before touching disk, and
  refuses rather than silently falling back to an older journal. Backups are now
  hashed at import time and verified before restore (a swapped or altered backup
  is refused), reversal never follows a dest or backup that was replaced by a
  symlink or directory, and the existing "only touch a file that still matches
  what the import wrote" guard is unchanged. Added an extensive test suite for
  these paths (corrupt/truncated/manipulated journals, moved/symlinked/dir-swapped
  files, tampered/missing backups, permission errors, and concurrent
  import/undo).
- **The undo journal lifecycle is documented** (location, `0600` file
  permissions, format version, retention, safe deletion, upgrade behavior, and
  the fact that merge/replace backups contain session content) in
  docs/internals.md, with a pointer from SECURITY.md.

### Added
- **The release workflow now smoke-tests the packaged artifacts.** A new gating
  job unpacks each built binary and runs `version → doctor → export → diff →
  import → undo` on Linux and Windows; a broken artifact blocks the release.
- **Compressed sessions skipped by full-text search are now visible.** `cct
  search`, `cct scan`, and `export --match` print how many `.jsonl.zst` sessions
  were skipped because their text was unavailable (and expose it in `--json`),
  instead of dropping them silently; `import --match`'s wording is aligned.

## [1.3.0] - 2026-07-22

### Added
- **`cct undo`** reverses the most recent import. Every `cct import` now records a
  small journal in cct's own config directory (never in an agent home) listing the
  files it created and the backups it made. `cct undo` deletes the created files
  and restores the backups — but only for files that still hash to exactly what
  the import wrote, so anything edited afterward is left untouched and reported as
  skipped. `--dry-run` previews the reversal and `--list` shows recent imports. A
  `--merge` update is reversible too: the pre-append original is backed up first.
- **Selective import.** `cct import` now honors `--project`, `--since`, and
  `--match` in addition to `--session`, so you can pull just a slice out of a large
  bundle (one project, only recent sessions, or only sessions matching a query).
  Filters combine with AND semantics and mirror the export-side filters.
- **`cct diff`** previews what importing a bundle would do — new, would-grow (with
  line counts), already-present, and conflicting sessions — completely read-only,
  using the same `--merge` analysis and the same selection/remap flags as import.
  Supports `--json`.

### Changed
- `import --project <path>` now **filters** the import to that project's sessions
  rather than only warning on a cwd mismatch. When importing everything is what you
  want, omit `--project`.

## [1.2.0] - 2026-07-18

### Added
- **Fuzz tests for the canonical merge comparator** (issue [#6] follow-up).
  Four Go fuzz targets hammer the serialization-tolerant comparison with deeply
  nested JSON, huge number literals, unusual unicode escapes, mixed CRLF/LF,
  corrupted trailing lines, very large single lines, and both plain and
  `.jsonl.zst` paths. The fuzzed invariant: the canonical comparison must never
  classify two semantically different values as equal, a fast-forward must
  preserve local bytes verbatim, and merging must be idempotent.
- **`SECURITY.md`**: supported versions, private vulnerability reporting (now
  enabled on the GitHub repo), response-time targets, and a hard rule to never
  attach real bundles or session files to reports.
- **Compatibility matrix in the README**: last-tested agent versions
  (Codex CLI 0.144.0, Claude Code 2.1.212), supported data, and known gaps per
  agent, including cross-agent handoff.
- **Release provenance.** Releases now ship `SHA256SUMS.txt`, a keyless
  Sigstore signature over it (`SHA256SUMS.txt.sigstore.json`), an SPDX SBOM,
  and GitHub artifact attestations for every archive. Verification steps are
  documented in docs/internals.md ("Verifying a release").

### Dependencies
- `github.com/mattn/go-isatty` 0.0.22 → 0.0.23 (Dependabot #8).
- Workflow actions bumped (supersedes Dependabot #1, #3, #4, #5, #7):
  `actions/checkout` v5 → v7, `actions/setup-go` v6 → v7,
  `actions/upload-artifact` v4 → v7, `actions/download-artifact` v4 → v8,
  `softprops/action-gh-release` v2 → v3.

## [1.1.1] - 2026-07-18

### Fixed
- **False merge conflicts after a cross-platform transfer** ([#6]). `import
  --merge` compared session records byte-for-byte, so the SAME records
  serialized with a different JSON key order (e.g. exported on macOS, imported
  on WSL/Linux) were misread as divergent — in the reported case 14 of 15
  sessions came back as conflicts even though 13 were identical and 1 had only
  grown. When the byte comparison says "diverged", merge now retries with a
  canonical comparison: each JSONL line is parsed and re-marshaled with sorted
  keys (normalizing key order, string escaping, and line endings), with number
  literals preserved exactly so values beyond float64 precision can never
  collapse into false equality. Canonically identical sessions report as
  already up to date; a canonically grown session fast-forwards by appending
  only the bundle's raw new lines — the local file's own serialization is
  preserved verbatim, and any genuine value difference remains a conflict.
  Works with `--map-cwd` (the mapping is applied before the comparison) and for
  compressed `.jsonl.zst` sessions.

[#6]: https://github.com/ahmojo/codex-claude-transfer/issues/6

## [1.1.0] - 2026-07-11

### Security / CI
- Linux CI now requires `age` and `age-keygen` and runs the recipients-file
  encryption round-trip as a mandatory test. Installation remains best-effort on
  macOS and Windows so platform package-manager availability does not mask the
  required Linux encryption gate.

## [1.0.0] - 2026-06-24

First stable release. No functional changes from 0.9.0 — this release marks an API
and bundle-format stability commitment.

### Changed
- **Adopt semantic versioning guarantees (see README → Stability & versioning).**
  The bundle format (`codex-sync-bundle-v1`) and the command-line interface are
  stable across the 1.x line: a bundle from any 1.x imports into any other 1.x, and
  existing commands/flags won't change incompatibly. `cct sync` remains explicitly
  experimental and is intentionally outside the stability guarantee until it graduates.
- Bumped `github.com/mattn/go-isatty` 0.0.20 → 0.0.22 (the sole third-party runtime
  dependency, used only by the interactive CLI for terminal detection).

### Notes
- The reusable `internal/` core remains standard-library-only.

## [0.9.0] - 2026-06-23

### Added
- **Ambient LAN sync.** `cct sync daemon` watches your sessions and automatically
  keeps already-remembered devices in step over the local network — no pairing code
  each time (it only ever talks to peers you previously paired with `--remember`).
  Peer **discovery** means `cct sync connect` with no address finds a serving/daemon
  peer on the LAN, and `sync serve` now advertises itself. Stdlib-only multicast
  beacon (no mDNS/DNS-SD dependency). `--interval`/`--once` tune the daemon. Each
  sync reuses the existing checksum-verified bundle + `import --merge` path, so every
  safety property is inherited. Still experimental and `--i-understand`-gated.
- **`cct resume [query]`** — find the best-matching session (by thread-id prefix or
  conversation text) and print the agent command that continues it; `--run` launches
  the agent on it directly.
- **`cct browse`** — an interactive session browser: search, pick one, then resume,
  export, or annotate it.
- **`cct stats`** — totals, busiest projects, and a recent-activity sparkline
  (`--json` supported).
- **`cct tag` / `cct name`** — annotate sessions with cct-only tags and friendly
  names. Stored in cct's own config dir (`meta.json`), never written into the agent's
  session files or index.
- **`cct config`** — save defaults (tool, codex/claude home, port) so you stop
  retyping flags; an explicit flag always wins.
- **`export --format html`** renders a session as a self-contained, escaped HTML
  document (alongside the existing `--format md`).
- **Pre-egress secret gate.** `export` and `sync` now refuse to write/send a bundle
  that contains a likely secret unless you pass `--redact` (mask them) or
  `--allow-secrets` (override). Turns the existing `scan` heuristics into an actual
  safety net at the moment data would leave the machine.
- **Desktop GUI parity.** `cct app` gained **Search**, **Stats**, and **Scan** views;
  Search results offer inline **resume**, **tag**, and **name**; the export form
  enforces the same secret gate (with a "replace secrets / export anyway" prompt).
- New core packages: `config`, `meta`, `stats`, `cctpaths`; `handoff.ToHTML`;
  `bundle.ScanBundleSecrets`; an append-only suffix-delta core (`lansync.PlanDelta`/
  `ApplyDelta`, SHA-verified) as the foundation for future wire-level optimization.

### Notes
- The reusable `internal/` core stays standard-library-only; third-party deps remain
  confined to the CLI/TUI layer.

## [0.8.0] - 2026-06-21

### Added
- **`cct search <query>` — full-text search across your sessions.** Searches the
  conversation text (not metadata) for a literal or `--regex` query, ranks by hit
  count, and shows a snippet per match, so you can find which session discussed
  something and then export it. Supports `--project`, `--since`, `--case-sensitive`,
  and `--json`, and is available in the `cct ui` menu.
- **`export --match <query>`** bundles only the sessions whose text matches —
  "export everything I discussed about X".
- **`export --format md`** renders the selected session(s) as readable Markdown
  (one file, or a directory for several) for reading or sharing — not re-importable.
- **`cct scan` + `export --redact` — secret awareness.** `cct scan` checks sessions
  for likely credentials (API keys, tokens, private keys; values masked) before you
  share or sync; `export --redact` replaces detected secrets with placeholders in
  the bundle (lossy, opt-in). Also in the `cct ui` menu.
- **`sync --remember` — trust-on-first-use peers.** After a one-time code pairing
  with `--remember` on both devices, each stores the other's pinned certificate
  fingerprint (in the OS config dir, never an agent home) and later syncs between
  trusted devices skip the code. A persistent per-device identity backs this.

## [0.7.1] - 2026-06-20

### Added
- **`cct sync` cwd remapping + missing-folder warning.** A synced session whose
  project lives at a different path on the receiving machine could land "hidden"
  (same gotcha as import). Sync now warns when that happens and accepts
  `--map-cwd-here` / `--map-cwd OLD=NEW` to place received sessions under the right
  local project.
- **`cct sync --json`** for scripting (peer, counts, remapped), matching the other
  commands.
- **`cct doctor` flags stale modification times.** It detects sessions imported by
  an older cct whose file mtime runs ahead of their content (the cause of the
  open-lag) and suggests `cct repair-times`.

### Changed
- **`repair-times` is much faster** on large/image-heavy sessions: it reads each
  file's tail first (where the newest timestamp is) instead of scanning the whole
  file.

### Security / CI
- Added a **fuzz harness for the bundle parser** (`FuzzImport`) — a malicious or
  corrupt bundle (now also a network-delivered input via `sync`) must be rejected
  with an error, never a panic or an out-of-home write. Seeds run in normal CI.
- CI now runs **`govulncheck`** (known-vulnerability scan) and **Dependabot** keeps
  the CLI-layer dependencies and GitHub Actions up to date.

## [0.7.0] - 2026-06-20

### Added
- **`cct sync` — experimental device-to-device sync over your local network.**
  `cct sync serve` waits for a peer and prints a one-time pairing code;
  `cct sync connect <host:port> --code <code>` joins it. New/grown sessions flow
  **both ways** and are applied through the existing `import --merge` path, so all
  checksum/conflict/mtime guarantees hold and nothing is ever silently overwritten.
  - **Peer-to-peer, no server/cloud.** TLS with per-process self-signed certs;
    the peer is authenticated by a high-entropy pairing code via an HMAC bound to
    both TLS fingerprints (a LAN man-in-the-middle can't forge it). **Zero new
    dependencies** — stdlib only; no mDNS, no PAKE library.
  - **Anti-exfiltration guard:** refuses non-private addresses unless
    `--allow-public` is passed. A connect target is resolved once and the chosen
    IP is dialed directly (closing the DNS-rebinding window), the raw TCP peer is
    re-checked before the TLS handshake, and CGNAT/overlay ranges (e.g. Tailscale's
    `100.64.0.0/10`) count as local.
  - **Hardened from a security pass:** per-phase network deadlines (no stalled-peer
    DoS); `serve` ignores failed pre-auth attempts and keeps waiting for the real
    peer; the pairing code is entered at a prompt rather than passed as `--code`
    (so it stays out of shell history/process lists); peer-supplied hostnames are
    stripped of C0/C1 terminal-control characters before display.
  - **Opt-in and clearly labelled:** requires `--i-understand` because, unlike
    everything else in cct, this sends session data off the machine. Supports
    `--dry-run` (preview only), `--pull-only`/`--push-only`, `--project`, and
    `--tool`.
  - Deferred for now: mDNS discovery + remembered peers (M3) and the desktop Sync
    tab (M4). See [docs/design/lan-sync.md](docs/design/lan-sync.md).

## [0.6.1] - 2026-06-20

### Fixed
- **Imported sessions no longer lag every time you open them.** Import used to
  stamp each session file with today's date instead of the session's real
  last-activity time. The agent's index (Codex's `state_db`) then saw the file as
  "newer than indexed" and re-parsed the whole rollout on *every* open
  (read-repair) — a multi-second "New chat → real title" delay each time. Import
  now restores the original modification time (from the manifest's `updated_at`),
  so the file matches what the index recorded. This also fixes imported sessions
  wrongly sorting to the top as "modified today". Never touches the index/SQLite.

### Added
- **`cct repair-times`.** One-time fix for sessions imported by an earlier version
  with the wrong modification time: it resets each affected file's mtime to the
  newest timestamp recorded inside it (its real last-activity time). Supports
  `--dry-run` and `--tool`. Only changes file modification times — never session
  content and never the index/SQLite — so it is safe to run repeatedly and is a
  no-op once everything is correct.

## [0.6.0] - 2026-06-19

### Added
- **`import --map-cwd-here`.** A shorthand for `--map-cwd` that maps a
  single-project bundle's recorded cwd to the directory you run the command from,
  so you don't have to look up the old path — the sessions appear under the current
  folder's project (in Claude Code, its sidebar group). A bundle spanning several
  projects is rejected as ambiguous (use explicit `--map-cwd`), and the flag can't
  be combined with `--map-cwd`. Available in all three surfaces: the CLI flag, the
  `cct ui` wizard ("Put these sessions under the folder I'm in now"), and the `cct
  app` WebUI ("Put these sessions under the current folder", mapping to the folder
  the app was launched in).

### Changed
- **Project groups are surfaced for Claude Code.** Claude's sidebar groups by
  project folder (`projects/<encoded-cwd>/`); a Claude `import` now always prints a
  **Project groups** summary showing exactly which groups the sessions land in (and
  flags any whose path is missing locally, with a ready-to-paste `--map-cwd` fix).
  `inspect` labels the same list "Project groups" for Claude. No behavior change for
  Codex. The underlying group-preserving/remapping logic (plain import keeps the
  folder; `--map-cwd` rewrites the cwd and moves the transcript into the new group
  folder; cross-agent translate computes the folder from cwd) was already in place;
  this makes it visible and documents it.

### Docs
- Clarified that **Claude Code exports also use the `.codexbundle` extension**
  (default name `claude-sessions.codexbundle`); the extension is historical and
  cosmetic — the manifest's `tool` field, not the file name, identifies the agent.

## [0.5.1] - 2026-06-18

### Security
A first security audit (self-review plus two independent passes) is recorded in
[`docs/security/audit.md`](docs/security/audit.md). The actionable findings are
fixed in this release; **a more detailed audit will come soon.** Also surfaces the
`--strip-images` "not merge-friendly" caveat at export time and in the desktop GUI.
- **Resource limits against malicious bundles (SEC-2/SEC-3).** Import/inspect now
  cap per-entry, metadata, and total uncompressed sizes, the bundle entry count,
  and full `zstd` decompression — so a crafted bundle (a zip/zstd "decompression
  bomb") can no longer exhaust memory, CPU, or disk.
- **Terminal-escape sanitization (SEC-10).** Bundle metadata (preview, cwd, git
  remote, warnings) printed by `inspect`/`list`/`import` and the `ui` wizard is now
  stripped of ANSI/OSC control sequences, so a malicious bundle cannot spoof the
  screen or write the clipboard (OSC 52) during the review step.
- **Hardened `import --clone` (SEC-1/SEC-4).** The git remote and commit come from
  the (untrusted) bundle; clone now rejects git remote-helper transports
  (`ext::`/`fd::`, the command-execution vector) and flag-like values, passes `--`,
  validates the commit as a hex object id, and sets `protocol.ext/fd.allow=never`.
  (Empirically, modern git already blocks `ext::` by default; this is defense in
  depth for permissive configs.)
- **Manifest is now authoritative (SEC-11).** Import and cross-agent translation
  used to trust the ZIP inventory, so a bundle could carry a valid, checksummed
  session file that was absent from `manifest.sessions` — invisible in
  inspect/preview yet still written. Such "hidden session" bundles are now rejected
  before any write (every importable entry must be declared in the manifest with a
  matching checksum).
- **No SMB probe from inspecting a bundle (SEC-12).** The recorded cwd is
  attacker-controlled; statting a UNC path (`\\host\share`) during a read-only
  inspect could trigger outbound SMB and leak Windows NetNTLM credentials. UNC and
  device paths are now reported as not-present without being statted.
- **Handoff preamble sanitized (SEC-10, extended).** Cross-agent translation
  embedded the source bundle's cwd/git metadata into the generated session text;
  those structured fields are now stripped of control/escape sequences (the
  conversation content itself is preserved verbatim).

## [0.5.0] - 2026-06-18

### Added
- **`export --strip-images`.** Shrinks an image-heavy bundle by replacing each
  inline base64 image with a short placeholder, keeping the conversation text. It
  recognizes both the data-URI shape (`data:image/...;base64,...`) and the
  base64-source object shape, and rewrites only the objects on the path to an
  image so everything else stays byte-for-byte. Lossy and opt-in; the export
  reports `Images stripped: N (saved ~X)`. Compressed `.jsonl.zst` sessions are
  decompressed, stripped, and recompressed when `zstd` is available (otherwise
  copied as-is with a warning). Exposed in the desktop GUI too.
  - **Caveat (flagged at export time and in the docs):** a stripped bundle is
    *not* merge-friendly. Because stripping changes the session bytes, `import
    --merge` sees it as diverged from an unstripped copy rather than appending.
    Use `--strip-images` for a fresh, space-saving import, not for incremental
    sync of a session you also keep unstripped elsewhere.

### Fixed
- **Docs:** the Limitations section still claimed "no merge"; `import --merge`
  shipped in 0.4.0. Corrected, and the incremental-sync/strip-images behavior is
  now described accurately.

### Tests
- Added coverage for compressed-`.jsonl.zst` incremental merge (already
  implemented in 0.4.0 but previously untested) and for image stripping.

## [0.4.0] - 2026-06-17

### Added
- **Incremental sync: `import --merge`.** When you work on the same conversation
  from two machines, re-importing used to report the grown session as a conflict
  and skip it. With `--merge`, cct recognizes that session files are append-only
  logs: if your local copy is a byte-prefix of the bundle's version, the session
  simply grew on the other device, so cct **appends only the new messages** to the
  local file instead of re-pasting the whole chat. This is lossless by construction
  (nothing in the local file is dropped), needs no backup, and is idempotent
  (re-importing the same bundle is a no-op). The import reports `Updated (new
  messages appended): N (+M lines)`.
  - When the local copy is already *ahead* of the bundle, that session is left
    untouched and reported as already up to date.
  - A session that genuinely changed on *both* sides stays a conflict. `--merge`
    composes with `--replace-with-backup` / `--import-as-copy`, which resolve those
    true divergences; `--merge` handles the clean append-only case first.
  - Compressed `.jsonl.zst` sessions are compared on their decompressed contents
    when the `zstd` tool is available; without it, a differing compressed session
    stays a conflict with a clear warning.
  - Wired through the CLI, `--json` output (`updated`, `lines_added`,
    `already_ahead`), and the interactive `ui` conflict prompt (offered as the
    recommended "Sync" choice).

- **Desktop GUI (`cct app`) brought to feature parity with the CLI.** The browser
  app previously exposed only a subset of operations; it now covers essentially
  everything:
  - Export: by one project, everything, or a **single session** (by id), an
    optional **`--since`** date/duration filter, and **recipient-based encryption**
    (age recipients or a recipients file → `<bundle>.age`).
  - Import: the new **`--merge`** incremental sync (offered as a "Sync" conflict
    choice), **selective `--session`** import, **`--project`** cwd-mismatch check,
    **cross-agent handoff** (translate the bundle into Codex or Claude Code),
    **`--clone`** of the recorded git remote, and decryption of `.age` bundles via
    an **age identity file**.
  - The two network actions (`--git-push`, `--clone`) carry plain-language hints in
    the UI stating they upload/download **code only — never sessions, never to any
    cct server**, and the export result confirms the exact branch/remote it pushed.
  - Inspecting an encrypted `.age` bundle (via an identity file).
  - **Passphrase** encryption/decryption is the sole intentional gap: the `age`
    CLI requires an interactive terminal for passphrases, which a loopback browser
    has no way to provide, so those bundles stay a terminal-only operation (the UI
    says so and uses recipient/identity key files instead).
  - The CLI and the desktop UI now share one `--since` parser (`bundle.ParseSince`).

## [0.3.0] - 2026-06-16

### Added
- **Cross-agent handoff: translate a session from one agent into the other**
  (`import <bundle> --to codex|claude`). Instead of importing a bundle natively,
  cct converts each session into the *other* agent's format and writes a real,
  discoverable session into that agent's home. It goes through a neutral
  intermediate representation (`agent-session-v1`): the user/assistant
  conversation is preserved, tool calls and command output are summarized to short
  text, and the translated session opens with a plain-language handoff preamble
  (project dir, git, "continue from here"). This is an **honest best-effort
  translation, not a perfect clone** — model/runtime state, exact tool-call replay,
  and provider-specific ids do not cross over. Output is deterministic (stable id
  and timestamps derived from the source), so re-running is an idempotent skip, and
  the source bundle's checksums are verified before anything is read. Works both
  directions (Codex→Claude and Claude→Codex); honored under `--dry-run`.

- **Claude Code session support.** Every existing command now works for Claude
  Code as well as Codex, selected with `--tool claude` (auto-detected when only
  Claude Code is installed). On import, the bundle's recorded tool always decides
  where the sessions go, so a Claude bundle is never written into the Codex home
  or vice versa.
  - `doctor`/`list`/`export`/`inspect`/`import` read and write Claude Code's
    `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl` transcripts (override the home
    with `--claude-home`/`$CLAUDE_HOME`). The encoded project-folder name is
    reproduced exactly (every character outside `[A-Za-z0-9-]`, including `_`,
    becomes `-`).
  - `--map-cwd` for Claude re-encodes the destination project folder **and**
    rewrites the recorded `cwd` on every transcript line (validated: same line
    count, only `cwd` changed). `--import-as-copy` assigns a fresh session id
    (rewritten on every line) under a new `<uuid>.jsonl`. `--replace-with-backup`,
    `--session`, `--since`, `--all`, `--json`, `age` encryption, and the git
    handoff (`--with-git`/`--git-push`/`--clone`) all work unchanged.
  - The interactive `ui` asks which tool to use (when both are installed), and the
    desktop `app` has a Codex/Claude Code toggle in its top bar.
  - cct never touches `~/.claude.json` or the Claude cloud — Claude Code
    rediscovers a dropped-in transcript on its next run, the same scan-and-reconcile
    contract that makes the Codex path safe.
- The bundle manifest now records a `tool` field (`codex`/`claude`). Older bundles
  with no `tool` field are treated as Codex, so they import unchanged.

### Changed
- Branding cleanup: removed remaining traces of the old `codex-sync` name from
  internal artifacts and the desktop UI. The `--replace-with-backup` backup suffix
  is now `.cct-bak-<nanos>` (was `.codexsync-bak-<nanos>`) and the desktop app's
  loopback request header is `X-Cct-Token` (was `X-Codex-Sync-Token`). These are
  internal — no bundle, flag, or on-disk session format changed. **Unchanged for
  compatibility:** the `.codexbundle` extension, the `codex-sync-bundle-v1` bundle
  format version, and the `--codex-home`/`$CODEX_HOME` flags (these name the Codex
  tool, not this project), so existing bundles still import.

## [0.2.0] - 2026-06-16

### Changed
- **Renamed the project from `codex-sync` to `codex-claude-transfer`**, with the
  command shortened to **`cct`**. The old "sync" name was misleading (the tool
  does a deliberate manual *transfer*, not automatic syncing), and the new name
  reflects that it is becoming a multi-agent tool (Codex today, Claude Code
  support in progress). The Go module path
  (`github.com/ahmojo/codex-claude-transfer`), `cmd/` directory (`cmd/cct`), docs,
  workflows, and packaging manifests were renamed. **Unchanged for
  compatibility:** the `.codexbundle` extension, the `codex-sync-bundle-v1` bundle
  format version, and the `--codex-home` / `$CODEX_HOME` flags (these name the
  Codex tool, not this project), so existing bundles still import.

## [0.1.13] - 2026-06-16

### Added
- `cct app`: a desktop GUI. It launches a small **loopback-only** local web
  server and opens your browser to a single-page app with Doctor, Sessions,
  Export, Inspect, and Import views, all backed by the same core as the CLI. It is
  pure standard library (no web framework, no build step), so it ships to every
  platform through the existing release pipeline. Security: it binds to
  `127.0.0.1` only, requires a per-launch random token on every API call (so other
  local processes and web pages cannot drive it), and checks the Host header to
  mitigate DNS-rebinding. It never uploads anything — it is just a local face over
  the existing operations. Flags: `--port` (default: a free port) and
  `--no-browser`.

### Added
- `export --git-push`: opt-in completion of the git handoff. Before exporting, it
  pushes the project's current branch to its own git remote (`git push <remote>
  <branch>`) so the commit recorded in the bundle is actually fetchable on the
  other machine. It uploads **your code to your own remote only — never your
  sessions, and never to any cct service**, and is deliberately
  conservative: it never force-pushes, never pushes tags, and never creates a
  remote (a diverged remote is rejected as a non-fast-forward). Scoped to a single
  project (not `--all`/`--session`); if the push fails, the export stops rather
  than write a bundle that falsely claims the commit is fetchable. The only two
  outbound git actions remain opt-in: `--git-push` and `import --clone`.

## [0.1.11] - 2026-06-15

### Added
- `cct version` (and `--version`) prints the build version plus OS/arch
  and Go version. Release binaries embed the tag via the linker; `go install`
  builds report the module version.
- `cct completion <bash|zsh|fish>` prints a shell completion script for
  the commands and flags.
- `doctor --json` now has machine-readable output too (joining list/inspect/
  export/import).
- Packaging manifests under `packaging/`: a Homebrew formula and a Scoop manifest
  that install the prebuilt release binary.

## [0.1.10] - 2026-06-15

### Added
- `doctor` now reports which optional external tools (`git`, `age`, `zstd`) are
  installed and what each enables, so it is clear up front which opt-in features
  are available on this machine. A missing tool is reported as info, not a
  warning (the core commands need none).
- `--json` output for `list`, `inspect`, `export`, and `import`: prints a single
  stable JSON object on stdout instead of human-readable text, for scripting and
  automation. Human status/warnings still go to stderr, so stdout stays pure JSON
  (with `--clone`, clone progress also moves to stderr in `--json` mode).
- Selective import: `import --session <id>` imports only the bundle session(s)
  whose thread id matches `<id>` (a unique prefix is enough), skipping the rest.
  Repeatable to pick several. An id that matches nothing in the bundle is an
  error (nothing is written). Reported as "Skipped (not selected by --session)".
- Compressed `--map-cwd`: when the external `zstd` tool is installed, `--map-cwd`
  now also rewrites a matching compressed `.jsonl.zst` session by decompressing
  it, rewriting only the `cwd` field (the same narrow, validated change as for
  plain files), and recompressing — additionally verifying the recompressed frame
  decompresses back to the rewritten content before writing. Without `zstd`, a
  matching compressed session is still copied byte-for-byte and reported as not
  remapped.

## [0.1.9] - 2026-06-14

### Added
- `import --import-as-copy`: opt-in conflict resolution that imports the bundle's
  version of a diverged session as a brand-new session — a fresh session id and a
  new rollout filename — instead of skipping it, leaving your local session
  untouched. Like `--map-cwd`, the only mutation is one canonical `session_meta`
  field (here the `id`); every other line is preserved byte-for-byte and the
  result is validated before writing. Compressed (`.jsonl.zst`) conflicts, or
  sessions without a `session_meta` id, stay skipped. Mutually exclusive with
  `--replace-with-backup`. Reported as "Imported as new copies: N" and skipped
  under `--dry-run`. The interactive `ui` now offers it as a third choice when
  conflicts are detected ("keep both").
- Compressed (`.jsonl.zst`) metadata recovery via the external `zstd` tool.
  `export` and `list` now decompress the head of each compressed rollout (when
  `zstd` is on `PATH`) to recover its recorded cwd, thread id, and preview. As a
  result, `export --project` now includes matching compressed sessions (which
  were previously always skipped because their cwd was unknown), and
  `list`/`inspect` show their details and project folders. The compressed files
  are only read, never recompressed or modified, and they are still copied into
  bundles byte-for-byte. When `zstd` is not installed, behavior is unchanged
  (compressed sessions are reported as metadata-unknown). `--map-cwd` still does
  not rewrite compressed sessions (that would require recompression).

## [0.1.8] - 2026-06-14

### Changed
- `cct ui` is much easier to use, especially for import:
  - **Import now reads the bundle first** and shows, in plain language, which
    project folders the sessions came from and which of those are missing on this
    computer. For each missing folder it offers three clear choices — *create
    that folder here*, *point the sessions to a different local folder*, or *skip*
    — and **builds the `--map-cwd` mapping for you**. You never type the old path
    (it comes from the bundle) and you are never asked to compose `OLD=NEW` by
    hand. The wizard can also create the destination folder for you.
  - Conflicts are detected automatically; the wizard only asks whether to replace
    diverged local sessions (keeping backups) when conflicts actually exist.
  - If the bundle recorded a git remote, the wizard offers to clone the code
    (`--clone`), which the interactive flow previously did not expose.
  - The import preview is now a short, plain-English summary (new / already here /
    redirected / differing) instead of the raw dry-run report.
  - Export's "specific project" choice now lets you **pick a project folder from a
    list** (with session counts) instead of typing a path.

### Fixed
- The import wizard could append the same `--map-cwd` mapping several times when a
  user re-entered the old remap loop, producing a "duplicate --map-cwd" error. The
  remap loop is gone — each missing folder is handled exactly once.
- `export --with-git` is no longer silent when the project folder is not a git
  repository (or git is not installed). It now warns clearly that no git metadata
  was recorded, so it is obvious why the imported bundle offers nothing to clone.
  The export wizard also says this immediately when you enable "record git" for a
  folder that is not a repository. (Previously any git warning that came without
  git metadata — including this one and the "no recorded cwd" notice — was dropped
  before reaching the user.)

## [0.1.7] - 2026-06-14

### Fixed
- `cct ui`: path prompts (project, bundle, output, clone, identity) now
  tolerate a path typed with surrounding quotes. Previously a quoted Windows
  path like `"C:\Users\you\project"` kept its literal quotes, so it was treated
  as a relative path and prefixed with the current directory — producing a
  corrupted path such as `C:\Users\you\"C:\Users\you\project"` and "no sessions
  selected". The wizard now strips a single pair of surrounding quotes, and the
  `.age` auto-detection works on quoted bundle paths too.

## [0.1.6] - 2026-06-14

### Added
- Interactive mode: `cct ui` opens a guided terminal menu
  (Export / Import / Inspect / List / Doctor) that asks only the questions
  relevant to your choice, populates the export "pick a session" list from your
  local sessions, prints the exact equivalent `cct …` command, and runs
  it through the same code path as the flags (so behavior is identical and
  nothing is hidden). Imports are always previewed with `--dry-run` first and
  only applied after you confirm. Requires an interactive terminal; in a pipe or
  CI it exits with guidance instead of blocking. Built with
  [charmbracelet/huh](https://github.com/charmbracelet/huh).

### Changed
- The single binary now depends on `charmbracelet/huh` (and its dependencies)
  for the `ui` command. The reusable core packages (`internal/bundle`,
  `sessions`, `codexhome`, `safety`, `git`, `crypt`) remain built only on the Go
  standard library, and the flag-based commands never invoke the TUI.
- Minimum Go version is now 1.23 (required by the TUI dependency).

## [0.1.5] - 2026-06-14

### Added
- cwd discovery: `inspect` now lists the distinct project folders (recorded
  cwds) across a bundle's sessions and flags any that do not exist on the
  current machine. `import` shows the same summary when one or more folders are
  missing (including under `--dry-run`). A missing folder is the #1 reason an
  imported session appears "missing" in Codex — it is hidden from a project's
  sidebar unless a folder at that exact cwd exists — so the output includes a
  ready-to-paste `--map-cwd "<old>=<new>"` hint. The check is read-only
  (`os.Stat`); nothing is created.
- `import --replace-with-backup`: opt-in conflict resolution. When a local
  session has diverged from the bundle's version (a conflict), the local file is
  copied to a sibling backup (`…jsonl.codexsync-bak-<nanos>`, a name Codex
  ignores on its next scan) and then overwritten with the bundle's version, so
  the previous content is always recoverable. Without the flag, conflicts are
  still skipped and never overwritten (the default). Reported as
  "Replaced (backup kept): N" and skipped under `--dry-run`.

## [0.1.4] - 2026-06-14

### Added
- Optional bundle encryption via the external `age` tool
  (https://github.com/FiloSottile/age), keeping cct a single,
  dependency-free binary (like the git integration, it shells out):
  - `export --encrypt-to <recipient>`: encrypt the bundle to one or more age
    recipients (`age1...`, `ssh-ed25519 ...`); repeatable. Output is written to
    `<output>.age` and the plaintext bundle is removed.
  - `export --recipients-file <file>`: encrypt to every recipient in a file.
  - `export --passphrase`: encrypt with an interactive passphrase (mutually
    exclusive with `--encrypt-to`/`--recipients-file`).
  - `import`/`inspect` auto-detect a `.age` bundle and decrypt it to a temporary
    file, requiring `--identity <file>` or `--passphrase`. The temporary
    plaintext is removed when the command finishes.
  - If `age` is not installed, encryption/decryption fails with install guidance
    and nothing else in cct is affected. cct still never uploads.

## [0.1.3] - 2026-06-14

### Added
- `export --session <thread-id>`: export exactly one session by its thread id.
  A unique prefix is enough (like a git short SHA); an ambiguous prefix or no
  match is an error. Ignores cwd filtering and is mutually exclusive with
  `--all` and `--project`. Defaults output to `session-<id>.codexbundle`.
- Git-assisted handoff (read-only, opt-in):
  - `export --with-git`: record the project's git remote, branch, commit, and
    `dirty`/`unpushed` status in the bundle manifest, even with `--all` or
    `--session`. When `--project` is used, git metadata is captured as before.
    Warns when the working tree is dirty or the commit is not on any remote
    (the other machine could not reproduce or fetch it).
  - On `import`, when the bundle records a git remote, the recovery commands
    (`git clone … && git checkout <commit>`) are printed.
  - `import --clone <dir>`: after importing sessions, clone the bundle's
    recorded remote into `<dir>` and check out the recorded commit. Opt-in;
    skipped under `--dry-run`. cct still never pushes or uploads.

## [0.1.2] - 2026-06-14

### Added
- `export --all`: export every session regardless of recorded cwd, into
  `codex-sessions.codexbundle` by default. Compressed `.jsonl.zst` sessions
  (whose cwd is unknown) are included by `--all`, unlike the `--project` filter.
  Mutually exclusive with `--project`.
- `export --since <when>`: only export sessions whose file was updated at or
  after `<when>`. Accepts an absolute date (`YYYY-MM-DD`, UTC midnight) or a
  relative duration (`7d`, `48h`, `90m`). Combines with `--project` or `--all`.

### Documentation
- `docs/safety.md`: documented that `--map-cwd` is the single opt-in exception to
  the "contents are never rewritten" rule, and that uploaded images/attachments
  travel inline (base64) inside the bundle (size + privacy implications).
- `README.md`: documented `--map-cwd`, `--all`, and `--since`; updated the
  limitations and roadmap to reflect what has shipped.

## [0.1.1] - 2026-06-14

### Added
- `--map-cwd OLD=NEW` flag for `import`: rewrites a session's recorded `cwd` from
  `OLD` to `NEW` during import, so sessions land in the right local project without
  needing to create a folder at the original path.
  - Repeatable: use multiple `--map-cwd` flags to handle several path mappings at once.
  - Only the canonical `cwd` field inside the `session_meta` line is rewritten.
  - All non-`session_meta` lines are preserved byte-for-byte.
  - Unknown fields inside `session_meta` are preserved semantically, although the
    `session_meta` line itself is re-serialized as JSON.
  - `.jsonl.zst` (compressed) sessions that match a mapping are copied byte-for-byte
    and reported as unmappable (cannot be rewritten without decompressing).
  - Mapping syntax is validated: `OLD=NEW`, `OLD` must not be empty, `NEW` must be
    absolute, `OLD` and `NEW` must differ, duplicate `OLD` paths are rejected.
  - On Windows, `OLD` matching is case-insensitive.
  - `--dry-run` respects `--map-cwd` — reports counts without writing anything.
  - All original bundle checksums are still verified before any write; after rewriting
    the effective checksum is recomputed from the mutated bytes.

## [0.1.0] - 2026-06-14

### Initial public release

- `doctor` — read-only health check: find Codex home, count sessions, confirm SQLite
  will not be modified.
- `list` — list all discovered Codex sessions with preview, thread id, cwd, and timestamp.
- `export --project <path>` — export sessions for a project into a `.codexbundle` ZIP.
- `inspect <file.codexbundle>` — show a bundle's manifest and contents (read-only).
- `import <file.codexbundle>` — import a bundle into your Codex home. Never overwrites
  existing files; identical files are skipped, conflicts are reported and skipped.
- `--dry-run` for import: validate and report only, write nothing.
- `--include-archived`: also include archived sessions in `list` / `export`.
- `--codex-home <path>` / `$CODEX_HOME`: override the default `~/.codex` location.
- Cross-compiled binaries for Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64).
- Zero external dependencies — single static binary.
- SQLite is never read or written.
- `.codexbundle` files are standard ZIP archives with a manifest and SHA-256 checksums.
- Atomic writes (temp file + rename); path-traversal protection on all bundle entries.
