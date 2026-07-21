# codex-claude-transfer

**Move local Codex and Claude Code sessions between machines.**
The command is **`cct`**.

![CI](https://github.com/ahmojo/codex-claude-transfer/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v1.3.0-blue)

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
```

Use `--tool claude` to export Claude Code sessions. Use `import --to claude` or
`import --to codex` to translate a session into the other agent. After importing,
restart the agent so it re-scans the files.

## Compatibility

Codex's and Claude Code's on-disk formats are their own internals and can change
at any time. This table records what each agent's support covers and the newest
agent version it was last verified against:

| Agent | Last tested | Supported data | Known gaps |
| --- | --- | --- | --- |
| **Codex CLI** | 0.144.0 (2026-07-18) | Sessions (`rollout-*.jsonl`, compressed `.jsonl.zst`), session metadata, git context, inline images | SQLite index is never copied (Codex rebuilds it by re-scanning); `.jsonl.zst` needs the external `zstd` tool for metadata, `--map-cwd`, and merge |
| **Claude Code** | 2.1.212 (2026-07-18) | Conversations (`projects/<encoded-cwd>/*.jsonl`), tool events, project mapping | `~/.claude.json` config is never touched; sidechains/subagent transcripts transfer as files but are not translated cross-agent |
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
