# Milestone 0 — Codex Source-Code Investigation

> Status: **Complete**. This document records the verified findings from inspecting the
> open-source [`openai/codex`](https://github.com/openai/codex) repository and the resulting
> technical direction for `cct` v0.1.
>
> Update: v0.1.1 later added the opt-in `--map-cwd` import flag. That feature is intentionally
> narrow: it rewrites only the canonical `cwd` field inside plain `.jsonl` `session_meta` lines
> and still never touches SQLite.

`cct` is an **unofficial** tool. Codex internals may change at any time; everything below
reflects the source as inspected and should be re-verified when Codex changes its storage format.

---

## 1. Verified Codex source-code findings

Files inspected (under `codex-rs/`):

- `rollout/src/lib.rs`, `rollout/src/list.rs`, `rollout/src/recorder.rs`
- `thread-store/src/local/mod.rs`, `thread-store/src/local/list_threads.rs`
- `app-server-protocol/src/protocol/v2/thread.rs`
- `app-server/src/message_processor.rs`, `app-server/src/request_processors/thread_processor.rs`
- `README.md`

### Storage layout

- Active sessions live at:
  `~/.codex/sessions/YYYY/MM/DD/rollout-YYYY-MM-DDThh-mm-ss-<uuid>.jsonl`
- Files may be compressed: a `.jsonl.zst` (zstd) variant exists alongside plain `.jsonl`.
- Archived sessions live under a separate directory: `~/.codex/archived_sessions/`.
- Constants in `rollout/src/lib.rs`: `SESSIONS_SUBDIR = "sessions"`,
  `ARCHIVED_SESSIONS_SUBDIR = "archived_sessions"`.
- The rollout filename encodes the `created_at` timestamp (second precision) and the thread UUID.
  `updated_at` is derived from the file modification time, not the filename.

### Two-tier storage model

From `thread-store/src/local/mod.rs` (paraphrasing the source documentation):

> "Rollout JSONL files are the durable replay format and remain readable without SQLite…
> The SQLite state DB, when available, is the queryable metadata index used by list/read
> paths for fast lookup."

- **JSONL rollout files are the canonical, durable source of truth.**
- **SQLite (`state_db`) is only an index/cache. It is NOT required for a session to be visible.**
- A source test confirms the store functions through JSONL replay when the database is absent.

### What makes a session discoverable (`rollout/src/list.rs`)

A session is returned by the list/scan path only if:

1. The file contains a `SessionMeta` line (`saw_session_meta == true`).
2. A non-empty `preview` exists (derived from the first user message / image / goal update).
3. It passes the active filters: `source` is in the allowed set, `model_provider` matches, and
   `cwd` matches if a cwd filter is applied.

Metadata extracted during a scan includes: `thread_id`, `cwd`, `source`, `model_provider`,
`cli_version`, git info (`git_branch`, `git_sha`, `git_origin_url`), `created_at`, the first user
message, and a preview.

### List path: SQLite vs JSONL (`thread-store/src/local/list_threads.rs`)

- The default mode (`use_state_db_only = false`) **scans the JSONL rollout files**, then enriches
  thread titles from SQLite, with a legacy file-scan fallback when SQLite is incomplete or stale.
- `use_state_db_only = true` skips the JSONL scan and reads only from SQLite.
- Net effect: a freshly **copied-in JSONL file appears on the next default scan** with no SQLite
  manipulation required. Codex reconciles the index itself.

### Writing rollouts (`rollout/src/recorder.rs`)

- `RolloutRecorderParams` has `Create` (new session, deferred file creation) and `Resume`
  (opens an existing rollout file for append) variants.
- **Writing a rollout file does not directly touch SQLite.** Reconcile/read-repair into `state_db`
  happens later, at list/read time (`state_db::reconcile_rollout()`, `read_repair_rollout_path()`).

### App-server thread API (`app-server-protocol/.../v2/thread.rs`, `message_processor.rs`)

- The Codex App/sidebar speaks the v2 thread API (`ThreadList`, `ThreadResume`, `ThreadRead`,
  `ThreadSearch`, `ThreadStart`, `ThreadFork`), all dispatched to `thread_processor`.
- `ThreadListParams` filters: `cursor`, `limit`, `sort_key`, `sort_direction`, `model_providers`,
  `source_kinds`, `archived`, `cwd`, `use_state_db_only`, `search_term`.
- `ThreadResumeParams` has a `history: Option<Vec<ResponseItem>>` field — but it is explicitly
  marked **`[UNSTABLE] FOR CODEX CLOUD - DO NOT USE`**.
- `ThreadStartParams` and `ThreadForkParams` do **not** accept history.
- There is **no public/supported "import a thread" endpoint.**

---

## 2. Final recommendation: Option A/C — file-based JSONL (incl. `.jsonl.zst`) export/import

Export and import operate primarily on the JSONL rollout files:

- **Export:** discover relevant rollout files (both `.jsonl` and `.jsonl.zst`), record their
  metadata, and package them in a `.codexbundle` ZIP preserving the `YYYY/MM/DD` layout.
- **Import:** copy rollout files back into `~/.codex/sessions/YYYY/MM/DD/`, never overwriting,
  never touching SQLite, then let Codex's native scan-and-reconcile rebuild the index on next run.
- **Optional v0.1.1 cwd mapping:** when explicitly requested with `--map-cwd`, rewrite only the
  canonical `cwd` field in a matching plain `.jsonl` `session_meta` line before writing. This is
  not a general JSONL/path rewrite and does not apply to compressed `.jsonl.zst` sessions.

Options A (raw JSONL copy) and C (copy + rely on scan-repair) **converge**: copying the JSONL files
*is* the trigger for Codex's own reconcile. We do nothing extra and call no private API.

| Criterion              | A/C (chosen) | B: App-server protocol            |
| ---------------------- | ------------ | --------------------------------- |
| Feasibility            | High         | Low (no import API; history Cloud-only) |
| Safety                 | High         | Low                               |
| Sidebar compatibility  | Full         | Uncertain                         |
| Complexity             | Low          | High (run app-server, JSON-RPC)   |
| Risk of breaking sessions | Very low  | High                              |
| MVP fit                | Excellent    | Poor                              |

---

## 3. Why we must NOT touch SQLite

- SQLite (`state_db`) is an index/cache, **not** the source of truth, and Codex rebuilds it from
  JSONL on the default scan path.
- Writing it directly couples us to a private, versioned schema that can change without notice and
  would diverge from Codex's own reconcile logic.
- If SQLite references a thread whose rollout file is missing, read/resume breaks — so we also never
  delete rollout files. Treating JSONL as canonical and leaving SQLite to Codex is the only safe,
  future-proof contract.

## 4. Why app-server import is not suitable for v0.1

- There is no public/supported endpoint to import an external thread.
- The only history-accepting field (`ThreadResumeParams.history`) is explicitly marked
  unstable and Codex-Cloud-only ("DO NOT USE"). `ThreadStart`/`ThreadFork` do not accept history.
- Using it would require running and speaking JSON-RPC to a Codex app-server process — far more
  complexity and breakage risk than copying files, with no upside for portability.

## 5. Risks and compatibility concerns

- **`.jsonl.zst` compression:** rollout files may be zstd-compressed. v0.1.x copies compressed
  sessions byte-for-byte but does not parse, decompress, or rewrite them.
- **cwd-based project filtering (the #1 portability gotcha):** the per-project sidebar view filters
  by `cwd`. If the project path differs between devices (e.g. `~/dev/x` vs `~/projects/x`), an
  imported session may be hidden from the project view even though it imported correctly. v0.1.1
  adds an explicit `--map-cwd OLD=NEW` escape hatch for plain `.jsonl` sessions. It rewrites only
  the canonical `cwd` field inside `session_meta`, validates the result, and still never touches
  SQLite.
- **Do not do global path replacement:** prompts, messages, tool output, and other JSONL lines may
  contain paths as normal content. Those must remain unchanged.
- **`source` / `model_provider` filters:** sessions can be filtered out if their `source` or
  provider is not in the allowed set. We preserve original metadata; we never normalize it.
- **JSONL schema drift:** the Codex format can change. Parsing must be defensive — line-by-line,
  tolerant of unknown fields and invalid lines, never crashing the whole scan over one bad file.

---

## 6. v0.1.x decisions

- **Go CLI** — single binary, fast, GitHub-release friendly; reusable core library + thin CLI.
- **Codex only** — no Claude support yet.
- **No hosting** — no accounts, no cloud, no server, no subscriptions, no background sync.
- **No SQLite writes** — never modify Codex's `state_db`.
- **No default JSONL rewriting** — normal import is byte-for-byte. The only mutation path is the
  explicit v0.1.1 `--map-cwd` option, which changes only `session_meta.payload.cwd` for matching
  plain `.jsonl` files.
- **No global path rewriting** — never replace path strings throughout prompts, messages, tool
  output, or other session content.
- **CLI first** — the desktop GUI shipped later as `cct app` (a loopback-only local web UI
  over the same Go core), rather than a native Wails app, to keep the CGO-free single-binary model.
- **Safe export/import only** — never overwrite existing session files silently; conflicts are
  reported and skipped by default.
