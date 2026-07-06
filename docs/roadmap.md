# Roadmap and project notes

This page collects longer project-status notes that used to live in the README.

## Shipped

Shipped since v0.1.0:

- `--map-cwd`
- `export --all`
- `export --since`
- `export --session`
- `export --with-git`
- `import --clone`
- `age` encryption
- cwd discovery
- `--replace-with-backup`
- interactive `cct ui`
- `--import-as-copy`
- compressed-session support through `zstd`
- `doctor` optional-tool checks
- `--json` output
- selective `import --session`
- `version` and `completion`
- opt-in `export --git-push`
- desktop GUI (`cct app`)
- Claude Code support (`--tool claude`)
- cross-agent handoff (`import --to codex|claude`)
- opt-in incremental sync (`import --merge`)
- `export --strip-images`

Shipped in v0.9.0:

- full-text `search`
- `stats`
- `resume`
- interactive `browse`
- cct-only `tag` / `name` annotations
- saved-default `config`
- `export --format html`
- pre-egress secret gate for export/sync
- ambient LAN sync: peer discovery and `sync daemon`
- Search, Stats, and Scan in the desktop GUI

## Not planned

These are intentionally out of scope:

- cloud sync
- accounts
- hosted sync
- sync through any server
- direct index or SQLite writes
- global path rewriting
- automatic silent merging of sessions that diverged on both sides
- uploading sessions anywhere

The `sync daemon` is the only background process. It is opt-in, local-network
only, and talks only to devices you have paired and remembered.

## AI assistance

This project was largely implemented with Claude Opus 4.8 under the maintainer's
direction for design, safety constraints, source investigation, review, and
releases.

Treat it like any other open-source code: review it, test it, and report issues.
AI assistance is a tool, not a correctness guarantee.

## Contributing notes

PRs are welcome. Keep the no-cloud and no-SQLite-writes principles, keep the
import path safe, treat session mutation as security-sensitive, test with fake
Codex homes only, and run the documented Go checks.

See [CONTRIBUTING.md](../CONTRIBUTING.md).
