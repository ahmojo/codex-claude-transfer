# Changelog

All notable changes to codex-sync are documented here.

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
