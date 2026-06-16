# codex-claude-transfer

**Transfer your local Codex & Claude Code sessions between machines.**
The command is **`cct`**.

![CI](https://github.com/ahmojo/codex-claude-transfer/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v0.1.13-orange)

> ⚠️ **Unofficial.** Not affiliated with or endorsed by OpenAI or Anthropic.
> These tools' internals can change at any time and break this tool. Use at your
> own risk — see the [Disclaimer](#disclaimer).

> **Status:** **Codex** session portability works today. **Claude Code** support
> is in progress — the storage format and the file-based resume contract have been
> verified (see [`docs/research/claude-code-sessions-investigation.md`](docs/research/claude-code-sessions-investigation.md)),
> and the shared export/import core is being extended to it. The name reflects
> that direction.

`cct` is a small, local-only CLI that moves your
[Codex](https://github.com/openai/codex) sessions between machines by hand (with
[Claude Code](https://github.com/anthropics/claude-code) support coming). You
export a project's sessions into one `.codexbundle` file, copy it across however
you like (USB stick, `scp`, Syncthing, an encrypted drive), and import it on the
other machine. **No cloud, no account, no server, no daemon** — and the agent's
index/state is never touched.

```text
Machine A:  cct export --project .      →  project.codexbundle
                        ⇣  (copy the file across yourself)
Machine B:  cct import ./project.codexbundle
```

## How it works

Codex stores each session as a durable JSONL **rollout file** under
`~/.codex/sessions/YYYY/MM/DD/`; its SQLite database is just a rebuildable index.
`cct` works only with those rollout files: **export** packages them (with a
manifest and SHA-256 checksums) into a `.codexbundle` ZIP, and **import** copies
them back into place after verifying every checksum. It never writes SQLite —
Codex re-indexes the files itself on its next run, so imported sessions show up
the next time you start it.

## Install

```bash
# From source (Go 1.23+)
go install github.com/ahmojo/codex-claude-transfer/cmd/cct@latest
```

Or download a prebuilt binary from [Releases](https://github.com/ahmojo/codex-claude-transfer/releases),
or build from a clone:

```bash
git clone https://github.com/ahmojo/codex-claude-transfer.git
cd codex-claude-transfer && go build -o cct ./cmd/cct
```

Package manifests for **Homebrew** and **Scoop** live in
[`packaging/`](packaging/) (they install the prebuilt release binary).

The binary is self-contained. Only the interactive `ui` command uses a
third-party library ([`charmbracelet/huh`](https://github.com/charmbracelet/huh));
the core packages are standard-library only.

### Optional external tools

The core commands need nothing extra. A few opt-in features shell out to a
standard tool **if you use them**; without it, that feature errors with guidance
or is simply skipped — nothing else is affected.

| Tool | Enables | Without it |
| ---- | ------- | ---------- |
| [`git`](https://git-scm.com/) | `export --with-git`, `import --clone` | git metadata not recorded; `--clone` errors |
| [`age`](https://github.com/FiloSottile/age) | bundle encryption / decryption | encrypt/decrypt errors; plain bundles unaffected |
| [`zstd`](https://github.com/facebook/zstd) | reading compressed `.jsonl.zst` metadata; `--map-cwd` on compressed sessions | compressed sessions are copied as-is, with cwd/preview unknown |

These tools are only ever used to *read* (clone/fetch, decompress) or to
encrypt/decrypt locally — they never change the "nothing is uploaded" guarantee.

## Quickstart

cct is a CLI first — the commands below are the whole tool. Two **optional**
front-ends are included if you prefer not to type flags: `cct app` (a
graphical app in your browser) and `cct ui` (a guided terminal menu).
Neither is required; everything they do is just the flags.

```bash
cct doctor                           # check it can see your sessions
cct list                             # list discovered sessions
cct export --project .               # → project.codexbundle
# … copy the bundle to the other machine …
cct inspect ./project.codexbundle    # look inside (read-only)
cct import  ./project.codexbundle --dry-run   # preview, write nothing
cct import  ./project.codexbundle             # import for real
```

After importing, **restart Codex (or run it again)** so it re-scans the files.

## Desktop app (optional)

If you'd rather click than type, `cct app` gives you a small graphical
interface with **Doctor, Sessions, Export, Inspect, and Import** views — the same
operations as the CLI, with project folders and sessions shown in lists and every
import previewed before anything is written.

```bash
cct app                  # opens the app in your default browser
cct app --no-browser     # just print the URL (open it yourself)
cct app --port 8765      # pin a port (default: a free one is chosen)
```

**How it works.** It is *not* a separate program or an Electron-style native
window — it is the same `cct` binary serving a tiny web page to your own
browser. On launch it starts a small web server bound to **`127.0.0.1` only**
(your machine, not the network), prints a URL, and opens it. The page talks to the
local server, which runs the exact same export/import code as the CLI. It is
**optional and self-contained**: no extra install, no Node, no toolchain — just
the one binary.

**Why it's safe.** The server is reachable only from your own machine; every
action requires a random token generated fresh each launch (handed to your browser
through the launch URL, never embedded in the page), and requests with a foreign
`Host` header are refused. It **never uploads anything** — the browser is just the
UI over your local files. Stop it with Ctrl-C when you're done. See
[`docs/safety.md`](docs/safety.md) for the details.

## Common workflows

**The project is at a different path on the other machine.** Codex's per-project
sidebar filters by the session's recorded working directory, so remap it on
import (preview with `--dry-run` first):

```bash
cct import ./project.codexbundle \
  --map-cwd "/Users/me/dev/project=C:\\Users\\me\\dev\\project"
```

`inspect` and `import` flag any recorded folder that's missing locally and print a
ready-to-paste mapping.

**Bring the code too (git handoff).** `--with-git` records the project's
remote/branch/commit; `--clone` checks it out on the other side. If your latest
commit isn't pushed yet, add `--git-push` to push your branch to its own git
remote first, so the recorded commit is actually fetchable — it uploads **your
code to your own remote only, never your sessions**. Without `--clone`, import
just prints the `git clone … && git checkout <commit>` commands for you.

```bash
cct export --project . --with-git --git-push
cct import ./project.codexbundle --clone ~/dev/project
```

**Encrypt a bundle in transit** (via [`age`](https://github.com/FiloSottile/age)).
`--encrypt-to <recipient>` (or `--passphrase`) writes `<output>.age` and removes
the plaintext; `import`/`inspect` auto-detect and decrypt a `.age` bundle.

```bash
cct export --project . --encrypt-to age1qz...
cct import ./project.codexbundle.age --identity ~/.age/key.txt
```

**Just one session, or a subset.** `export --session <id>` exports a single
conversation (a unique prefix is enough); `import --session <id>` (repeatable)
imports only the chosen ones.

**Resolve a diverged session.** By default, a local session that differs from the
bundle is reported as a conflict and skipped. Opt into `--replace-with-backup`
(overwrite, keeping a backup) or `--import-as-copy` (import the bundle's version
as a brand-new session, leaving yours untouched).

## Command reference

| Command | Description |
| ------- | ----------- |
| `cct app` | Launch the desktop GUI: a loopback-only local web app that opens in your browser. Nothing is uploaded. |
| `cct ui` | Interactive guided menu; builds and runs the commands below (and prints each one). Requires a terminal. |
| `cct doctor` | Read-only health check: Codex home, session counts, missing-cwd and optional-tool (`git`/`age`/`zstd`) status. |
| `cct list` | List discovered sessions (preview, thread id, cwd, source, updated time). |
| `cct export [--project <path> \| --all \| --session <id>]` | Package matching sessions into a `.codexbundle`. |
| `cct inspect <bundle>` | Show a bundle's manifest and contents, read-only, and flag any recorded project folder that's missing locally. |
| `cct import <bundle>` | Import rollout files into your Codex home. Verifies checksums; never overwrites by default. |
| `cct version` | Print the version (also `--version`). |
| `cct completion <bash\|zsh\|fish>` | Print a shell completion script. |
| `cct help` | Show help. |

### Flags

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--codex-home <path>` | all | Use a specific Codex home instead of the default (also honors `$CODEX_HOME`). |
| `--project <path>` | export, import | Export: filter sessions by recorded cwd. Import: warn on cwd mismatch. |
| `--all` | export | Export every session regardless of cwd. Mutually exclusive with `--project`. |
| `--session <id>` | export, import | Export: exactly one session by thread id (unique prefix); excludes `--all`/`--project`. Import: only the matching session(s); repeatable. |
| `--since <when>` | export | Only sessions updated at/after a date (`YYYY-MM-DD`) or duration (`7d`, `48h`, `90m`). |
| `--with-git` | export | Also record the project's git remote/branch/commit (and dirty/unpushed status). |
| `--git-push` | export | Opt-in. Push the project's current branch to its own git remote first, so the recorded commit is fetchable on the other machine. Uploads your code only, never sessions; never force-pushes. Needs a project and a remote. |
| `--output, -o <path>` | export | Bundle output path (defaults derived from `--project`/`--all`/`--session`). |
| `--include-archived` | list, export | Also consider archived sessions. |
| `--json` | doctor, list, inspect, export, import | Print a machine-readable JSON summary on stdout instead of text. |
| `--dry-run` | import | Validate and report only; write nothing. |
| `--map-cwd OLD=NEW` | import | Rewrite matching sessions' recorded cwd. Plain `.jsonl` always; `.jsonl.zst` when `zstd` is installed. Repeatable. |
| `--replace-with-backup` | import | On a conflict, back up the local file and overwrite it with the bundle's version. |
| `--import-as-copy` | import | On a conflict, import the bundle's version as a new session, leaving yours untouched. Excludes `--replace-with-backup`. |
| `--clone <dir>` | import | After importing, clone the bundle's recorded git remote into `<dir>` and check out its commit. |
| `--encrypt-to <recipient>` | export | Encrypt to an `age` recipient (`age1...`/`ssh-ed25519 ...`); repeatable. Writes `<output>.age`. |
| `--recipients-file <file>` | export | Encrypt to every `age` recipient listed in `<file>`. |
| `--passphrase` | export, import, inspect | Export: encrypt with a passphrase. Import/inspect: decrypt a passphrase-encrypted bundle. |
| `--identity <file>` | import, inspect | `age` identity (private key) file used to decrypt a `.age` bundle. |

## Safety

Safe by default — the full model and privacy notes are in
[`docs/safety.md`](docs/safety.md). In short:

- **Checksums are verified before any write;** a corrupt or tampered bundle
  changes nothing.
- **No silent overwrites:** new files are written, identical ones skipped, and a
  differing one is reported as a conflict and skipped — unless you opt into
  `--replace-with-backup` or `--import-as-copy`.
- **SQLite is never modified;** path-traversal/zip-slip and absolute paths are
  rejected; writes are atomic (temp file + rename).
- **Default import is byte-for-byte.** The only content changes are opt-in and
  narrow: `--map-cwd` (the `cwd` field) and `--import-as-copy` (the `id` field),
  each validated before writing.

> **A `.codexbundle` can contain prompts, code, command output, file paths, and
> accidentally-printed secrets.** Treat it like your shell history plus your
> source tree: don't post it publicly, and encrypt it to move over a channel you
> don't fully control.

## Bundle format

A `.codexbundle` is a ZIP archive:

```text
project.codexbundle
├── manifest.json     # format version, source info, per-session metadata
├── checksums.json    # SHA-256 of every other file (not itself)
└── sessions/YYYY/MM/DD/rollout-…-<uuid>.jsonl[.zst]
```

Format version `cct-bundle-v1`. Compressed `.jsonl.zst` rollouts are copied
in **byte-for-byte** and never recompressed or modified; their metadata may be
read (decompressed) on export when `zstd` is installed.

## Limitations

- **Codex internals may change.** Parsing is defensive, but the on-disk format can
  drift — re-check after Codex updates.
- **Compressed `.jsonl.zst` sessions need `zstd`** to recover their metadata and to
  be remapped with `--map-cwd`; without it they're copied as-is, and their cwd is
  unknown to the `--project` filter (use `--all` to include them).
- **Project visibility depends on matching cwd paths.** If the project lives at a
  different path on each machine, an imported session may not appear under that
  project until you `--map-cwd` it.
- **No global path rewriting, no merge, no cloud sync.** `--map-cwd` only changes
  the `cwd` field in `session_meta`; sessions are copied, not merged.
- **The desktop GUI runs in your browser**, not a native window — `cct app`
  serves a local, loopback-only web app (no native packaging, no extra toolchain).

## Roadmap

Shipped since v0.1.0: `--map-cwd`, `export --all`/`--since`/`--session`/`--with-git`,
`import --clone`, `age` encryption, cwd discovery, `--replace-with-backup`, an
interactive `ui`, `--import-as-copy`, `zstd`-based compressed-session support,
`doctor` tool checks, `--json` output, selective `import --session`,
`version`/`completion` commands, opt-in `export --git-push`, and a desktop GUI
(`cct app`, a loopback-only local web app over the same Go core).

Planned, explicitly **not** in v0.1.x: optional Claude support. **Never** planned:
cloud sync, accounts, hosting, background sync, direct SQLite writes, global path
rewriting, automatic merge, or uploading your sessions anywhere.

## Built with AI assistance

Largely implemented with Claude Opus 4.8 under the maintainer's direction (design,
safety constraints, source investigation, review, and releases). Treat it like any
other open-source code: **review it, test it, and report issues** — the AI is a
tool, not a guarantee of correctness.

## Contributing

PRs welcome. Keep the no-cloud / no-SQLite-writes principles and the import path
safe (no silent overwrites), treat anything that mutates session content as
security-sensitive, test with **fake Codex homes only**, and run
`go fmt/vet/build/test ./...`. See [`CONTRIBUTING.md`](CONTRIBUTING.md). Licensed
under [MIT](LICENSE).

## Disclaimer

Unofficial and **not affiliated with or endorsed by OpenAI**. It works against
Codex's local files based on the open-source code at a point in time, which may
change and break it. `.codexbundle` files can contain sensitive data (see
[`docs/safety.md`](docs/safety.md)). Provided "as is", without warranty.
**Use at your own risk.**
