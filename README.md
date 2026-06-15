# codex-sync

**Export. Move. Import. Continue your local Codex sessions anywhere.**

![CI](https://github.com/ahmojo/Codex_Sync/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v0.1.10-orange)

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

```text
Machine A:  codex-sync export --project .
            → project.codexbundle      (copy this file to Machine B)

Machine B:  codex-sync import ./project.codexbundle
```

If the project lives at a different path on the destination machine, v0.1.1 can
optionally rewrite the recorded working directory during import:

```bash
codex-sync import ./project.codexbundle \
  --map-cwd "/old/project/path=/new/project/path"
```

## 2. Badges

The badges above show CI status, the Go version, the license, and the current
release. CI runs `build`, `vet`, and `test` on Linux, macOS, and Windows.

## 3. What is codex-sync?

`codex-sync` is a CLI for **local Codex session portability**. Its core is three
commands:

- **Export** the Codex session files into one `.codexbundle` — for a project
  (`--project`), for everything (`--all`), or for a single conversation
  (`--session <thread-id>`), optionally filtered by time (`--since`).
- **Inspect** a bundle's contents without extracting anything.
- **Import** a bundle into another machine's Codex home, safely and without
  overwriting existing sessions.

A few optional, opt-in helpers round it out, all keeping everything on your own
machines:

- **See which project folders are missing** — `inspect` (and `import`) list the
  recorded cwds and flag any that don't exist on this machine, the #1 reason an
  imported session looks "missing" in Codex's sidebar.
- **Map cwd paths on import** with `--map-cwd OLD=NEW`, so sessions created at
  one project path appear under the matching project path on the destination.
- **Resolve conflicts** with `--replace-with-backup`, which overwrites a diverged
  local session with the bundle's version while keeping a backup, or with
  `--import-as-copy`, which imports the bundle's version as a brand-new session
  and leaves your local one untouched.
- **Recover compressed sessions** — when the `zstd` tool is installed, `export`
  and `list` read the metadata of `.jsonl.zst` rollouts, so compressed sessions
  are included by `--project` and shown with their cwd and preview.
- **Git-assisted handoff** (`--with-git` on export, `--clone` on import) to move
  the project's code alongside the sessions.
- **Bundle encryption** (`--encrypt-to` / `--passphrase`, via `age`) to protect
  a bundle in transit.

Prefer not to memorize flags? **`codex-sync ui`** opens an interactive, guided
menu that asks only the relevant questions and then builds and runs the
equivalent command for you (it prints the command each time, so you also learn
the flags). Everything it does is still available as plain flags for scripting.

It is intentionally small and predictable.

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
- **Inspect** reads the bundle metadata without extracting or writing anything.
- **Import** verifies the bundle, then copies the rollout files into the right
  place under your Codex sessions directory. It **never** touches SQLite. The
  next time Codex runs, it scans those files and reconciles its own index
  automatically.
- **Import with `--map-cwd`** is opt-in. It rewrites only the canonical `cwd`
  field inside the plain `.jsonl` session's `session_meta` line, then validates
  the resulting JSONL before writing. It does not globally replace paths in chat
  messages, tool output, or other session lines.

Because the JSONL files are the source of truth, imported sessions become visible
as soon as Codex next scans and reconciles them — typically on its next run.

## 6. Installation

### From source (requires Go 1.23+)

```bash
go install github.com/ahmojo/Codex_Sync/cmd/codex-sync@latest
```

Or clone and build:

```bash
git clone https://github.com/ahmojo/Codex_Sync.git
cd Codex_Sync
go build -o codex-sync ./cmd/codex-sync
```

> **Dependencies.** The reusable core (`internal/bundle`, `sessions`,
> `codexhome`, `safety`, `git`, `crypt`) is built only on the Go standard
> library. The single binary depends on [`charmbracelet/huh`](https://github.com/charmbracelet/huh)
> for the optional interactive `ui` command; the flag-based commands work
> without ever invoking it.

### From releases

Download a prebuilt binary for your OS from the
[Releases](https://github.com/ahmojo/Codex_Sync/releases) page and put it on your
`PATH`.

### Requirements

The core commands (`doctor`, `list`, `export`, `inspect`, `import`) need **no
external tools** — codex-sync is a single self-contained binary. A few **optional**
features shell out to a well-known external tool, kept separate so the binary
stays dependency-free. Each is only needed if you use that feature, and codex-sync
fails with clear guidance (or simply skips the enhancement) when the tool is
missing:

| Tool | Needed for | If it's not on your `PATH` |
| ---- | ---------- | -------------------------- |
| [`git`](https://git-scm.com/) | `export --with-git` (record the project's remote/commit) and `import --clone` (fetch the code) | Git metadata is not recorded on export; `--clone` errors. Everything else works. |
| [`age`](https://github.com/FiloSottile/age) | Bundle encryption: `export --encrypt-to`/`--recipients-file`/`--passphrase`, and decrypting a `.age` bundle on `import`/`inspect` | The encrypt/decrypt command errors with install guidance; unencrypted bundles are unaffected. |
| [`zstd`](https://github.com/facebook/zstd) | Reading metadata of compressed `.jsonl.zst` sessions so `export --project` includes them and `list`/`inspect` show their details | Compressed sessions are still copied byte-for-byte, but their cwd/preview are unknown (use `export --all` to include them). |

These tools are read-only from codex-sync's perspective: `git`/`zstd` are only
ever used to *read* (clone/fetch or decompress), `age` to encrypt/decrypt locally.
None of them change codex-sync's "never uploads anything" guarantee.

## 7. Quickstart

The fastest way to get going without learning the flags is interactive mode:

```bash
codex-sync ui
```

It opens a guided menu (Export / Import / Inspect / List / Doctor), asks only the
questions relevant to your choice, prints the exact `codex-sync …` command it
built, and runs it. You pick project folders and sessions **from a list** instead
of typing paths.

On import the wizard reads the bundle first and tells you, in plain language,
which project folders are missing on this computer — then for each one offers to
**create the folder here**, **point the sessions to a different local folder**
(it builds the `--map-cwd` mapping for you, so you never type the old path), or
skip. It detects conflicts automatically and only asks about replacing them when
they exist, and offers to clone the project's code if the bundle recorded a git
remote. Imports are always previewed before anything is written and only applied
after you confirm.

Or use the commands directly:

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

If the project path differs between machines, preview with `--map-cwd` first:

```bash
codex-sync import ./project.codexbundle \
  --map-cwd "/Users/example/dev/project=C:\\Users\\example\\dev\\project" \
  --dry-run

codex-sync import ./project.codexbundle \
  --map-cwd "/Users/example/dev/project=C:\\Users\\example\\dev\\project"
```

After importing, **restart the Codex App (or run Codex again)** so it scans and
reconciles the imported rollout files.

### Git-assisted handoff

A session is only useful on the other machine if the code it refers to is also
there. `--with-git` records the project's git remote, branch, and commit (plus
whether the working tree was dirty or the commit was unpushed) in the bundle, so
the other machine knows exactly what to check out:

```bash
# Source machine — capture the git state alongside the sessions
codex-sync export --project . --with-git
# (warns if the working tree is dirty or HEAD isn't pushed to a remote)

# Destination machine — import sessions AND clone the project in one step
codex-sync import ./project.codexbundle --clone ~/dev/project
```

This stays within the tool's safety model: it only ever **reads** git on export
and, with `--clone`, runs `git clone`/`git checkout` against the recorded remote
on import. It never pushes, creates repositories, or uploads anything. If you
omit `--clone`, import simply prints the `git clone … && git checkout <commit>`
commands for you to run yourself.

Export a single conversation by its thread id (a unique prefix is enough):

```bash
codex-sync export --session 9f3c1a2b   # → session-9f3c1a2b.codexbundle
```

### Encrypting a bundle

Because a `.codexbundle` can contain prompts, code, and accidentally-printed
secrets (see the safety model), you can encrypt it before it ever leaves your
machine. codex-sync shells out to [`age`](https://github.com/FiloSottile/age) —
the same single-binary, dependency-free philosophy as the git integration — so
you need `age` on your `PATH`:

```bash
# Encrypt to an age public key (repeatable) → project.codexbundle.age
codex-sync export --project . --encrypt-to age1qz...

# Or encrypt with an interactive passphrase
codex-sync export --all --passphrase

# inspect/import auto-detect a .age bundle and decrypt it
codex-sync import ./project.codexbundle.age --identity ~/.age/key.txt
codex-sync inspect ./project.codexbundle.age --passphrase
```

The plaintext bundle is removed after encryption, and decryption happens into a
temporary file that is deleted when the command finishes. If `age` is not
installed, encryption/decryption fails with install guidance and nothing else is
affected. codex-sync still never uploads anything.

## 8. Commands

| Command | Description |
| ------- | ----------- |
| `codex-sync ui` | Interactive mode: a guided menu that asks only the relevant questions, prints the equivalent command, and runs it. Imports are previewed with `--dry-run` first. Requires an interactive terminal. |
| `codex-sync doctor` | Read-only health check: finds your Codex home, counts sessions, warns about cwd/compressed files, reports which optional tools (`git`/`age`/`zstd`) are installed, and confirms SQLite will not be modified. |
| `codex-sync list` | Lists discovered Codex sessions (preview, thread id, cwd, source, updated time). |
| `codex-sync export --project <path>` | Exports sessions whose recorded cwd matches `<path>` into a `.codexbundle`. |
| `codex-sync export --all` | Exports every session regardless of recorded cwd (includes compressed `.jsonl.zst`) into `codex-sessions.codexbundle`. |
| `codex-sync export --session <thread-id>` | Exports a single session by thread id (a unique prefix is enough) into `session-<id>.codexbundle`. |
| `codex-sync export --project . --with-git` | Exports and records the project's git remote/branch/commit (and dirty/unpushed status) in the bundle. |
| `codex-sync export --project . --encrypt-to <recipient>` | Exports and encrypts the bundle to an `age` recipient, writing `<output>.age`. |
| `codex-sync import <file.codexbundle> --clone <dir>` | Imports, then clones the bundle's recorded git remote into `<dir>` and checks out the recorded commit. |
| `codex-sync inspect <file.codexbundle>` | Shows a bundle's manifest and contents without extracting anything, and lists the recorded project folders (cwds), flagging any that don't exist on this machine. |
| `codex-sync import <file.codexbundle>` | Imports rollout files into your Codex home. Never overwrites; verifies checksums first. |
| `codex-sync import <file.codexbundle> --dry-run` | Validates and reports what *would* happen, writing nothing. |
| `codex-sync import <file.codexbundle> --map-cwd OLD=NEW` | Imports while rewriting matching sessions' recorded cwd from `OLD` to `NEW` (plain `.jsonl`, and `.jsonl.zst` when `zstd` is installed). Repeatable. |
| `codex-sync import <file.codexbundle> --session <id>` | Imports only the session(s) in the bundle matching `<id>` (a unique prefix is enough; repeatable), skipping the rest. |
| `codex-sync import <file.codexbundle> --replace-with-backup` | On a conflict (a local session that diverged from the bundle), overwrites the local file with the bundle's version after saving a backup next to it. |
| `codex-sync import <file.codexbundle> --import-as-copy` | On a conflict, imports the bundle's version as a brand-new session (fresh id + filename), leaving the local one untouched. Mutually exclusive with `--replace-with-backup`. |
| `codex-sync help` | Show help. |

### Common flags

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--codex-home <path>` | all | Use a specific Codex home instead of the default (also honors `$CODEX_HOME`). |
| `--project <path>` | export, import | Export: filter sessions by recorded cwd. Import: check for cwd mismatch. |
| `--all` | export | Export every session regardless of cwd (includes compressed sessions). Mutually exclusive with `--project`. |
| `--session <thread-id>` | export, import | Export: export exactly one session by thread id (a unique prefix is enough); mutually exclusive with `--all` and `--project`. Import: import only the session(s) with this thread id; repeatable to pick several. |
| `--since <when>` | export | Only export sessions updated at/after `<when>`. Accepts a date (`YYYY-MM-DD`) or a duration (`7d`, `48h`, `90m`). Combines with `--project` or `--all`. |
| `--with-git` | export | Also record the project's git remote/branch/commit (and dirty/unpushed status) in the bundle, even with `--all` or `--session`. |
| `--output, -o <path>` | export | Bundle output path (default `<project>.codexbundle`, or `codex-sessions.codexbundle` with `--all`, or `session-<id>.codexbundle` with `--session`). |
| `--include-archived` | list, export | Also consider archived sessions. |
| `--json` | list, inspect, export, import | Print a machine-readable JSON summary on stdout instead of human-readable text (for scripting). |
| `--dry-run` | import | Write nothing; just report. |
| `--map-cwd OLD=NEW` | import | Opt-in cwd rewrite for matching sessions. Plain `.jsonl` always; `.jsonl.zst` too when the `zstd` tool is installed (otherwise copied byte-for-byte). |
| `--replace-with-backup` | import | Opt-in. On a conflict, back up the local file (to a sibling `…jsonl.codexsync-bak-*` that Codex ignores) and overwrite it with the bundle's version. Default is to skip conflicts and never overwrite. |
| `--import-as-copy` | import | Opt-in. On a conflict, import the bundle's version as a brand-new session (fresh id + filename) instead of skipping it, leaving the local session untouched. Plain `.jsonl` only; mutually exclusive with `--replace-with-backup`. |
| `--clone <dir>` | import | After importing, clone the bundle's recorded git remote into `<dir>` and check out the recorded commit. Opt-in; needs a git remote recorded in the bundle (by `--project` or `--with-git` on export). |
| `--encrypt-to <recipient>` | export | Encrypt the bundle to an `age` recipient (`age1...`/`ssh-ed25519 ...`); repeatable. Writes `<output>.age`. Requires `age` on `PATH`. |
| `--recipients-file <file>` | export | Encrypt to every `age` recipient listed in `<file>`. |
| `--passphrase` | export, import, inspect | Export: encrypt with an interactive passphrase. Import/inspect: decrypt a passphrase-encrypted bundle. |
| `--identity <file>` | import, inspect | `age` identity (private key) file used to decrypt a `.age` bundle. |

## 9. Safety model

`codex-sync` is designed to be safe by default:

- **Checksums are verified before anything is written.** A corrupt or tampered
  bundle aborts the import with nothing changed.
- **Dry-run support.** `--dry-run` validates and reports exactly what would
  happen without touching disk.
- **No silent overwrites.** A new file is written, an identical file is skipped,
  and a file that differs is reported as a **conflict and skipped** — never
  replaced unless you opt in with `--replace-with-backup`.
- **Conflicts are skipped by default.** Your existing local sessions are never
  modified. With `--replace-with-backup`, a conflicting local file is first
  copied to a sibling backup (which Codex ignores) and only then overwritten, so
  the previous content is always recoverable.
- **SQLite is never modified.** Codex rebuilds its own index from the JSONL files.
- **Path-traversal / zip-slip and absolute paths are rejected.** Only
  `sessions/YYYY/MM/DD/` rollout files are imported.
- **Atomic writes** (temp file + rename) so files are never left half-written.
- **Default import is byte-for-byte.** Session content is only changed if you
  explicitly pass `--map-cwd`.
- **`--map-cwd` is intentionally narrow.** It rewrites only the `cwd` field in
  the `session_meta` line; all non-`session_meta` lines are preserved
  byte-for-byte. For `.jsonl.zst` files it does nothing unless the `zstd` tool is
  installed, in which case it decompresses, rewrites only that field, and
  recompresses after verifying the round-trip.

See [`docs/safety.md`](docs/safety.md) for the full safety and privacy details,
including the important note that **`.codexbundle` files may contain sensitive
data**.

## 10. Bundle format

A `.codexbundle` is a ZIP archive with this layout:

```text
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
- Compressed rollout files (`.jsonl.zst`) are copied into the bundle
  **byte-for-byte** and never recompressed or modified. Their metadata (cwd,
  thread id, preview) may be recovered read-only on export when the `zstd` tool
  is installed (see §11).

## 11. Limitations

- **Codex internals may change.** Parsing is defensive, but Codex's on-disk
  format can drift. Re-check after Codex updates.
- **`.jsonl.zst` metadata recovery needs the external `zstd` tool.** When `zstd`
  is on your `PATH`, `export` and `list` decompress the head of each compressed
  rollout (read-only) to recover its cwd, thread id, and preview, so compressed
  sessions are included by `--project` and shown with their details. Without
  `zstd` installed, their cwd is unknown, so they are skipped by the `--project`
  filter (use `--all` to include them) and shown without metadata.
- **`.jsonl.zst` cwd mapping needs the external `zstd` tool.** When `zstd` is
  installed, `--map-cwd` rewrites a matching compressed session by decompressing,
  rewriting only its `cwd`, and recompressing (verifying the round-trip first).
  Without `zstd`, a compressed session that matches a mapping is copied
  byte-for-byte and reported as not remapped.
- **Project-specific visibility depends on matching cwd paths.** Codex's
  per-project sidebar filters by the session's recorded working directory. If
  your project lives at a different path on the two machines, an imported session
  may not appear in that project's view until you import with an appropriate
  `--map-cwd OLD=NEW` mapping.
- **No global path rewriting.** `--map-cwd` only changes the canonical `cwd` field
  in `session_meta`; it does not rewrite paths mentioned in prompts, assistant
  messages, terminal output, or other JSONL lines.
- **No automatic merge.** Sessions are copied, not merged.
- **No cloud sync.**
- **Terminal UI only, no desktop GUI yet.** `codex-sync ui` is an interactive
  *terminal* wizard; there is no graphical desktop app (a Wails wrapper is on the
  roadmap).
- **Encryption requires the external `age` tool** (no crypto is embedded).

## 12. Roadmap

Already shipped since v0.1.0: `--map-cwd` (v0.1.1), `export --all` and
`export --since` (v0.1.2), `export --session`, `export --with-git` and
`import --clone` (v0.1.3), optional `age` bundle encryption (v0.1.4),
cwd discovery in `inspect`/`import` and `import --replace-with-backup` (v0.1.5),
an interactive `ui` mode (v0.1.6), `import --import-as-copy` and read-only
`.jsonl.zst` metadata recovery via the external `zstd` tool (v0.1.9),
`doctor` optional-tool checks, `--json` output, selective `import --session`,
and compressed-session `--map-cwd` via `zstd` (v0.1.10).

Planned, explicitly **not** in v0.1.x:

- Optional `git push`/repo-creation on export (the upload half of handoff),
  clearly separated and opt-in — codex-sync does not upload anything today
- A desktop app wrapper later, reusing the same Go core
- Optional Claude support later (not in v0.1.x)

Never planned: cloud sync, accounts, hosting, background sync, direct SQLite
writes, global JSONL path rewriting, or automatic merge.

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
- Treat any feature that mutates session content, including cwd mapping, as
  security-sensitive and test it with fake Codex homes only.
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
