# Safety & Privacy

`codex-sync` is designed to be **safe by default** and to never silently destroy
your local Codex sessions. This document explains the safety model in detail and,
just as importantly, the **privacy risks of `.codexbundle` files**.

Read the privacy section before you share a bundle with anyone.

---

## 1. `.codexbundle` files may contain sensitive data

A `.codexbundle` is a packaged copy of your real Codex **rollout files**. Those
files are a full transcript of a session, so a bundle can contain:

- **Your prompts** — everything you typed to Codex.
- **Model output** — including code, explanations, and suggested commands.
- **Source code** — snippets and files that were read into or written during the session.
- **Terminal output** — command results that Codex captured.
- **Absolute filesystem paths** — revealing your username, directory layout, and project names.
- **Git metadata** — branch names, commit SHAs, and remote URLs.
- **Secrets that were accidentally printed** — API keys, tokens, passwords, or
  `.env` contents that happened to appear in a prompt, a file, or command output
  during the session. Codex records what it sees; it does not scrub secrets.
- **Uploaded images and attachments** — anything you dropped into a session.

> **Treat a `.codexbundle` like you would treat your shell history plus your
> source tree.** It is at least as sensitive as both.

### A note on images and attachments

When you drop an image (or other attachment) into a Codex session, it is stored
**inline in the rollout JSONL as base64**, not as a separate file reference.
That has two consequences for bundles:

- **They travel with the bundle.** Because the image bytes live inside the
  rollout file, exporting a session also exports every image in it. There is no
  way to include the transcript but omit the pictures in v0.1.x.
- **They inflate bundle size.** Base64 encoding is ~33% larger than the raw
  image, so image-heavy sessions produce noticeably larger `.codexbundle` files.

### Practical guidance

- **Do not post bundles publicly** — not in GitHub issues, gists, pastebins, or chat.
- **Do not commit bundles to a repository.** Add `*.codexbundle` to your `.gitignore`.
- Move bundles over channels you trust (USB stick, `scp`/`rsync` over SSH,
  Syncthing, an encrypted drive).
- If you must share a bundle for debugging, **inspect it first**
  (`codex-sync inspect bundle.codexbundle`) and assume the JSONL inside contains
  everything from those sessions.
- Delete bundles you no longer need.

`codex-sync` does **not** upload anything anywhere. The only data movement is you
copying the file by hand. The privacy risk is entirely about **who you hand the
file to**.

---

## 2. Import never overwrites your sessions silently

Import is deliberately conservative. For each session in the bundle, exactly one
of these happens:

| Situation | What `codex-sync` does |
| --------- | ---------------------- |
| The session does **not** exist locally | **Imported** (new file written). |
| The session exists locally and is **identical to the effective import content** | **Skipped** (already present). |
| The session exists locally but **differs** | **Reported as a conflict and skipped** — your local file is left untouched. |

For normal imports, the effective import content is the byte-for-byte bundle
entry. For `--map-cwd` imports, the effective content is the safely rewritten
plain `.jsonl` file.

There is no force overwrite in v0.1.x. A differing file is **never** replaced. If
you see conflicts reported, your existing sessions were not modified.

---

## 3. Checksums are verified before anything is written

Every bundle carries a `checksums.json` mapping each file to its SHA-256.

- On **import**, `codex-sync` verifies the checksum of every file in the bundle
  **before it writes a single byte** to your Codex home.
- If any checksum does not match — a corrupt download, a truncated copy, or a
  tampered bundle — the import **aborts with nothing changed**.
- If `--map-cwd` is used, the original bundle checksum is still verified first.
  Then the mapped file intentionally differs from the bundle entry, so
  `codex-sync` computes a new effective checksum for conflict detection.

This is a whole-bundle gate: either the bundle is intact and import proceeds, or
it is rejected and your Codex home is left exactly as it was.

---

## 4. Path-traversal and unexpected entries are rejected

Bundles are ZIP files, and ZIP files can be malicious. `codex-sync` defends
against this:

- **Zip-slip / path traversal** (`..` segments) is rejected.
- **Absolute paths** and Windows drive-letter paths are rejected.
- **Backslashes** and non-canonical paths are rejected.
- Only entries matching `sessions/YYYY/MM/DD/rollout-*.jsonl[.zst]` are eligible
  for import. Anything else is **skipped**, never written.

A crafted bundle cannot make `codex-sync` write outside
`~/.codex/sessions/`.

---

## 5. SQLite is never modified

Codex keeps an internal SQLite database as an **index/cache**. The durable,
canonical record of every session is the JSONL rollout file on disk.

`codex-sync` works **only** with those rollout files. It **never** opens, writes,
or migrates Codex's SQLite database. After you import, Codex rebuilds its own
index from the JSONL files on its next normal scan.

This is why the recommended step after import is simply: **restart the Codex App
(or run Codex again)** so it scans and reconciles the new files itself.

---

## 6. Atomic writes

Each imported file is written to a temporary file in the destination directory
and then renamed into place. A file is therefore **never left half-written**,
even if the process is interrupted mid-import.

---

