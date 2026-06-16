# Claude Code — Session-Storage Investigation

> Status: **Initial findings complete; build not started.** This document records what was
> learned about how **Claude Code** stores its sessions locally, and whether `codex-sync`'s
> file-based export → move → import model can be applied to it (a "codex-sync for Claude Code").
>
> Claude Code is **closed-source**, so — unlike the Codex investigation, which read the
> open-source `openai/codex` repository — these findings come from **inspecting a live install**
> on Windows and from **empirical behavior tests**, not from source. We deliberately did **not**
> use leaked/translated source: it would be stale (Claude Code ships very frequently) and is not
> ours to use. The live install for the exact version you run is the authoritative source anyway.
>
> Everything below reflects **Claude Code v2.1.170 / v2.1.78** on Windows and must be re-verified
> when Claude Code changes its storage format (it changes often).

---

## 0. Scope: Claude *Code*, not the Claude desktop chat app

There are three things people call "Claude", and they split into two storage models:

| Surface | Where sessions live | Portable file model? |
| ------- | ------------------- | -------------------- |
| **Claude Code** in a terminal (`claude`) | `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl` | ✅ yes |
| **Claude Code** launched from the desktop app / an IDE | the **same** `~/.claude/projects/…` files | ✅ yes (identical) |
| The **Claude desktop app's** normal chats (claude.ai client) | cloud, cached locally in `IndexedDB/https_claude.ai…leveldb` | ❌ no |

**In scope:** Claude *Code* session portability. Because Claude Code uses the **same**
`~/.claude/projects/` JSONL store regardless of how it is launched (the per-line `entrypoint`
field just records the front-end), one implementation covers the terminal, the desktop app's
Claude Code panel, and IDE extensions at once.

**Out of scope:** the desktop app's regular chats. They are server-side (account-tied), stored
locally only as an Electron `IndexedDB`/`Local Storage` cache keyed to `https_claude.ai` — there
is no portable session file to move, and reaching them would require the cloud/account, breaking
codex-sync's no-cloud / no-accounts principle. They also already sync when you log in elsewhere,
so portability is moot.

---

## 1. Verified findings (live install)

### Storage layout

```
~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl
```

