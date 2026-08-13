# Usage guide

This guide keeps the everyday workflows out of the README. For every command and
flag, see [Command reference](reference.md).

## Quickstart

`cct` is a CLI first. The optional front-ends (`cct app` and `cct ui`) run the
same export/import code.

```bash
cct doctor                           # check it can see your sessions
cct list                             # list discovered sessions
cct export --project .               # -> project.codexbundle
# ... copy the bundle to the other machine ...
cct inspect ./project.codexbundle    # look inside (read-only)
cct import  ./project.codexbundle --dry-run   # preview, write nothing
cct import  ./project.codexbundle             # import for real
cct import  ./project.codexbundle --reconcile # optional Codex discovery now
```

For Claude Code, add `--tool claude` to commands that discover or export
sessions. Import reads the agent from the bundle:

```bash
cct list --tool claude
cct export --tool claude --project .
cct import ./project.codexbundle
```

After importing, run the agent again so it re-scans the files. For a native
Codex import, `--reconcile` is an opt-in alternative: cct launches a short-lived
Codex app-server scoped to the selected `CODEX_HOME`, asks Codex to read any
changed thread IDs missing from its state-backed list, then verifies them through
`thread/list`. cct does not write SQLite or `session_index.jsonl`; the Codex
process owns any index repair. Because app-server is experimental, an unavailable
or incompatible protocol is a non-fatal reconcile failure: the rollout import
remains complete and cct prints restart guidance. It prints an exact
`cct resume <thread-id> --run` fallback only when the imported rollout's
`session_meta.id` is an exact UUID-shaped value and the selected Codex home can
be represented byte-for-byte in the same copy-paste command across sh,
PowerShell, and cmd.exe. Ambiguous paths (for example, ones containing
consecutive backslashes) get restart guidance without a resume command.
With `--json`, the same validated commands are returned on reconcile failure as
`reconcile.fallback_commands`; the field is omitted when no safe command is
available.

## Optional external tools

The core commands need nothing extra. A few opt-in features shell out to a
standard tool if you use them; without it, that feature errors with guidance or
is skipped.