## 7. cwd mismatch can affect where sessions appear

Codex's per-project sidebar filters sessions by the **working directory recorded
in the session** at the time it was created.

If your project lives at a different path on the two machines — for example
`/home/you/dev/app` on one and `C:\Users\you\projects\app` on the other — an
imported session is stored correctly and is fully intact, but Codex may **not
show it under that project's view**, because the recorded cwd no longer matches.

`codex-sync` handles this in two ways:

- Without `--map-cwd`, it detects mismatch and warns, but imports byte-for-byte.
- With `--map-cwd OLD=NEW`, it can rewrite the recorded cwd for matching plain
  `.jsonl` sessions during import so they point at the destination machine's
  project path.

Example:

```bash
codex-sync import ./project.codexbundle \
  --map-cwd "/home/you/dev/app=C:\\Users\\you\\projects\\app" \
  --dry-run
```

Use `--dry-run` first. Path mapping is useful, but it is the only feature in
`codex-sync` that intentionally mutates session content.

---

## 8. `--map-cwd` is intentionally narrow

`--map-cwd` exists only to change Codex's project association metadata. It does
**not** do global search-and-replace.

When a mapping matches a plain `.jsonl` session:

- Only the canonical `cwd` field inside the `session_meta` line is rewritten.
- All non-`session_meta` lines are preserved byte-for-byte.
- Unknown fields inside `session_meta` are preserved semantically, although the
  `session_meta` line itself is re-serialized as JSON.
- The resulting JSONL is minimally validated before it is written.
- Existing files are still never overwritten silently.

`--map-cwd` deliberately does **not** rewrite:

- prompts
- assistant messages
- tool output
- terminal output
- file paths mentioned in normal chat content
- compressed `.jsonl.zst` sessions

If a `.jsonl.zst` session matches a mapping, it is copied byte-for-byte and
reported as not remapped. `codex-sync` does not decompress or recompress sessions
in v0.1.x.

---

## 9. Compressed sessions are copied byte-for-byte

Compressed rollout files (`.jsonl.zst`) are **never parsed or decompressed** by
`codex-sync` in v0.1.x. When included in a bundle they are copied
**byte-for-byte** and verified by checksum like any other file. Because their
contents are not read, their recorded cwd is unknown, so they are skipped by the
`--project` cwd filter on export (and reported), and they cannot be rewritten by
`--map-cwd`.

---

## 10. Dry run

Use `codex-sync import bundle.codexbundle --dry-run` to validate a bundle and see
exactly what *would* happen — new vs. already-present vs. conflict, and how many
sessions would be cwd-mapped — **without writing anything**. This is the safe way
to preview an import.

---

## 11. What codex-sync deliberately does NOT do

These are intentional non-goals. They keep the tool small, predictable, and safe:

- **Does not modify Codex's SQLite database.** Ever. It only works with rollout
  files.
- **Does not rewrite session content by default.** Normal import is byte-for-byte.
- **Does not globally rewrite paths.** `--map-cwd` only changes the canonical
  `cwd` field inside `session_meta` for matching plain `.jsonl` files.
- **Does not overwrite or merge existing sessions.** Conflicts are reported and
  skipped.
- **Does not decompress `.jsonl.zst` files.** They are copied byte-for-byte.
- **Does not upload anything.** No network calls, no cloud, no telemetry.
- **Does not require accounts, servers, or a background daemon.**
- **Does not scrub secrets from bundles.** It cannot tell what is sensitive — that
  responsibility stays with you (see §1).

---

## 12. Recommended safe workflow

1. On the source machine, run `codex-sync export --project .` from your project
   directory.
2. **Inspect the bundle** before moving it: `codex-sync inspect ./project.codexbundle`.
   Remember the JSONL inside contains the full session transcript.
3. Move the bundle over a channel you trust (USB, `scp`/`rsync` over SSH,
   Syncthing, an encrypted drive). Do **not** post it publicly.
4. On the destination machine, **dry-run first**:
   `codex-sync import ./project.codexbundle --dry-run`.
5. If the project path differs, dry-run with an explicit mapping:
   `codex-sync import ./project.codexbundle --map-cwd "OLD=NEW" --dry-run`.
6. If the dry-run looks right, import for real:
   `codex-sync import ./project.codexbundle` or
   `codex-sync import ./project.codexbundle --map-cwd "OLD=NEW"`.
7. **Restart the Codex App (or run Codex again)** so it scans and reconciles the
   imported files.
8. **Delete the bundle** once you no longer need it.

## Summary

- Bundles can contain **prompts, code, terminal output, paths, and secrets** —
  do not share them publicly.
- Import **never** overwrites silently; conflicts are reported and skipped.
- Checksums are verified **before** any write; a bad bundle changes nothing.
- Path traversal and non-session entries are rejected.
- **SQLite is never touched**; Codex rebuilds its index itself.
- A **cwd mismatch** can hide a correctly-imported session from a project view.
- `--map-cwd` can fix path mismatch for plain `.jsonl` sessions, but only by
  rewriting the canonical `cwd` field in `session_meta`.
