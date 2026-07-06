# Contributing to cct

Thanks for your interest in contributing. `cct` is intentionally small
and conservative — please keep changes focused and safe.

## Before opening a PR

Run the full check suite and make sure everything passes:

```bash
go fmt ./...
go build ./...
go vet ./...
go test ./...
```

## Testing rules

- **Use fake Codex homes in tests.** Build a temporary directory and point the
  code at it.
- **Never use your real `~/.codex` (or `$CODEX_HOME`) in tests.** Tests must not
  read from or write to a real Codex installation.

## Design principles

These are non-negotiable for this project:

- **Do not write to Codex's SQLite database directly.** Codex rebuilds its index
  from the JSONL rollout files; we only touch those files.
- **Do not add cloud, account, hosting, or background-daemon features** without
  discussing it first in an issue. This tool is local-only by design.
- **Keep the import path safe:** verify checksums before writing, reject
  path-traversal/zip-slip and absolute paths, and never overwrite a differing
  session silently (report it as a conflict and skip).
- **Keep parsing defensive.** Tolerate unknown fields and malformed lines rather
  than crashing.
- **Keep the reusable core dependency-free.** The `internal/bundle`, `sessions`,
  `codexhome`, `safety`, `git`, and `crypt` packages must build on the Go
  standard library alone. Third-party dependencies are confined to the CLI/TUI
  layer (e.g. `charmbracelet/huh` powers the interactive `ui` command). New
  dependencies should be discussed in an issue first.

## Scope

If you want to add a larger feature (encryption, path mapping, a GUI, etc.),
please open an issue to discuss it before implementing. See
[`docs/roadmap.md`](docs/roadmap.md) for what is planned and what is explicitly
out of scope.
