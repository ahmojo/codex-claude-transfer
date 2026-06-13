# codex-sync

**Export. Move. Import. Continue your local Codex sessions anywhere.**

![CI](https://github.com/ahmojo/Codex_Sync/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v0.1.0-orange)

> ⚠️ **Unofficial tool.** Not affiliated with or endorsed by OpenAI. Codex's
> internals can change at any time and break this tool. Use at your own risk.
> See the [Disclaimer](#14-disclaimer).

---

## 1. codex-sync

A small, local-only command-line tool that moves your local
[Codex](https://github.com/openai/codex) sessions from one machine to another by
hand — no cloud, no accounts, no server, no background daemon.

You export the sessions for a project into a single `.codexbundle` file, copy
that file to another machine however you like (USB stick, `scp`, Syncthing,
encrypted drive), and import it there. Codex picks the sessions up the next time
it scans.

```
Machine A:  codex-sync export --project .
            → project.codexbundle      (copy this file to Machine B)

Machine B:  codex-sync import ./project.codexbundle
```

## 2. Badges

The badges above show CI status, the Go version, the license, and the current
release. CI runs `build`, `vet`, and `test` on Linux, macOS, and Windows.

## 3. What is codex-sync?

`codex-sync` is a CLI for **local Codex session portability**. It does exactly
three things:

- **Export** the Codex session files for a project into one `.codexbundle`.
- **Inspect** a bundle's contents without extracting anything.
- **Import** a bundle into another machine's Codex home, safely and without
  overwriting existing sessions.

That's the whole tool. It is intentionally small and predictable.

## 4. Why this exists

Codex stores its sessions locally. There is no built-in, account-free way to take
"the session I was working on" to a different computer. `codex-sync` fills that
gap with the simplest mechanism that works:

- **No cloud** — nothing is uploaded anywhere.
- **No accounts** — there is nothing to sign in to.
- **No hosting / no server / no daemon** — it is just a CLI and a file you move.
- **No SQLite writes** — Codex's internal index database is never modified.
- **File-based** — it works directly with Codex's durable session files.

It is for people who want their local sessions to be portable while keeping
everything on their own machines.

## 5. How it works

At a high level, Codex records each session as a JSONL **rollout file** under its
sessions directory, organized by date (`YYYY/MM/DD/`). These JSONL files are
Codex's durable, canonical record of a session. A separate SQLite database exists
only as an **index/cache** that Codex rebuilds from the JSONL files on its normal
list/scan path.

`codex-sync` relies on exactly that contract:

- **Export** discovers the rollout files for a project (matching the session's
  recorded working directory) and packages them — preserving the `YYYY/MM/DD`
  layout — into a `.codexbundle` ZIP with a manifest and checksums.
- **Import** verifies the bundle, then copies the rollout files into the right
  place under your Codex sessions directory. It **never** touches SQLite. The
  next time Codex runs, it scans those files and reconciles its own index
  automatically.

Because the JSONL files are the source of truth, imported sessions become visible
as soon as Codex next scans and reconciles them — typically on its next run.

## 6. Installation

### From source (requires Go 1.22+)

```bash
go install github.com/ahmojo/Codex_Sync/cmd/codex-sync@latest
```

Or clone and build:

```bash
git clone https://github.com/ahmojo/Codex_Sync.git
cd Codex_Sync
go build -o codex-sync ./cmd/codex-sync
```

### From releases

Download a prebuilt binary for your OS from the
[Releases](https://github.com/ahmojo/Codex_Sync/releases) page and put it on your
`PATH`.

## 7. Quickstart

```bash
# Check codex-sync can see your local Codex sessions
codex-sync doctor

# List discovered sessions
codex-sync list

# Export the sessions for the current project
codex-sync export --project .
# → project.codexbundle

# --- copy project.codexbundle to the other machine yourself ---

# Look inside the bundle (read-only)
codex-sync inspect ./project.codexbundle

# Preview the import without writing anything
codex-sync import ./project.codexbundle --dry-run

# Import for real
codex-sync import ./project.codexbundle
```

After importing, **restart the Codex App (or run Codex again)** so it scans and
reconciles the imported rollout files.

## 8. Commands

| Command | Description |
| ------- | ----------- |
| `codex-sync doctor` | Read-only health check: finds your Codex home, counts sessions, warns about cwd/compressed files, and confirms SQLite will not be modified. |
| `codex-sync list` | Lists discovered Codex sessions (preview, thread id, cwd, source, updated time). |
| `codex-sync export --project <path>` | Exports sessions whose recorded cwd matches `<path>` into a `.codexbundle`. |
| `codex-sync inspect <file.codexbundle>` | Shows a bundle's manifest and contents without extracting anything. |
| `codex-sync import <file.codexbundle>` | Imports rollout files into your Codex home. Never overwrites; verifies checksums first. |
| `codex-sync import <file.codexbundle> --dry-run` | Validates and reports what *would* happen, writing nothing. |
| `codex-sync help` | Show help. |

### Common flags

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--codex-home <path>` | all | Use a specific Codex home instead of the default (also honors `$CODEX_HOME`). |
| `--project <path>` | export, import | Export: filter by session cwd. Import: check for cwd mismatch (no rewriting). |
| `--output, -o <path>` | export | Bundle output path (default `<project>.codexbundle`). |
| `--include-archived` | list, export | Also consider archived sessions. |
| `--dry-run` | import | Write nothing; just report. |

## 9. Safety model

`codex-sync` is designed to be safe by default:

- **Checksums are verified before anything is written.** A corrupt or tampered
  bundle aborts the import with nothing changed.
- **Dry-run support.** `--dry-run` validates and reports exactly what would
  happen without touching disk.
- **No silent overwrites.** A new file is written, an identical file is skipped,
  and a file that differs is reported as a **conflict and skipped** — never
  replaced.
- **Conflicts are skipped by default.** Your existing local sessions are never
  modified.
- **SQLite is never modified.** Codex rebuilds its own index from the JSONL files.
- **Path-traversal / zip-slip and absolute paths are rejected.** Only
  `sessions/YYYY/MM/DD/` rollout files are imported.
- **Atomic writes** (temp file + rename) so files are never left half-written.
- **`.jsonl.zst` files are copied byte-for-byte**, never parsed or decompressed.

See [`docs/safety.md`](docs/safety.md) for the full safety and privacy details,
including the important note that **`.codexbundle` files may contain sensitive
data**.

## 10. Bundle format

A `.codexbundle` is a ZIP archive with this layout:

```
project.codexbundle
├── manifest.json          # format version, source info, per-session metadata
├── checksums.json         # SHA-256 of every other file in the bundle
└── sessions/
    └── YYYY/MM/DD/
        └── rollout-YYYY-MM-DDThh-mm-ss-<uuid>.jsonl[.zst]
```

- `manifest.json` — `format_version` (`codex-sync-bundle-v1`), creation
  time/device, source OS, source Codex home, source project path, optional git
  info, and a list of included sessions with their thread id, original path,
  original cwd, preview, timestamps, source, model provider, size, and SHA-256.
- `checksums.json` — maps every bundle file (including `manifest.json`) to its
  SHA-256. It does **not** reference itself.
- Compressed rollout files (`.jsonl.zst`) are copied **byte-for-byte**; they are
  never parsed or decompressed.

## 11. Limitations

- **Codex internals may change.** Parsing is defensive, but Codex's on-disk
  format can drift. Re-check after Codex updates.
- **`.jsonl.zst` metadata parsing is not implemented yet.** Compressed sessions
  are copied byte-for-byte, but their recorded cwd is unknown, so they are
  skipped by the `--project` cwd filter (and reported).
- **Project-specific visibility depends on matching cwd paths.** Codex's
  per-project sidebar filters by the session's recorded working directory. If
  your project lives at a different path on the two machines, an imported session
  may not appear in that project's view even though it imported correctly.
- **No automatic cwd rewriting.** v0.1 detects and warns about cwd mismatch; it
  does not rewrite paths.
- **No automatic merge.** Sessions are copied, not merged.
- **No cloud sync.**
- **No GUI yet.**
- **No Claude support yet.**
- **No encrypted bundles yet.**

## 12. Roadmap

Planned, explicitly **not** in v0.1:

- `export --all`
- `export --since <dur>`
- `export --session <thread-id>`
- Optional encrypted bundles (still no accounts, no hosting)
- Safer path mapping (`--map-cwd`) after research proves it safe
- A desktop app wrapper later, reusing the same Go core
- Optional Claude support later (not in v0.1)

Never planned: cloud sync, accounts, hosting, background sync, direct SQLite
writes, automatic JSONL rewriting, or automatic merge.

## 13. Built with AI assistance

This project was largely implemented with AI assistance. The initial
implementation and tests were developed through an AI-assisted coding workflow
using Claude Opus 4.8 with high-effort reasoning. The design decisions, safety
constraints, source-code investigation, review prompts, and release direction
were guided by the maintainer.

This does not make the code special or exempt from scrutiny. Treat it like any
other open-source code: **review it, test it, and report issues.** The AI is a
tool the maintainer used — not the maintainer, and not a guarantee of
correctness.

## 14. Disclaimer

`codex-sync` is an **unofficial**, community tool and is **not affiliated with or
endorsed by OpenAI**. It interacts with Codex's local files based on the Codex
open-source code as inspected at a point in time. Codex's internals may change
and break this tool. `.codexbundle` files can contain sensitive data — see
[`docs/safety.md`](docs/safety.md). Provided "as is", without warranty of any
kind. **Use at your own risk.**

## 15. Contributing

Contributions are welcome. Please:

- Keep the **no cloud / no accounts / no hosting / no SQLite writes** principles
  intact.
- Keep parsing defensive and the import path safe (no silent overwrites).
- Run the full check suite before opening a PR:

  ```bash
  go fmt ./...
  go vet ./...
  go build ./...
  go test ./...
  ```

- **Never use your real Codex home in tests** — always use a temporary fake
  Codex home.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for details.
