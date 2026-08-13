# Command reference

Use this page when you need exact commands and flags. For guided workflows, see
[Usage guide](usage.md).

## Commands

| Command | Description |
| ------- | ----------- |
| `cct app` | Launch the local browser UI. It binds to loopback only and uploads nothing. |
| `cct ui` | Interactive terminal wizard; builds and runs the commands below. |
| `cct doctor` | Read-only health check: agent home, session counts, missing cwd, and optional tool status. Use `--tool` to pick Codex or Claude Code. For Claude Code it also reads `cleanupPeriodDays` from `settings.json` and warns which transcripts Claude's own cleanup is about to delete. |
| `cct list` | List discovered sessions with preview, thread id, cwd, source, and updated time. |
| `cct search <query>` | Full-text search across session conversation text. Supports `--regex`, `--case-sensitive`, `--project`, `--since`, and `--json`. |
| `cct scan` | Check sessions for likely secrets before sharing or syncing. Read-only; values are masked. |
| `cct stats` | Summarize sessions: totals, busiest projects, and recent activity (`--json`). |
| `cct resume [query]` | Find the best matching session and print the agent command that continues it; `--run` launches it. |
| `cct browse` | Interactive session browser: search, pick one, then resume, export, tag, or name it. |
| `cct tag add\|rm\|ls` / `cct name` | Add cct-only tags and friendly names. These are stored in cct config, never in agent session files. |
| `cct config list\|get\|set\|path` | Save defaults such as tool, homes, port, and the `repo-sync` mode. Explicit flags always win. |
| `cct skill install\|print\|path` | Install the `cct-session-sync` skill into your Claude Code home (`~/.claude/skills/`), so an agent knows how to keep a project's sessions in git. `print --plain` emits the same instructions without frontmatter for Codex's `AGENTS.md`. Writes one file; no session file or agent index is touched. |
| `cct skill init` | Write `.cct/sessions.json` and `.cct/README.md` into a project (`--project <dir>`, default `.`), pointing it at the private session store from `repo-sync-repo` or `--repo`. Commit them. Repointing an existing reference needs `--force`. |
| `cct skill show` | Read that reference and explain the store: URL, the project's folder, encryption, where it is cloned on this machine, and each tool's bundle path. `--json` for the machine-readable form. Refuses a reference with a remote-helper transport (`ext::`) or control characters. |
| `cct export [--project <path> \| --all \| --session <id>]` | Package matching sessions into a `.codexbundle`, or render readable Markdown/HTML with `--format`. By default it refuses likely secrets unless `--redact` or `--allow-secrets` is set. |
| `cct inspect <bundle>` | Show a bundle's manifest and contents, read-only, and flag missing recorded project folders. |
| `cct diff <bundle>` | Preview what importing the bundle would do — new, would-grow (with line counts), already-present, and conflicting sessions — read-only, nothing is written. Honors the same selection/remap flags as `import`. |
| `cct import <bundle>` | Import session files into the matching agent home, or translate across agents with `--to`. Verifies checksums and never overwrites by default. Filter which sessions to import with `--session`, `--project`, `--since`, or `--match`; native Codex imports may opt into post-import discovery with `--reconcile`. |
| `cct relocate OLD NEW` | Rewrite a project's recorded sessions after its folder changes location. By default `NEW` must already exist; add `--move-project` to rename the folder too. With `--tool claude` each transcript also moves into the folder encoding `NEW` — along with the project's auto memory (`projects/<encoded-cwd>/memory/`) — and an original is removed only after its copy is written. A memory file that already exists at the destination with different content stops the relocation. |
| `cct undo [--list] [--dry-run]` | Reverse the most recent import or relocation: delete the files it created, restore the backups it made, and put back transcripts a relocation removed. Only touches files that still match what was written, so later edits are never lost. `--list` shows recent imports; `--dry-run` previews. |
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
| `--claude-home <path>` | Claude-capable commands | Use a specific Claude Code home instead of `~/.claude`. Also honors `$CLAUDE_HOME`. On `relocate` it requires `--tool claude`. |
| `--project <path>` | export, import, diff | Export: filter sessions by recorded cwd. Import/diff: import only the sessions whose recorded cwd is `<path>` — pull one project out of a multi-project bundle. |
| `--all` | export | Export every session regardless of cwd. Mutually exclusive with `--project`. |
| `--session <id>` | export, import, diff | Export exactly one session by thread id prefix. Import/diff: act only on matching sessions. Repeatable on import/diff. |
| `--since <when>` | export, import, diff | Only sessions updated at or after a date (`YYYY-MM-DD`) or duration (`7d`, `48h`, `90m`). On import/diff it filters which of the bundle's sessions are considered. |
| `--with-git` | export | Record the project's git remote, branch, commit, and dirty/unpushed status. |
| `--with-memory` | export, import | Claude Code only. Also carry the selected projects' auto memory (`projects/<encoded-cwd>/memory/`). Opt-in on **both** sides: an export without it puts no memory in the bundle, an import without it skips what a bundle carries. Import writes memory under the project the cwd mapping resolves to, and never overwrites a file that differs. |
| `--git-push` | export | Opt-in. Push the current branch to its own remote first so the recorded commit is fetchable. Never force-pushes. |
| `--strip-images` | export | Replace inline base64 images with placeholders to shrink the bundle. Lossy; needs `zstd` for `.jsonl.zst`. |
| `--output`, `-o <path>` | export | Bundle output path. Defaults are derived from `--project`, `--all`, or `--session`. |
| `--include-archived` | list, export, relocate | Include archived sessions. Codex only: Claude Code keeps no separate archive location, so `relocate --tool claude` refuses the flag. |
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
| `--force` | skill install | Replace an installed `SKILL.md` whose contents differ from this cct's. The current file is kept as a `.cct-bak-<nanos>` copy next to it. |
| `--plain` | skill print | Drop the YAML frontmatter, so the instructions can be pasted into `AGENTS.md` or another agent's instruction file. |
| `--repo <git-url>` | skill init | The session store's git remote for this project, instead of the saved `repo-sync-repo`. Remote-helper transports (`ext::`, `fd::`) and flag-like values are refused. |

## Config keys

`cct config set <key> <value>` saves a default; an explicit flag always wins. The
file is plain JSON under cct's config dir (`cct config path`) and holds no
secrets.

| Key | Values | Meaning |
| --- | ------ | ------- |
| `tool` | `codex`, `claude` | Which agent commands act on by default. |
| `codex-home` / `claude-home` | path | Default agent home, below `$CODEX_HOME` / `$CLAUDE_HOME` in precedence. |
| `port` | 0-65535 | Default port for `cct app`. |
| `repo-sync` | `plain`, `encrypted` | How the `cct-session-sync` skill commits a bundle into a project's repo: as-is, or `age`-encrypted. Answered once, during that skill's setup. |
| `repo-sync-recipient` | `age1...`, `ssh-...` | The `age` recipient that workflow encrypts to. A recipient is a public key; a private key is refused. |
| `repo-sync-repo` | git URL | A separate, private session-store repo that holds bundles for all projects. Set it and projects keep only a reference file; leave it empty and each project's bundle stays in its own repo under `.cct/`. |
| `repo-sync-dir` | path | Where that store is cloned on this machine (default `~/cct-sessions`). Per-machine on purpose: it is never written into a project. |
