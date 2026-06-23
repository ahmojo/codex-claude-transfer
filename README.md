# codex-claude-transfer

**Transfer your local Codex & Claude Code sessions between machines.**
The command is **`cct`**.

![CI](https://github.com/ahmojo/codex-claude-transfer/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v0.9.0-orange)

> ⚠️ **Unofficial.** Not affiliated with or endorsed by OpenAI or Anthropic.
> These tools' internals can change at any time and break this tool. Use at your
> own risk — see the [Disclaimer](#disclaimer).

> **Status:** Works with both **Codex** and **Claude Code**. Pick the agent with
> `--tool codex` / `--tool claude` (auto-detected when only one is installed), and
> **move a session from one agent to the other** with `import --to codex|claude`.
> The Claude Code storage format and its file-based resume contract were verified
> empirically against a live install (see
> [`docs/research/claude-code-sessions-investigation.md`](docs/research/claude-code-sessions-investigation.md)).

`cct` is a small, local-only CLI that moves your
[Codex](https://github.com/openai/codex) and
[Claude Code](https://github.com/anthropics/claude-code) sessions between machines
by hand. You export a project's sessions into one `.codexbundle` file, copy it
across however you like (USB stick, `scp`, Syncthing, an encrypted drive), and
import it on the other machine. **No cloud, no account, no server, no daemon** —
and the agent's index/state is never touched.

```text
Machine A:  cct export --project .      →  project.codexbundle
                        ⇣  (copy the file across yourself)
Machine B:  cct import ./project.codexbundle

# Claude Code instead of Codex:
Machine A:  cct export --tool claude --project .
Machine B:  cct import ./project.codexbundle      # the bundle knows it's Claude
```

## Demo

Export on one machine, then incrementally sync onto the other — only what's new
is appended, nothing is ever re-pasted or overwritten:

![Overview: doctor, grouped list, export](demo/clips/01-overview.gif)

![Incremental sync with import --merge](demo/clips/02-sync.gif)

There's also a local desktop GUI (`cct app`) with the same features:

![The cct desktop WebUI](demo/clips/10-webui.gif)

More clips in [`demo/`](demo/): [LAN sync](demo/clips/15-sync.gif),
[full-text search](demo/clips/11-search.gif),
[secret scan & redact](demo/clips/12-secrets.gif),
[Markdown export](demo/clips/13-markdown.gif),
[repair-times](demo/clips/14-repair-times.gif),
[cross-agent handoff](demo/clips/03-claude-handoff.gif),
[encryption](demo/clips/04-encryption.gif),
[conflict resolution & cwd remap](demo/clips/05-conflicts-remap.gif),
[export filters](demo/clips/06-export-filters.gif),
[git handoff](demo/clips/07-git-handoff.gif), the
[interactive `cct ui` wizard](demo/clips/08-cli-ui.gif), and
[reading compressed `.jsonl.zst` sessions](demo/clips/09-compressed.gif). All
recordings use throwaway demo sessions — never a real `~/.codex` or `~/.claude`.

## How it works

Each agent stores a session as a durable JSONL file, with a rebuildable index
alongside it that `cct` never writes:

- **Codex** → `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (its SQLite DB is the
  index).
- **Claude Code** → `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl` (its
  `~/.claude.json` holds only config, not a session index).

`cct` works only with those JSONL files: **export** packages them (with a manifest
and SHA-256 checksums) into a `.codexbundle` ZIP, and **import** copies them back
into place after verifying every checksum. It never writes the index — each agent
re-discovers the files itself on its next run, so imported sessions show up the
next time you start it. The bundle records which agent it came from, so import
always writes to the right home.

> **Note on the `.codexbundle` extension.** Every bundle uses the `.codexbundle`
> extension — including Claude Code exports (whose default file name is
> `claude-sessions.codexbundle`). The name is historical (the tool began as a
> Codex-only utility) and is kept for compatibility; the extension does **not**
> mean the bundle holds Codex sessions. What's actually inside is recorded in the
> manifest's `tool` field (`codex` or `claude`), and `inspect`/`import` read that,
> not the file name. If you prefer, pass `-o my-project.claudebundle` — the
> extension is purely cosmetic and any name works.

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

# For Claude Code, add --tool claude to doctor/list/export
# (import auto-detects the agent from the bundle):
cct list --tool claude
cct export --tool claude --project .
```

After importing, **run the agent again** (restart Codex, or relaunch Claude Code)
so it re-scans the files.

## Desktop app (optional)

If you'd rather click than type, `cct app` gives you a small graphical
interface with **Doctor, Sessions, Export, Inspect, and Import** views. It is at
**feature parity with the CLI**: export by project / everything / one session,
with `--since`, git record/push, image stripping, and recipient-based encryption;
import with
preview, incremental `--merge` sync, conflict handling, cwd remap/check, selective
sessions, cross-agent handoff, and git clone. Project folders and sessions are
shown in lists and every import is previewed before anything is written. The one
exception is **passphrase** encryption/decryption: the `age` CLI reads a passphrase
only from an interactive terminal, so the browser uses age *recipient/identity key
files* instead — passphrase bundles stay a terminal-only operation.

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

**Keep a session in sync as it grows (incremental sync).** When you work on the
same conversation from two machines, re-importing normally reports the grown
session as a conflict. Add `--merge` and cct recognizes that the session is
append-only and simply **appends the new messages** to your local copy — it never
re-pastes the whole chat:

```bash
# Desktop: export, work more, export again. Laptop: import --merge to catch up.
cct import ./project.codexbundle --merge
# -> Updated (new messages appended): 1 (+12 lines)
```

It's lossless by construction (your local copy is a prefix of the bundle's, so
nothing is lost), needs no backup, and is idempotent — importing the same bundle
twice is a no-op. If your laptop is *ahead* of the bundle, that session is left
untouched ("already up to date"). A session that genuinely changed on *both* sides
stays a conflict; combine `--merge` with the resolution flags below to handle those
too.

**Resolve a diverged session.** By default, a local session that differs from the
bundle is reported as a conflict and skipped. Opt into `--replace-with-backup`
(overwrite, keeping a backup) or `--import-as-copy` (import the bundle's version
as a brand-new session, leaving yours untouched).

**Move work between agents (cross-agent handoff).** `import --to <agent>` doesn't
import the bundle natively — it **translates** each session into the *other*
agent's format and writes a real, discoverable session into that agent's home:

```bash
cct export --project .                 # a Codex bundle
cct import ./project.codexbundle --to claude   # continue it in Claude Code

cct export --tool claude --project .   # a Claude Code bundle
cct import ./project.codexbundle --to codex     # continue it in Codex
```

The translated session opens with a short handoff note (project dir, git, "continue
from here") and replays the prior conversation as text. It's an **honest
best-effort handoff, not a perfect clone**: the conversation and project context
cross over, but tool calls and command output are summarized rather than replayed,
and model/runtime state does not transfer. It's deterministic, so re-running is an
idempotent skip rather than a duplicate.

**Project groups are preserved.** Claude Code groups its sidebar by project, and
that grouping comes entirely from the folder a transcript lives in
(`projects/<encoded-cwd>/`) — not from anything inside the JSONL. `cct` carries
that folder through, so groups travel with the bundle automatically:

- A plain `import` writes each transcript back into the same project folder, so on
  a machine where the project sits at the same path the sessions land in the same
  sidebar group.
- When the project lives at a **different** path on the target machine, remap it on
  import — `cct` rewrites the recorded `cwd` *and* moves the transcript into the new
  group folder, so it shows under the project's location here:

  ```bash
  cct import ./claude-sessions.codexbundle \
    --map-cwd "/home/me/dev/app=C:\Users\me\dev\app"
  ```

  Or, if you just want them under the folder you're standing in, skip looking up
  the old path and use the shorthand — `--map-cwd-here` maps the bundle's project
  to the current directory (single-project bundles only):

  ```bash
  cd C:\Users\me\dev\app
  cct import ./claude-sessions.codexbundle --map-cwd-here
  ```

- A cross-agent `import --to claude` computes the right group folder from each
  session's recorded cwd, so translated Codex sessions are grouped too.

`inspect` and a Claude `import` print a **Project groups** summary so you can see
exactly which groups the sessions will land in, and flag any whose path doesn't
exist locally (with a ready-to-paste `--map-cwd` line to fix the grouping).

## LAN sync (experimental)

Skip the file entirely when both machines are on the same network. On one device:

```bash
cct sync serve --i-understand
#   On the other device run:  cct sync connect 192.168.1.20:<port> --i-understand
#   When it asks, enter this pairing code:  4YIX-FE35-T5OT-L2EM-C75Q
```

On the other device — you'll be prompted for the code (so it never lands in your
shell history or process list):

```bash
cct sync connect 192.168.1.20:54321 --i-understand
# Enter the pairing code shown on the other device: 4YIX-FE35-T5OT-L2EM-C75Q
```

New and grown sessions flow **both ways** and are applied through the same
`import --merge` path as a file bundle — so checksums are verified, append-only
growth is merged losslessly, and genuinely diverged sessions are reported as
conflicts, **never overwritten**. Use `--dry-run` to preview, `--pull-only` /
`--push-only` to go one direction, `--project` / `--tool` to scope it, and `--json`
for scripting.

If a synced session's project lives at a **different path** on the receiving
machine, it can land "hidden" (same cwd gotcha as import). Sync warns you when that
happens; re-sync with `--map-cwd-here` (place them under the folder you're in) or
`--map-cwd "<old>=<local path>"` to fix the grouping.

> **Firewall note.** Running `cct sync serve` binds a listener, so the first time,
> Windows/macOS may pop a firewall dialog — allow it for **private networks only**.
> If multicast/discovery is blocked on the network, the manual `connect <host:port>`
> shown here is the way (there is no auto-discovery yet).

Why it's safe, and why it's `--i-understand`:

- It is **peer-to-peer** — no server, no relay, no cloud, no account. The two
  devices talk **directly**.
- The connection is **TLS**, and the peer is **authenticated by the one-time code**
  (an HMAC bound to both certificate fingerprints), so a device on your network
  can't silently interpose.
- It **refuses to talk to a non-private address** (`--allow-public` to override),
  keeping "local network only" an enforced rule rather than a promise.
- Still: unlike everything else in cct, this **sends session data off the machine**.
  That's why it's opt-in, experimental, and requires `--i-understand`. There is no
  auto-discovery yet — you type the peer's `host:port` (see
  [docs/design/lan-sync.md](docs/design/lan-sync.md) for the roadmap and threat
  model).

## Command reference

| Command | Description |
| ------- | ----------- |
| `cct app` | Launch the desktop GUI: a loopback-only local web app that opens in your browser. Nothing is uploaded. |
| `cct ui` | Interactive guided menu; builds and runs the commands below (and prints each one). Requires a terminal. |
| `cct doctor` | Read-only health check: the agent's home, session counts, missing-cwd and optional-tool (`git`/`age`/`zstd`) status. Use `--tool` to pick Codex or Claude Code. |
| `cct list` | List discovered sessions (preview, thread id, cwd, source, updated time). |
| `cct search <query>` | Full-text search across your sessions' conversation text (`--regex`, `--case-sensitive`, `--project`, `--since`, `--json`). Find which session discussed something, then export it. |
| `cct scan` | Check sessions for likely secrets (API keys, tokens, private keys) before sharing or syncing. Read-only; values are masked. |
| `cct stats` | Summarize your sessions: totals, busiest projects, and a recent-activity sparkline (`--json`). |
| `cct resume [query]` | Find the best-matching session (by thread-id prefix or conversation text) and print the agent command that continues it; `--run` launches it now. |
| `cct browse` | Interactive session browser: search, pick one, then resume / export / tag / name it. Requires a terminal. |
| `cct tag add\|rm\|ls` / `cct name` | Annotate sessions with cct-only tags and friendly names, stored in cct's own config dir — never written into the agent's session files. |
| `cct config list\|get\|set\|path` | Save defaults (tool, homes, port) so you stop retyping flags. An explicit flag always wins. |
| `cct export [--project <path> \| --all \| --session <id>]` | Package matching sessions into a `.codexbundle`. `--format md\|html` writes a readable document instead. By default export **refuses to write a bundle that contains a likely secret** — use `--redact` to mask them or `--allow-secrets` to override. |
| `cct inspect <bundle>` | Show a bundle's manifest and contents, read-only, and flag any recorded project folder that's missing locally. |
| `cct import <bundle>` | Import session files into the matching agent's home (or translate across agents with `--to`). Verifies checksums; never overwrites by default. |
| `cct repair-times` | One-time fix for sessions imported by an older version with the wrong modification time (which made the agent re-parse them on every open). Resets each file's mtime to its real last-activity time. Only changes mtimes — never content or the index. Supports `--dry-run`. |
| `cct sync serve` / `cct sync connect [host:port]` / `cct sync daemon` | **Experimental.** Device-to-device session sync over your local network (peer-to-peer, no server/cloud), authenticated with a one-time pairing code. `connect` with no address auto-discovers a peer on the LAN; `daemon` watches your sessions and keeps remembered peers in sync automatically (no code). Refuses non-private addresses; requires `--i-understand`. See below. |
| `cct version` | Print the version (also `--version`). |
| `cct completion <bash\|zsh\|fish>` | Print a shell completion script. |
| `cct help` | Show help. |

### Flags

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--tool <codex\|claude>` | all | Which agent to act on. Default: auto-detect (Claude Code if only it is installed, else Codex). On import the bundle's recorded tool always wins. |
| `--codex-home <path>` | all | Use a specific Codex home instead of the default (also honors `$CODEX_HOME`). |
| `--claude-home <path>` | all | Use a specific Claude Code home instead of `~/.claude` (also honors `$CLAUDE_HOME`). |
| `--project <path>` | export, import | Export: filter sessions by recorded cwd. Import: warn on cwd mismatch. |
| `--all` | export | Export every session regardless of cwd. Mutually exclusive with `--project`. |
| `--session <id>` | export, import | Export: exactly one session by thread id (unique prefix); excludes `--all`/`--project`. Import: only the matching session(s); repeatable. |
| `--since <when>` | export | Only sessions updated at/after a date (`YYYY-MM-DD`) or duration (`7d`, `48h`, `90m`). |
| `--with-git` | export | Also record the project's git remote/branch/commit (and dirty/unpushed status). |
| `--git-push` | export | Opt-in. Push the project's current branch to its own git remote first, so the recorded commit is fetchable on the other machine. Uploads your code only, never sessions; never force-pushes. Needs a project and a remote. |
| `--strip-images` | export | Replace inline base64 images with a small placeholder to shrink the bundle. Lossy (pictures dropped, text kept); needs `zstd` for `.jsonl.zst`. |
| `--output, -o <path>` | export | Bundle output path (defaults derived from `--project`/`--all`/`--session`). |
| `--include-archived` | list, export | Also consider archived sessions. |
| `--json` | doctor, list, inspect, export, import | Print a machine-readable JSON summary on stdout instead of text. |
| `--dry-run` | import | Validate and report only; write nothing. |
| `--to <codex\|claude>` | import | Cross-agent handoff: translate the bundle's sessions into the *other* agent's format and write them into that agent's home (best-effort: conversation + context preamble, tool calls summarized). |
| `--regex` / `--case-sensitive` | search, export | Treat the query (`search` or `export --match`) as a regular expression / match case-sensitively. |
| `--match <query>` | export | Bundle only sessions whose conversation text matches the query. |
| `--format md\|html` | export | Render the selected session(s) as a readable document — Markdown or self-contained HTML (`-o file`, or a directory for several) — instead of a bundle. Not re-importable. |
| `--redact` | export, sync | Replace likely secrets in the exported/sent sessions with placeholders (lossy, opt-in). |
| `--allow-secrets` | export, sync | Proceed even though a likely secret was detected (the default refuses; `--redact` masks instead). |
| `--run` | resume | Launch the agent on the chosen session now, instead of just printing the command. |
| `--remember` | sync | After a code pairing, remember the peer so later syncs between trusted devices skip the code. |
| `--interval <n>` / `--once` | sync daemon | Poll for changes every `<n>` seconds (default 5); `--once` runs a single discover-and-sync sweep then exits. |
| `--map-cwd OLD=NEW` | import, sync | Rewrite matching sessions' recorded cwd. Plain `.jsonl` always; `.jsonl.zst` when `zstd` is installed. Repeatable. |
| `--map-cwd-here` | import, sync | Shorthand for `--map-cwd` that maps the project to the directory you run the command from — no need to look up the old path. Single-project only; can't be combined with `--map-cwd`. |
| `--merge` | import | Incremental sync. When a session grew on the other device (the local file is a prefix of the bundle's), append only the new messages instead of reporting a conflict. Lossless; composes with the resolution flags for genuinely diverged sessions. |
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
  `--merge` (append-only, lossless), `--replace-with-backup`, or `--import-as-copy`.
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

Format version `codex-sync-bundle-v1`. Compressed `.jsonl.zst` rollouts are copied
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
- **No global path rewriting and no cloud sync.** `--map-cwd` only changes the
  `cwd` field in `session_meta`. Incremental sync (`import --merge`) is opt-in and
  only ever *appends* to a session that grew on one side — it never combines edits
  that diverged on both.
- **`--strip-images` is lossy and not merge-friendly.** It shrinks a bundle by
  replacing inline images with a placeholder; the picture bytes are dropped (the
  conversation text stays). Because the content changes, a stripped bundle no
  longer matches an unstripped copy of the same session, so `import --merge` reads
  it as *diverged* instead of appending. Use it for a fresh import to save space —
  not for incremental sync of a session you also keep unstripped elsewhere.
- **The desktop GUI runs in your browser**, not a native window — `cct app`
  serves a local, loopback-only web app (no native packaging, no extra toolchain).
- **Claude Code's format is closed-source and moves fast.** Support was verified
  against a recent install and parses defensively, but re-check after Claude Code
  updates. Sub-agent *sidechains* are carried inside the same transcript file (and
  whole project folders travel together on export); a logical session that spans
  *separate* sidechain files has not been observed and is not specially handled yet.
- **Cross-agent handoff (`--to`) is a translation, not a clone.** It carries the
  conversation and project context and writes a session the target agent can
  discover, but tool calls/command output are summarized (not replayed), and
  model/runtime state, exact tool-call history, and provider-specific ids do not
  transfer. Treat a handed-off session as a primed "continue from here" context,
  not a byte-for-byte continuation.

## Roadmap

Shipped since v0.1.0: `--map-cwd`, `export --all`/`--since`/`--session`/`--with-git`,
`import --clone`, `age` encryption, cwd discovery, `--replace-with-backup`, an
interactive `ui`, `--import-as-copy`, `zstd`-based compressed-session support,
`doctor` tool checks, `--json` output, selective `import --session`,
`version`/`completion` commands, opt-in `export --git-push`, a desktop GUI
(`cct app`, a loopback-only local web app over the same Go core),
**Claude Code support** (`--tool claude`) across every command and both
front-ends, **cross-agent handoff** (`import --to codex|claude`) that
translates a session from one agent into the other, **opt-in incremental
sync** (`import --merge`) that appends only the new messages to a session that
grew on another device, and **`export --strip-images`** to shrink image-heavy
bundles by dropping inline picture data.

Shipped in v0.9.0: **full-text `search`**, **`stats`**, **`resume`** (find a
session and continue it in the agent), an interactive **`browse`**r, cct-only
**`tag`/`name`** annotations, saved-default **`config`**, **`export --format
html`**, a **pre-egress secret gate** (export/sync refuse to write/send a likely
secret unless `--redact` or `--allow-secrets`), and **ambient LAN sync**: peer
**discovery** (`sync connect` with no address), and a **`sync daemon`** that keeps
already-remembered devices in step automatically. The desktop GUI (`cct app`) gained
Search, Stats, and Scan to match.

**Never** planned: cloud sync, accounts, hosting, **any sync to a server or off
your LAN**, direct index/SQLite writes, global path rewriting, automatic (silent
or default) merging of sessions that diverged on both sides, or uploading your
sessions anywhere. The `sync daemon` is the one background process, and it is
strictly opt-in (`--i-understand`), local-network-only, and talks solely to devices
you have already paired and remembered — never the internet. `--merge` is opt-in
and only ever *appends* to an append-only log; it never combines conflicting edits.

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

Unofficial and **not affiliated with or endorsed by OpenAI or Anthropic**. It
works against Codex's and Claude Code's local files based on their behavior at a
point in time, which may change and break it (Claude Code is closed-source and
changes often). `.codexbundle` files can contain sensitive data (see
[`docs/safety.md`](docs/safety.md)). Provided "as is", without warranty.
**Use at your own risk.**
