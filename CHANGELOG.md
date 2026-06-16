# Changelog

All notable changes to codex-claude-transfer are documented here.

## [Unreleased]

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
