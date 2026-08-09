# codex-claude-transfer

**Move local Codex and Claude Code sessions between machines.**
The command is **`cct`**.

![CI](https://github.com/ahmojo/codex-claude-transfer/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v1.8.0-blue)

> **Unofficial.** Not affiliated with or endorsed by OpenAI or Anthropic.
> These tools' internals can change at any time and break this tool. Use at your
> own risk. See the [Disclaimer](#disclaimer).

## The problem it solves

Codex and Claude Code keep valuable project context in local session files. That
is good for privacy, but painful when you switch machines, rebuild a laptop, or
want to continue a thread in the other agent.

`cct` turns that local context into a simple handoff:

```text
Machine A:  cct export --project .      ->  project.codexbundle
                        copy it however you trust
Machine B:  cct import ./project.codexbundle
```

No cloud account, hosted sync service, or background process by default. You move
one `.codexbundle` over a channel you trust, then import it after checksum
verification. The agents' indexes are left alone and re-scan the session files
themselves.

It works for Codex, Claude Code, and cross-agent handoff. The same flow is
available as a CLI, a terminal wizard (`cct ui`), and a local browser app
(`cct app`).

## Demo

Export on one machine, then import or incrementally sync onto the other:

![Overview: doctor, grouped list, export](demo/clips/01-overview.gif)

![Incremental sync with import --merge](demo/clips/02-sync.gif)

The optional local desktop UI uses the same engine:

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
[reading compressed `.jsonl.zst` sessions](demo/clips/09-compressed.gif).

All recordings use throwaway demo sessions, never a real `~/.codex` or
`~/.claude`.

## Install

```bash
# From source (Go 1.23+)
go install github.com/ahmojo/codex-claude-transfer/cmd/cct@latest
```

Or download a prebuilt binary from
[Releases](https://github.com/ahmojo/codex-claude-transfer/releases), or build
from a clone:

```bash
git clone https://github.com/ahmojo/codex-claude-transfer.git
cd codex-claude-transfer && go build -o cct ./cmd/cct
```

Package manifests for **Homebrew** and **Scoop** live in
[`packaging/`](packaging/).

## 30-second use

```bash
cct doctor
cct export --project .
# copy project.codexbundle to the other machine
cct import ./project.codexbundle --dry-run
cct import ./project.codexbundle
# Optional: ask Codex to discover and verify changed threads immediately.
cct import ./project.codexbundle --reconcile
```

Use `--tool claude` to export Claude Code sessions. Use `import --to claude` or
`import --to codex` to translate a session into the other agent. After importing,
restart the agent so it re-scans the files. For a native Codex bundle, opt-in
`--reconcile` can instead ask Codex's own app-server to read and verify the
changed thread IDs immediately; failure leaves the imported rollout files intact
and prints restart guidance plus an exact `cct resume <thread-id> --run`
fallback only when the rollout ID is a valid UUID and the command can be
rendered safely. The terminal wizard (`cct ui`) and browser app (`cct app`)
expose the same opt-in native Codex reconciliation flow.

### Relocate a project

`cct relocate` rewrites the recorded working directory (`cwd`) in every matching
session, so the sessions remain grouped with the project at its new path:

```bash
# The project was already copied or moved; NEW exists.
cct relocate /old/project /new/project --dry-run
cct relocate /old/project /new/project

# Move the project too; NEW must not exist yet.
cct relocate /old/project /new/project --move-project

# Claude Code: transcripts also move into the folder encoding the new path.
cct relocate /old/project /new/project --tool claude
```

The command preserves session backups and never modifies Codex's SQLite database
or `~/.claude.json`. Add `--include-archived` to relocate archived Codex sessions
too. For Claude Code, each transcript is written under the new project folder
first and its original removed only afterward, so a session id is never
duplicated; `cct undo` reverses both halves. See the
[usage guide](docs/usage.md#relocate-a-project) for rollback behavior and
same-filesystem moves.

### Let your agent do it: sessions through git

`cct skill install` writes a skill into your Claude Code home that teaches the
agent one workflow: save this project's sessions into git when you stop, restore
them after a clone on the other machine.

![Install the skill, point the project at a private session store, restore it on a second machine](demo/clips/16-skill.gif)

```bash
cct skill install                       # ~/.claude/skills/cct-session-sync/
cct skill print --plain >> ~/.codex/AGENTS.md   # the same for Codex
```

The recommended layout keeps chat history **out** of the code repo: one private
session-store repo holds every project's bundles, and each project commits only
a small reference file pointing at it.

```bash
cct config set repo-sync-repo git@github.com:you/cct-sessions.git
cct skill init     # writes .cct/sessions.json + .cct/README.md — commit them
cct skill show     # where the history lives and the exact commands
```

```text
~/cct-sessions/projects/my-app/
  claude/claude-all.codexbundle      # every session for this project
  claude/groups/auth-refactor.codexbundle   # optional: one topic per file
  codex/codex-all.codexbundle
```

Without any agent, it is still just two commands — save with `cct export -o <that
path>` and commit, restore with `cct import <that path> --merge --map-cwd-here`.
Keeping the bundle in the project's own repo under `.cct/` stays supported for
private repos.

A bundle is readable by everyone with repo access, forever, so the skill asks
once whether to commit it plainly (private repos only) or `age`-encrypted, and
stores the answer in `cct config`. It never pushes or passes `--allow-secrets`
on its own, and treats a reference file it did not write as untrusted. See the
[usage guide](docs/usage.md#carry-sessions-through-git).

## Compatibility

Codex's and Claude Code's on-disk formats are their own internals and can change
at any time. This table records what each agent's support covers and the newest
agent version it was last verified against:

| Agent | Last tested | Supported data | Known gaps |
| --- | --- | --- | --- |
| **Codex CLI / app-server** | 0.144.6 (2026-07-23) | Sessions (`rollout-*.jsonl`, compressed `.jsonl.zst`), session metadata, git context, inline images; synthetic live-import `thread/read` reconciliation | SQLite/session_index are never written directly by cct; `--reconcile` is capability-probed because app-server is experimental; `.jsonl.zst` needs external `zstd` for metadata, `--map-cwd`, and merge |
| **Claude Code** | 2.1.212 (2026-07-18) | Conversations (`projects/<encoded-cwd>/*.jsonl`), tool events, project mapping, project relocation (`relocate --tool claude`, which also moves the project's `memory/`) | `~/.claude.json` config is never touched; auto memory moves with `relocate` but does not travel in a bundle yet; sidechains/subagent transcripts transfer as files but are not translated cross-agent; the project-folder encoding is lossy, so two project paths can share one folder (relocation rewrites those in place) |
| **Cross-agent handoff** (`import --to`) | same versions | Conversation text and project context, translated between the two formats | A translation, not a clone: tool calls, command output, runtime state, and provider-specific ids do not carry over byte-for-byte |

If a newer agent version breaks something, please
[open an issue](https://github.com/ahmojo/codex-claude-transfer/issues) — with
synthetic session data only, never a real bundle (see
[SECURITY.md](SECURITY.md)).

## Docs

- [Usage guide](docs/usage.md): quickstart, desktop app, common workflows, LAN
  sync, and optional external tools.
- [Command reference](docs/reference.md): commands and flags.
- [Internals](docs/internals.md): how bundles work, safety model, limitations,
  and versioning.
- [Safety notes](docs/safety.md): detailed privacy and write-safety model.
- [Roadmap](docs/roadmap.md): shipped features, never-planned features, and
  project notes.

## Contributing

PRs welcome. Keep changes small, preserve the no-cloud / no-SQLite-writes
principles, test with fake Codex homes only, and run the documented Go checks.
See [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Disclaimer

Unofficial and **not affiliated with or endorsed by OpenAI or Anthropic**. It
works against Codex's and Claude Code's local files based on their behavior at a
point in time, which may change and break it. `.codexbundle` files can contain
prompts, code, command output, paths, and secrets; treat them like sensitive local
history and encrypt them over untrusted channels. Provided "as is", without
warranty. **Use at your own risk.**