| Tool | Enables | Without it |
| ---- | ------- | ---------- |
| [`git`](https://git-scm.com/) | `export --with-git`, `import --clone` | git metadata not recorded; `--clone` errors |
| [`age`](https://github.com/FiloSottile/age) | bundle encryption / decryption | encrypt/decrypt errors; plain bundles unaffected |
| [`zstd`](https://github.com/facebook/zstd) | reading compressed `.jsonl.zst` metadata; `--map-cwd` on compressed sessions | compressed sessions are copied as-is, with cwd/preview unknown |
| Codex `app-server` | opt-in `import --reconcile` | import succeeds; cct prints restart / resume fallback guidance |

These tools are only used locally. They do not change the "nothing is uploaded"
guarantee.

## Desktop app

If you would rather click than type, `cct app` gives you a small graphical
interface with Doctor, Sessions, Export, Inspect, Import, Search, Stats, and Scan
views. It is feature-parity with the CLI for the core workflows: project export,
single-session export, `--since`, git metadata, image stripping, recipient-based
encryption, import preview, incremental merge, conflict handling, cwd remap,
selective import, cross-agent handoff, and git clone. Post-import Codex
reconciliation is also available: after previewing a native Codex bundle, enable
**Ask Codex to discover and verify imported sessions now**. The browser reports
the native verification result and, if app-server is unavailable or
incompatible, keeps the completed import and shows restart / exact resume
fallback guidance.

The terminal wizard (`cct ui`) offers the same opt-in question for native Codex
imports and then runs the normal `import --reconcile` path.

```bash
cct app                  # opens the app in your default browser
cct app --no-browser     # just print the URL
cct app --port 8765      # pin a port (default: a free one is chosen)
```

It is not an Electron app. The same `cct` binary serves a tiny web page bound to
`127.0.0.1` only. Each launch uses a fresh random token, foreign `Host` headers
are refused, and nothing is uploaded. Stop it with Ctrl-C when done.

Passphrase `age` bundles stay terminal-only because the `age` CLI reads a
passphrase from an interactive terminal. In the browser app, use age
recipient/identity key files.

## Common workflows

### Carry sessions through git

Instead of moving a bundle by hand each time, keep it in git. `cct skill install`
writes a skill into your Claude Code home that teaches the agent the whole loop:

```bash
cct skill install
# -> ~/.claude/skills/cct-session-sync/SKILL.md
```

Restart Claude Code and ask it to sync this project's sessions (or invoke
`/cct-session-sync`). For Codex, append the same instructions to your `AGENTS.md`:

```bash
cct skill print --plain >> ~/.codex/AGENTS.md
```

There are two places the bundle can live. Both work without any agent — the
skill only automates them.

#### A. A separate private session store (recommended)

One private repo holds the history for **all** your projects; each project repo
carries only a small reference file. Chat history never enters the code repo,
which is what you want when the code repo is public or has other contributors.

Set the store up once per machine:

```bash
# create a PRIVATE repo (e.g. you/cct-sessions) first, then:
cct config set repo-sync-repo git@github.com:you/cct-sessions.git
cct config set repo-sync-dir ~/cct-sessions     # optional; this is the default
git clone git@github.com:you/cct-sessions.git ~/cct-sessions
```

And once per project:

```bash
cct skill init                  # writes .cct/sessions.json + .cct/README.md
git add .cct && git commit -m "Point at the session store"
cct skill show                  # what it points at, and the resolved paths
```

The store is organized so a human and an agent can both navigate it:

```text
~/cct-sessions/
  projects/
    my-app/
      claude/
        claude-all.codexbundle        # every Claude Code session for my-app
        groups/
          auth-refactor.codexbundle   # optional: one topic, one file
      codex/
        codex-all.codexbundle
        groups/
    other-project/
```

Save and restore are the ordinary commands, pointed at that path:

```bash
# Save (from the project)
cd ~/cct-sessions && git pull
cct export --project /path/to/my-app --tool claude \
  -o ~/cct-sessions/projects/my-app/claude/claude-all.codexbundle
cd ~/cct-sessions && git add -A && git commit -m "Update my-app sessions"

# Restore (on the other machine, from the project directory)
git clone git@github.com:you/cct-sessions.git ~/cct-sessions
cd /path/to/my-app
cct import ~/cct-sessions/projects/my-app/claude/claude-all.codexbundle \
  --merge --map-cwd-here
```

A **group** is just another export with a filter, written into `groups/`. The
full bundle stays the source of truth; groups overlap with it on purpose:

```bash
cct export --project . --tool claude --match "auth refactor" \
  -o ~/cct-sessions/projects/my-app/claude/groups/auth-refactor.codexbundle
cct export --project . --tool claude --session 9f3c \
  -o ~/cct-sessions/projects/my-app/claude/groups/that-one-chat.codexbundle
```

`.cct/sessions.json` records only the store URL, the project's folder, the
encryption mode, and (in encrypted mode) the age **recipient** — a public key.
It contains no local paths and no private key, so committing it reveals nothing
about your machine. Where the store is cloned is per-machine config.

Because that file lives in the repo, anyone who can commit can change where it
points. `cct skill show` prints the URL with the reminder to confirm it, refuses
remote-helper transports (`ext::`, which can execute a command) and control
characters, and the skill tells the agent to ask before cloning a store the user
did not set up.

#### B. In the project's own repo

Simplest, and only acceptable when the project repo itself is private:

```bash
cct export --project . --tool claude -o .cct/claude.codexbundle
git add .cct && git commit -m "Update .cct session bundle"

# after cloning on the other machine
cct import .cct/claude.codexbundle --merge --map-cwd-here
```

#### Both layouts

`--map-cwd-here` rewrites the recorded project path to the current directory
(so run the import from the project), and `--merge` keeps repeat restores
incremental. Restart the agent afterwards so it rescans, then
`cct resume <thread-id>`.

**A bundle in a repo is the repo's history.** It holds prompts, code, and command
output for everyone with access, permanently. So the skill asks once how you want
it stored and remembers the answer:

```bash
cct config set repo-sync plain        # only for a private repo
# or
cct config set repo-sync encrypted
cct config set repo-sync-recipient age1...
```

In `encrypted` mode every committed bundle gains `.age` and the export leaves no
plaintext behind; restoring needs `--identity` (or `--passphrase`). The export
secret gate still applies in both modes: a likely credential stops the export
instead of committing it.

The skill also tells the agent what not to do on its own — never pass
`--allow-secrets`, never `git push` without asking, never touch `~/.claude.json`
or Codex's SQLite index. Read the whole document with `cct skill print`.

One caveat worth knowing: each save commits a full new copy of the bundle, and
git cannot delta compressed archives, so the repo grows with every commit. If
that becomes a problem, export a window instead: `--since 30d`.

### Know before Claude Code deletes your transcripts

Claude Code removes session files older than `cleanupPeriodDays` from
`~/.claude/settings.json` — **30 days when you never set it** — and it does so
silently: no prompt, no trash. People have lost hundreds of transcripts that way
([#85466](https://github.com/anthropics/claude-code/issues/85466),
[#41458](https://github.com/anthropics/claude-code/issues/41458)).

`cct doctor --tool claude` now reads that setting and projects it onto the
transcripts actually on disk:

```bash
cct doctor --tool claude
# warning: 12 transcript(s) fall out of Claude Code's cleanup window
# (cleanupPeriodDays=30 from settings.json) within 14 days, the first on
# 2026-08-27; archive them with `cct export --all --tool claude -o claude-archive.codexbundle`
```

Then the archive is the one command it names:

```bash
cct export --all --tool claude -o claude-archive.codexbundle
```

Two honest limits. cct cannot stop the cleanup — it only tells you what is next,
early enough to act; keeping the sessions is what the export is for. And the
projection uses each transcript's modification time, which is what an age-based
cleanup goes by, so a file cct itself re-dated (see `repair-times`) is judged by
its corrected time. cct reads nothing from `settings.json` except that one
number: the same file holds API keys, which are none of cct's business.

### Take a project's memory to the other machine

Claude Code keeps what it has learned about a project in
`projects/<encoded-cwd>/memory/`, and keeps it machine-local by design. `cct`
therefore only moves it when you say so twice — once when packing, once when
unpacking:

```bash
cct export --project . --tool claude --with-memory -o project.codexbundle
cct import ./project.codexbundle --with-memory --map-cwd-here
```

Without the flag on export, no memory goes into the bundle. Without it on
import, memory that is in the bundle is skipped and the import says so. Memory
lands under the project the cwd mapping resolves to, so `--map-cwd-here` places
it with the sessions it belongs to.

A memory file that already exists on this machine with **different** content is
never overwritten — it is reported and kept, the same way a diverged session is.
Identical files are counted as already present. Memory is prose the agent wrote
about your project, so it passes through the same pre-egress secret gate as a
transcript: a likely credential in a memory file stops the export.

`cct relocate --tool claude` moves a project's memory on the same machine
without any flag; see below.

### Relocate a project

When a project moves to a different folder on the same machine, `relocate`
packages the matching sessions into a private temporary bundle and feeds it back
through the normal checked import path. Preview first:

```bash
cct relocate /old/project /new/project --dry-run
```

If you already copied or moved the project, `NEW` must exist and the command
updates only the sessions:

```bash
cct relocate /old/project /new/project
```

To have `cct` rename the project directory too, add `--move-project`. This uses
an atomic same-filesystem rename; it intentionally does not fall back to a
copy-and-delete operation:

```bash
cct relocate /old/project /new/project --move-project
```

Archived Codex sessions are excluded by default. Add `--include-archived` to
relocate matching rollouts under `archived_sessions/` through the same backup and
undo path:

```bash
cct relocate /old/project /new/project --include-archived
```

Stop the agent before the real run so it cannot append to a session during
relocation. CCT checks that every selected session still matches the temporary
bundle, backs up each original, records the standard undo journal, and checks the
real import result before reporting success. An import error or incomplete result
restores session backups and rolls the project directory back. `cct undo`
restores session files only; if `--move-project` succeeded, move the project
directory back separately.

If any compressed rollout has unknown cwd metadata, relocation stops before
changing files. Install [`zstd`](https://github.com/facebook/zstd) and retry so
CCT can verify whether every compressed session belongs to the project.

#### Claude Code projects

Claude Code stores a project's transcripts in `projects/<encoded-cwd>/`, so
relocating also moves each transcript into the folder that encodes the new path:

```bash
cct relocate /old/project /new/project --tool claude --dry-run
cct relocate /old/project /new/project --tool claude
```

`--move-project` and `--claude-home` work the same way as for Codex:

```bash
cct relocate /old/project /new/project --tool claude --move-project
cct relocate /old/project /new/project --tool claude --claude-home /path/to/.claude
```

A project's **auto memory** moves with it. Claude Code keeps it in
`projects/<encoded-cwd>/memory/`, keyed by the same encoded path as the
transcripts, so a relocation that moved only the transcripts would leave the
project with its conversations but without anything the agent had learned about
it. Memory files get the same treatment: copied and verified first, originals
removed afterwards, both halves in the undo journal. If the new project folder
already holds a memory file of the same name with **different** content, the
whole relocation stops before anything is written — memory is never overwritten.
An identical file is left as it is and its original is still moved out.

CCT writes every remapped transcript under the new project folder first, and only
then backs up and deletes each original, so a session id is never present twice.
If the new folder already holds a transcript with the same session id, relocation
stops before writing anything. A failure while the originals are being removed
restores them from their backups, deletes the copies, and rolls back a
`--move-project` rename.

`cct undo` reverses both halves: it restores each original transcript first and
then removes the relocated copy — and if an original cannot be restored, the copy
is deliberately kept so the session still exists somewhere. `~/.claude.json` is
never touched; restart Claude Code so it re-scans the transcripts.

`--include-archived` is refused with `--tool claude`, because Claude Code keeps no
separate archive location — every transcript recorded under `OLD` is already
included.

Because Claude's folder encoding is lossy, two different project paths can map to
the same folder name. When `OLD` and `NEW` do, the transcripts stay where they
are and only their `cwd` fields are rewritten — the same in-place rewrite Codex
uses, with no originals to remove.

### Remap a project during import

Codex and Claude Code group sessions by recorded working directory. If a project
lives at a different path on the target machine, remap it on import:

```bash
cct import ./project.codexbundle \
  --map-cwd "/Users/me/dev/project=C:\Users\me\dev\project"
```

`inspect` and `import` flag missing folders and print a ready-to-paste mapping.

For single-project bundles, you can map the old project path to the directory
you are standing in:

```bash
cd C:\Users\me\dev\project
cct import ./project.codexbundle --map-cwd-here
```

### Bring the code too

`--with-git` records the project's remote, branch, commit, and dirty/unpushed
status. `--clone` checks out the recorded commit on the other side. If the
commit is not pushed yet, add `--git-push` to push your code to your own remote
before exporting.

```bash
cct export --project . --with-git --git-push
cct import ./project.codexbundle --clone ~/dev/project
```

This uploads code only to your git remote. It never uploads sessions.

### Encrypt a bundle

Encryption uses [`age`](https://github.com/FiloSottile/age). `--encrypt-to`
writes `<output>.age` and removes the plaintext bundle. `import` and `inspect`
auto-detect encrypted bundles.

```bash
cct export --project . --encrypt-to age1qz...
cct import ./project.codexbundle.age --identity ~/.age/key.txt
```

Passphrase encryption is also available with `--passphrase`.

### Export or import only a subset

Use a thread-id prefix to export or import a single session, or pull a slice out
of a large bundle on import with the same filters `export` uses:

```bash
cct export --session <id>
cct import ./big.codexbundle --session <id>     # one session (repeatable)
cct import ./big.codexbundle --project .         # only this project's sessions
cct import ./big.codexbundle --since 7d          # only recently-updated sessions
cct import ./big.codexbundle --match "auth"      # only sessions about a topic
```

The filters combine (AND). `--match` reads conversation text, so compressed
`.jsonl.zst` sessions are skipped by it.

### Preview an import

`cct diff` shows exactly what an import would do — which sessions are new, which
would grow (and by how many lines), which are already present, and which would
conflict — without writing anything:

```bash
cct diff ./project.codexbundle
#   new        3   would be imported
#   grow       2   would append new messages
#   identical  9   already present, unchanged
#   conflict   1   changed on both sides
```

It accepts the same selection and remap flags as `import`, so the preview matches
the command you are about to run.

### Undo the last import

Commands that write session files flow through the import engine, so `import`
and `relocate` record a small journal you can reverse:

```bash
cct undo --dry-run    # preview what would be undone
cct undo              # delete the files this import created, restore its backups
cct undo --list       # show recent imports
```

Undo only removes or restores a file that still matches what the import wrote, so
anything you edited afterward is never lost — it is reported as skipped instead.
Undoing a Claude Code relocation also puts back the transcripts it removed from
the old project folder, and it restores those before deleting the relocated
copies, so a session is never left with no copy on disk.

### Incremental sync

When you work on the same conversation from two machines, re-importing normally
reports the grown session as a conflict. Add `--merge` and `cct` recognizes that
the session is append-only and appends only the new messages:

```bash
cct import ./project.codexbundle --merge
# -> Updated (new messages appended): 1 (+12 lines)
```

This is lossless when your local copy is a prefix of the bundle's copy. Importing
the same bundle twice is a no-op. If both sides changed independently, the
session remains a conflict.

### Resolve a diverged session

By default, a local session that differs from the bundle is reported as a
conflict and skipped. Opt into one of these:

```bash
cct import ./project.codexbundle --replace-with-backup
cct import ./project.codexbundle --import-as-copy
```

`--replace-with-backup` overwrites after backing up the local file.
`--import-as-copy` writes the bundle's version as a new session.

### Move work between agents

`import --to <agent>` translates each session into the other agent's format and
writes a real, discoverable session into that agent's home:

```bash
cct export --project .
cct import ./project.codexbundle --to claude

cct export --tool claude --project .
cct import ./project.codexbundle --to codex
```

This is an honest handoff, not a byte-for-byte clone. The target session starts
with a short handoff note and includes the prior conversation as text. Tool calls
and command output are summarized rather than replayed.

### Claude Code project groups

Claude Code groups its sidebar by the transcript folder
`projects/<encoded-cwd>/`. `cct` preserves that folder on export/import. If a
project path changes, use `--map-cwd` or `--map-cwd-here` so the imported
session appears under the expected local project group.

`inspect` and Claude imports print a Project groups summary so you can see where
sessions will land.

## LAN sync (experimental)

Skip the bundle file when both machines are on the same private network.

On one device:

```bash
cct sync serve --i-understand
# On the other device run: cct sync connect 192.168.1.20:<port> --i-understand
# Enter the pairing code shown here.
```

On the other device:

```bash
cct sync connect 192.168.1.20:54321 --i-understand
```

New and grown sessions flow both ways through the same merge logic as
`import --merge`: checksums are verified, append-only growth is merged, and
genuinely diverged sessions are reported as conflicts.

Useful sync flags:

```bash
cct sync connect --dry-run
cct sync connect --pull-only
cct sync connect --push-only
cct sync connect --project .
cct sync connect --tool claude
cct sync connect --json
```

`cct sync` is peer-to-peer, uses TLS, authenticates the peer with a one-time code,
and refuses non-private addresses unless you pass `--allow-public`. It is still
the only feature that sends session data off the machine, so it is opt-in,
experimental, and gated by `--i-understand`.

For the threat model and design notes, see
[docs/design/lan-sync.md](design/lan-sync.md).
