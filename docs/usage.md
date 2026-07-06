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
```

For Claude Code, add `--tool claude` to commands that discover or export
sessions. Import reads the agent from the bundle:

```bash
cct list --tool claude
cct export --tool claude --project .
cct import ./project.codexbundle
```

After importing, run the agent again so it re-scans the files.

## Optional external tools

The core commands need nothing extra. A few opt-in features shell out to a
standard tool if you use them; without it, that feature errors with guidance or
is skipped.

| Tool | Enables | Without it |
| ---- | ------- | ---------- |
| [`git`](https://git-scm.com/) | `export --with-git`, `import --clone` | git metadata not recorded; `--clone` errors |
| [`age`](https://github.com/FiloSottile/age) | bundle encryption / decryption | encrypt/decrypt errors; plain bundles unaffected |
| [`zstd`](https://github.com/facebook/zstd) | reading compressed `.jsonl.zst` metadata; `--map-cwd` on compressed sessions | compressed sessions are copied as-is, with cwd/preview unknown |

These tools are only used locally. They do not change the "nothing is uploaded"
guarantee.

## Desktop app

If you would rather click than type, `cct app` gives you a small graphical
interface with Doctor, Sessions, Export, Inspect, Import, Search, Stats, and Scan
views. It is feature-parity with the CLI for the core workflows: project export,
single-session export, `--since`, git metadata, image stripping, recipient-based
encryption, import preview, incremental merge, conflict handling, cwd remap,
selective import, cross-agent handoff, and git clone.

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

### Project path changed

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

### Export one session or a subset

Use a thread-id prefix to export or import only selected sessions:

```bash
cct export --session <id>
cct import ./project.codexbundle --session <id>
```

`--session` is repeatable on import.

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
