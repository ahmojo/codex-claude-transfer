# Command reference

Use this page when you need exact commands and flags. For guided workflows, see
[Usage guide](usage.md).

## Commands

| Command | Description |
| ------- | ----------- |
| `cct app` | Launch the local browser UI. It binds to loopback only and uploads nothing. |
| `cct ui` | Interactive terminal wizard; builds and runs the commands below. |
| `cct doctor` | Read-only health check: agent home, session counts, missing cwd, and optional tool status. Use `--tool` to pick Codex or Claude Code. |
| `cct list` | List discovered sessions with preview, thread id, cwd, source, and updated time. |
| `cct search <query>` | Full-text search across session conversation text. Supports `--regex`, `--case-sensitive`, `--project`, `--since`, and `--json`. |
| `cct scan` | Check sessions for likely secrets before sharing or syncing. Read-only; values are masked. |
| `cct stats` | Summarize sessions: totals, busiest projects, and recent activity (`--json`). |
| `cct resume [query]` | Find the best matching session and print the agent command that continues it; `--run` launches it. |
| `cct browse` | Interactive session browser: search, pick one, then resume, export, tag, or name it. |
| `cct tag add\|rm\|ls` / `cct name` | Add cct-only tags and friendly names. These are stored in cct config, never in agent session files. |
| `cct config list\|get\|set\|path` | Save defaults such as tool, homes, and port. Explicit flags always win. |
| `cct export [--project <path> \| --all \| --session <id>]` | Package matching sessions into a `.codexbundle`, or render readable Markdown/HTML with `--format`. By default it refuses likely secrets unless `--redact` or `--allow-secrets` is set. |
| `cct inspect <bundle>` | Show a bundle's manifest and contents, read-only, and flag missing recorded project folders. |
| `cct diff <bundle>` | Preview what importing the bundle would do — new, would-grow (with line counts), already-present, and conflicting sessions — read-only, nothing is written. Honors the same selection/remap flags as `import`. |
| `cct import <bundle>` | Import session files into the matching agent home, or translate across agents with `--to`. Verifies checksums and never overwrites by default. Filter which sessions to import with `--session`, `--project`, `--since`, or `--match`; native Codex imports may opt into post-import discovery with `--reconcile`. |
| `cct relocate OLD NEW` | Rewrite Codex sessions after a project folder changes location. By default `NEW` must already exist; add `--move-project` to rename the folder too. Claude Code and `--claude-home` are tracked separately in [#13](https://github.com/ahmojo/codex-claude-transfer/issues/13). |
| `cct undo [--list] [--dry-run]` | Reverse the most recent import: delete the files it created and restore the backups it made. Only touches files that still match what the import wrote, so later edits are never lost. `--list` shows recent imports; `--dry-run` previews. |
| `cct repair-times` | Fix modification times for sessions imported by older versions. Supports `--dry-run`; changes mtimes only. |
| `cct sync serve` / `cct sync connect [host:port]` / `cct sync daemon` | Experimental LAN sync. Peer-to-peer, no server/cloud, paired by one-time code or remembered devices, requires `--i-understand`. |
| `cct version` | Print the version. Also supports `--version`. |
| `cct completion <bash\|zsh\|fish>` | Print a shell completion script. |
| `cct help` | Show help. |

## Flags

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--tool <codex\|claude>` | all | Which agent to act on. Default: auto-detect when possible. On import, the bundle's recorded tool wins. |
| `--codex-home <path>` | all | Use a specific Codex home instead of the default. Also honors `$CODEX_HOME`. |
| `--claude-home <path>` | Claude-capable commands except relocate | Use a specific Claude Code home instead of `~/.claude`. Also honors `$CLAUDE_HOME`. Claude relocation is tracked in [#13](https://github.com/ahmojo/codex-claude-transfer/issues/13). |
| `--project <path>` | export, import, diff | Export: filter sessions by recorded cwd. Import/diff: import only the sessions whose recorded cwd is `<path>` — pull one project out of a multi-project bundle. |
| `--all` | export | Export every session regardless of cwd. Mutually exclusive with `--project`. |
| `--session <id>` | export, import, diff | Export exactly one session by thread id prefix. Import/diff: act only on matching sessions. Repeatable on import/diff. |
| `--since <when>` | export, import, diff | Only sessions updated at or after a date (`YYYY-MM-DD`) or duration (`7d`, `48h`, `90m`). On import/diff it filters which of the bundle's sessions are considered. |
| `--with-git` | export | Record the project's git remote, branch, commit, and dirty/unpushed status. |
| `--git-push` | export | Opt-in. Push the current branch to its own remote first so the recorded commit is fetchable. Never force-pushes. |
| `--strip-images` | export | Replace inline base64 images with placeholders to shrink the bundle. Lossy; needs `zstd` for `.jsonl.zst`. |
| `--output`, `-o <path>` | export | Bundle output path. Defaults are derived from `--project`, `--all`, or `--session`. |
| `--include-archived` | list, export, relocate | Include archived sessions. |
| `--json` | doctor, list, inspect, export, import, relocate, diff, sync | Print machine-readable JSON instead of text. |
| `--dry-run` | import, relocate, undo, sync | Validate and report only; write nothing. |
| `--move-project` | relocate | Rename `OLD` to `NEW` before rewriting session cwd. Uses a same-filesystem rename only; `NEW` must not exist. |
| `--list` | undo | Show recent imports (newest first) instead of reversing one. |
| `--to <codex\|claude>` | import | Cross-agent handoff: translate bundle sessions into the other agent's format. |
| `--regex` / `--case-sensitive` | search, export, import, diff | Treat the query (`--match`) as regex / match case-sensitively. |
| `--match <query>` | export, import, diff | Keep only sessions whose conversation text matches the query. On import/diff it filters the bundle; compressed `.jsonl.zst` sessions are skipped. |
| `--format md\|html` | export | Render selected sessions as readable Markdown or self-contained HTML instead of a re-importable bundle. |
| `--redact` | export, sync | Replace likely secrets with placeholders. Lossy and opt-in. |
| `--allow-secrets` | export, sync | Proceed even though a likely secret was detected. |
| `--run` | resume | Launch the agent on the chosen session now. |
| `--remember` | sync | After code pairing, remember the peer so later syncs can skip the code. |
| `--interval <n>` / `--once` | sync daemon | Poll every `<n>` seconds (default 5), or run one discover-and-sync sweep and exit. |
| `--map-cwd OLD=NEW` | import, sync | Rewrite matching sessions' recorded cwd. Plain `.jsonl` always; `.jsonl.zst` when `zstd` is installed. Repeatable. |
| `--map-cwd-here` | import, sync | Map the bundle's project to the current directory. Single-project only; cannot combine with `--map-cwd`. |
| `--merge` | import | Incremental sync. If the local session is a prefix of the bundle's, append only the new messages. The comparison is serialization-tolerant: records that differ only in JSON key order or escaping (as after a cross-platform transfer) count as equal, and the local file's own serialization is preserved. |
| `--reconcile` | import | Codex-only, opt-in, and incompatible with `--dry-run` / `--to`. After changed rollouts are written, capability-probe Codex app-server, use native `thread/read` for missing IDs, and verify via `thread/list`. Failure does not undo the import and prints safe restart/resume guidance. cct never writes SQLite/session_index directly. |
| `--replace-with-backup` | import | On conflict, back up the local file and overwrite it with the bundle's version. |
| `--import-as-copy` | import | On conflict, import the bundle's version as a new session, leaving yours untouched. Excludes `--replace-with-backup`. |
| `--clone <dir>` | import | After importing, clone the bundle's recorded git remote into `<dir>` and check out its commit. |
| `--encrypt-to <recipient>` | export | Encrypt to an `age` recipient (`age1...` or `ssh-ed25519 ...`). Repeatable. Writes `<output>.age`. |
| `--recipients-file <file>` | export | Encrypt to every `age` recipient listed in `<file>`. |
| `--passphrase` | export, import, inspect | Export with a passphrase, or decrypt a passphrase-encrypted bundle. |
| `--identity <file>` | import, inspect | `age` identity/private key file used to decrypt a `.age` bundle. |
