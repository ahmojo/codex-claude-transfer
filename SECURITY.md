# Security Policy

`cct` moves local AI-agent session files between machines. Those files can
contain prompts, source code, command output, file paths, and accidentally
printed secrets, so security reports get priority over everything else.

## Supported versions

| Version | Supported |
| --- | --- |
| Latest 1.x release | Yes |
| Older releases | No — please update to the latest release first |

The bundle format (`codex-sync-bundle-v1`) is stable across all 1.x releases,
so updating is safe: newer versions read bundles from older ones.

## Reporting a vulnerability

**Please do not open a public issue for an exploitable problem.**

Use GitHub's private vulnerability reporting instead:

1. Go to
   [Security → Report a vulnerability](https://github.com/ahmojo/codex-claude-transfer/security/advisories/new).
2. Describe the issue, affected version, and reproduction steps.
3. Reproduce with **synthetic session data only** (see below).

### Response times

This is a single-maintainer project; the targets are best effort:

- **Acknowledgement:** within 7 days.
- **Initial assessment:** within 14 days.
- **Fix or coordinated disclosure:** within 90 days. Confirmed issues that leak
  or corrupt session data are treated as release blockers.

## Never attach real session data

Never attach a real `.codexbundle`, a real session `.jsonl`, or anything from
your actual `~/.codex` / `~/.claude` to an issue or report. These files can
contain:

- prompts and full conversation text,
- source code and command output,
- absolute paths, usernames, and machine details,
- API keys, tokens, and other secrets that were ever printed in a session.

Reproduce with throwaway data instead: the test suite shows how to build fake
Codex/Claude homes in a temp directory, and `cct` itself never requires a real
home (`--codex-home` / `CODEX_HOME` point it anywhere).

## What is in scope

- Anything that writes outside the target agent home (path traversal, zip-slip,
  symlink tricks) — the importer is designed to reject all of these.
- Checksum or verification bypasses.
- Data leaving the machine unexpectedly. Apart from the opt-in `--clone` fetch
  and the experimental, explicitly gated LAN `cct sync`, `cct` must never touch
  the network.
- Secret-gate bypasses (`export`/`sync` refusing bundles with likely secrets).
- Weaknesses in the LAN sync pairing/transport (experimental, but still in
  scope).

Out of scope: the agents' own on-disk formats changing (that is breakage, not a
vulnerability — open a regular issue), and the inherent sensitivity of bundle
contents, which is documented behavior.

## More detail

The full threat and write-safety model is documented in
[docs/safety.md](docs/safety.md) and [docs/internals.md](docs/internals.md).