- One **JSONL transcript per session**, named by the session UUID.
- Grouped into a directory whose name is the project's **working directory, encoded**.
  **Encoding rule (verified by probing tricky paths):** every character that is **not**
  `[A-Za-z0-9_-]` is replaced with `-`, per character. So `:`, `\`, `/`, space, `.`, `(`, `)`,
  `@`, … each become `-`; `_`, `-`, digits, and **case** are preserved. Examples:
  - `C:\Users\faruk\Documents\Codex_sync` → `C--Users-faruk-Documents-Codex-sync`
  - `C:\Users\faruk\Desktop\Java documentation` → `C--Users-faruk-Desktop-Java-documentation`
  - `C:\Users\faruk\AppData\Local\Temp\cs.Enc Test (v2)@home_x-y`
    → `C--Users-faruk-AppData-Local-Temp-cs-Enc-Test--v2--home-x-y`
- **The encoding is lossy / not reversible** — a `-` in the folder name could have come from `\`,
  `/`, `:`, space, `.`, etc. So the folder name alone cannot reconstruct the real path; the
  per-line **`cwd` field is the source of truth** for the actual project path. (macOS/Linux POSIX
  roots like `/home/user/proj` should follow the same rule → `-home-user-proj`, but that should be
  confirmed on a real non-Windows install.)

This is directly analogous to Codex's `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl`,
except the **project association is the directory name** rather than a date layout.

### Transcript schema

Each line is a JSON object. Observed top-level `type` values:

`user`, `assistant`, `system`, `progress`, `queue-operation`, `attachment`, `last-prompt`, `mode`.

Content lines (`user`/`assistant`) carry rich metadata, e.g.:

```
parentUuid, isSidechain, type, uuid, timestamp, sessionId, cwd, version, gitBranch,
userType, entrypoint, message{role, content, ...}, requestId
```

Key points:

- **`parentUuid` links the messages into a tree/chain** — a transcript is self-contained, so
  copying the whole file preserves the conversation. No cross-file references for the main thread.
- **`cwd` is recorded on *every* content line** (e.g. `C:\Users\faruk\Documents\Codex_sync`), in
  addition to being encoded in the folder name. So the project path is stored **twice**.
- **`version`** is the Claude Code version (e.g. `2.1.78`). Free per-line drift detection.
- **`entrypoint`** records the launching front-end (CLI / desktop app / IDE) — same store for all.
- **`isSidechain`** flags sub-agent sidechain content (see risks).

### Discovery contract — **empirically verified** (no model call needed)

The most important unknown was whether Claude Code makes a session resumable **purely from the
transcript file existing in the right `projects/<encoded-cwd>/` folder** (the "drop the file in,
the app reconciles" contract codex-sync depends on). It does. Verified on v2.1.170:

- **No session registry.** `~/.claude.json` has a `projects` map, but its entries are only config
  (allowed tools, MCP servers, trust-dialog/onboarding flags) — **not** a session index. The JSONL
  files are the durable record, like Codex's JSONL vs its SQLite index.
- **Error-differential probe** (nested `claude --resume <id> -p …`; the child process is not
  logged in, so it never reaches the API — but session *lookup* happens first):
  - resume an **existing** transcript in the cwd's folder → `Not logged in · Please run /login`
    (i.e. the session was **found**; it then hit the auth gate).
  - resume a **non-existent** id → `No conversation found with session ID: …` (not found).
- **Relocate test (the codex-sync scenario):** copy a transcript into a **different** project
  path's encoded folder and resume from that cwd → **found** (`Not logged in`). Negative control —
  resume the same id from a third cwd whose folder lacks the file → **not found**.

- **No trust/registry entry needed (verified).** The throwaway project paths used in these tests
  were **never** added to `~/.claude.json`'s `projects` map (0 entries), yet resume still *found*
  the session. So session discovery is independent of the `~/.claude.json` registry — an import
  need never touch it (the analog of Codex's "never touch SQLite"). The interactive trust dialog,
  if any, is a separate permissions concern for *running tools*, not a gate on session visibility.

**Conclusion:** session discovery is filesystem-based and scoped to the current cwd's encoded
folder. Dropping a transcript into the destination's `projects/<encoded-cwd>/` makes it
resumable, with no database/registry to update. This is the same scan-and-reconcile contract that
makes codex-sync possible.

---

## 2. What maps cleanly from codex-sync, and what is harder

**Same / reusable:** the whole export → move → import core, the safety model (verify checksums
before writing, never overwrite silently, atomic writes), the bundle/manifest/checksums shape, and
defensive JSONL parsing.

**Harder than Codex:**

1. **Project path is in the directory name** (not only a field). The cross-machine path-mapping
   problem (`--map-cwd`) therefore means **re-encoding/renaming the folder**, and `cwd` also
   appears on *every* line, so a faithful remap rewrites many lines (vs Codex's single
   `session_meta` line). A minimal remap could rename the folder only and leave the per-line `cwd`
   stale — needs testing whether Claude cares.
2. **Richer, faster-moving schema.** More line types, and versions drift quickly (2.1.78 and
   2.1.170 seen on one machine). Parsing must be defensive and version-aware; the per-line
   `version` field should be surfaced for drift warnings.
3. **Closed-source** — no spec to read; behavior must be re-verified against the installed version
   empirically (as done here).

---

## 3. Open questions

**Resolved since first draft:**

- ~~Folder-name encoding rule~~ — **resolved** (§1): `[^A-Za-z0-9_-] → -` per character; lossy.
- ~~Trust / project entry~~ — **resolved** (§1): discovery needs **no** `~/.claude.json` entry.

**Still open (verify before shipping):**

- **Sub-agent sidechains (the main one).** Does a logical session span multiple files (a main
  transcript plus separate sidechain/sub-agent transcripts that must travel together to resume
  cleanly), or are sidechains inlined in the one file with `isSidechain: true`? **Could not be
  determined here** — all 5 sessions on this machine are single-file, single-`sessionId`,
  `isSidechain: false`, and none used a `Task`/sub-agent tool. Needs a logged-in session that
  spawns a sub-agent. Until then, treat a session as possibly-multi-file and design export to
  detect/gather related transcripts defensively.
- **Attachments.** Are images/attachments inlined (base64) or referenced as separate files that
  must be bundled too? (`attachment` line type exists; one transcript had 79 of them.)
- **Compression.** Does Claude Code ever compress old transcripts (Codex has `.jsonl.zst`)? None
  seen so far.
- **macOS/Linux paths.** Confirm the same `~/.claude/projects/<encoded-cwd>/` layout and that the
  `[^A-Za-z0-9_-]→-` rule yields `-home-user-proj` for `/home/user/proj` (the cross-machine case
  usually mixes OSes).

---

## 4. Proposed scope for the build

A "codex-sync for Claude Code" mirrors the Codex tool:

- **Export** the `<uuid>.jsonl` transcript(s) for a project (its encoded folder) into a
  `.codexbundle`-style bundle with a manifest and checksums.
- **Import** them into the destination's `~/.claude/projects/<encoded-cwd>/`, never overwriting,
  checksums verified first, atomic writes — the existing safety model.
- **Path mapping** (the `--map-cwd` analog): re-encode the destination folder name for the local
  project path, and optionally rewrite the per-line `cwd`.
- **Never touch** the cloud, the account, `~/.claude.json`, or the desktop app's chat storage.

**Architecture decision still open:** ship as a **separate tool**, or extend codex-sync with a
`--tool claude|codex` (auto-detected) target. The core (bundle/safety/manifest) is reusable either
way; the per-tool parts are the home location, the path↔folder addressing, and the schema-specific
remap. Leaning toward one binary with a tool target, since the safe export/import machinery is
identical.

> Reminder: this is **unofficial** and Claude Code's format can change without notice. Any
> implementation must parse defensively, surface the recorded `version`, and be easy to re-verify
> against a fresh install.
