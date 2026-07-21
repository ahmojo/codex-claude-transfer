# Internals

This page explains the technical model behind `cct`. For everyday commands, see
[Usage guide](usage.md).

## How it works

Each agent stores a session as a durable JSONL file, with a rebuildable index
alongside it that `cct` never writes:

- **Codex** stores sessions under
  `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`. Its SQLite database is the
  index.
- **Claude Code** stores sessions under
  `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl`. Its `~/.claude.json` holds
  config, not a session index.

`cct` works only with those JSONL files. `export` packages them with a manifest
and SHA-256 checksums into a `.codexbundle` ZIP. `import` verifies checksums and
copies session files back into place. The agent then re-discovers the files on
its next run.

The bundle records which agent it came from, so import writes to the matching
home unless you explicitly request a cross-agent handoff with `--to`.

## The `.codexbundle` extension

Every bundle uses the `.codexbundle` extension, including Claude Code exports.
The name is historical: the tool began as a Codex-only utility. The extension
does not mean the bundle holds Codex sessions.

The manifest records the real source in its `tool` field (`codex` or `claude`),
and `inspect` / `import` read that field rather than trusting the file name. If
you prefer another name, pass `-o my-project.claudebundle`; the extension is
cosmetic.

## Bundle format

A `.codexbundle` is a ZIP archive:

```text
project.codexbundle
|-- manifest.json     # format version, source info, per-session metadata
|-- checksums.json    # SHA-256 of every other file (not itself)
`-- sessions/YYYY/MM/DD/rollout-...-<uuid>.jsonl[.zst]
```

Format version: `codex-sync-bundle-v1`.

Compressed `.jsonl.zst` rollouts are copied byte-for-byte and never recompressed
or modified. Their metadata can be read during export when `zstd` is installed.

## Safety model

The full model lives in [safety.md](safety.md). In short:

- Checksums are verified before any write.
- New files are written, identical files are skipped, and differing files are
  conflicts unless you opt into `--merge`, `--replace-with-backup`, or
  `--import-as-copy`.
- SQLite/index files are never modified.
- Path traversal, zip-slip, and absolute paths inside bundles are rejected.
- Writes are atomic: temp file plus rename.
- Default import is byte-for-byte. Content changes are opt-in and narrow:
  `--map-cwd` changes the `cwd` field, and `--import-as-copy` changes the `id`
  field.
- Every `cct import` records a small journal in cct's own config directory (never
  in the agent home). `cct undo` reverses the most recent import: it deletes the
  files that import created and restores the backups it made, but only for files
  that still hash to exactly what the import wrote — anything edited afterward is
  left untouched and reported as skipped.

A `.codexbundle` can contain prompts, code, command output, file paths, and
accidentally printed secrets. Treat it like shell history plus source context.

## Limitations

- **Codex internals may change.** Parsing is defensive, but the on-disk format
  can drift.
- **Claude Code's format is closed-source and moves fast.** Support is based on
  empirical behavior and may need updates after Claude Code changes.
- **Compressed `.jsonl.zst` sessions need `zstd`** to recover metadata and to be
  remapped with `--map-cwd`. Without it they are copied as-is and their cwd may
  be unknown to `--project`.
- **Project visibility depends on matching cwd paths.** If the project lives at
  a different path on each machine, use `--map-cwd` or `--map-cwd-here`.
- **No global path rewriting and no cloud sync.** `--map-cwd` only changes the
  recorded session cwd. Incremental sync only appends to a session that grew on
  one side; it never merges two independently diverged transcripts.
- **`--strip-images` is lossy and not merge-friendly.** It drops image bytes and
  leaves text, so a stripped bundle no longer matches an unstripped copy.
- **The desktop GUI runs in your browser**, served by `cct` on loopback only. It
  is not native packaging.
- **Cross-agent handoff is a translation, not a clone.** It carries conversation
  and project context, but tool calls, command output, runtime state, exact tool
  history, and provider-specific ids do not transfer byte-for-byte.

## Stability and versioning

As of **v1.0.0**, `cct` follows [semantic versioning](https://semver.org/):

- **The bundle format (`codex-sync-bundle-v1`) is stable.** A bundle exported by
  any 1.x version imports into any other 1.x version. A breaking format change
  would require a new format version and a major release.
- **The command-line interface is stable.** Existing commands, flags, and their
  meanings will not change incompatibly within 1.x; new ones may be added.
- **`cct sync` remains experimental** and `--i-understand` gated. Its wire
  protocol may still change between minor releases until it graduates.
- What `cct` reads is the agents' own on-disk formats, which are outside this
  project's control and can change at any time.

## Verifying a release

Releases newer than v1.1.1 ship with provenance material next to the binaries:

- `SHA256SUMS.txt` — SHA-256 checksums of every archive and the SBOM,
- `SHA256SUMS.txt.sigstore.json` — a keyless [Sigstore](https://www.sigstore.dev/)
  signature over the checksum file, made by the release workflow's OIDC
  identity (no long-lived private key exists),
- `cct_<tag>_sbom.spdx.json` — an SPDX SBOM of the source module,
- GitHub [artifact attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations)
  binding each archive to the exact workflow run that built it.

To verify a download:

```bash
# 1. Checksums
sha256sum -c SHA256SUMS.txt --ignore-missing

# 2. Signature on the checksum file (requires cosign)
cosign verify-blob \
  --bundle SHA256SUMS.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/ahmojo/codex-claude-transfer/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS.txt

# 3. Build provenance (requires the GitHub CLI)
gh attestation verify cct_<tag>_linux_amd64.tar.gz \
  --repo ahmojo/codex-claude-transfer
```

Step 1 alone proves integrity; steps 2 and 3 additionally prove the assets were
built by this repository's release workflow on GitHub Actions, not on someone's
machine.

## Claude Code research

Claude Code support was verified empirically against a live install. The storage
format and file-based resume contract notes are in
[docs/research/claude-code-sessions-investigation.md](research/claude-code-sessions-investigation.md).
