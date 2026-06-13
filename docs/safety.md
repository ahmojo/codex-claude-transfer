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

> **Treat a `.codexbundle` like you would treat your shell history plus your
> source tree.** It is at least as sensitive as both.

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
| The session exists locally and is **byte-identical** | **Skipped** (already present). |
| The session exists locally but **differs** | **Reported as a conflict and skipped** — your local file is left untouched. |

There is no "force overwrite" in v0.1. A differing file is **never** replaced.
If you see conflicts reported, your existing sessions were not modified.

---

## 3. Checksums are verified before anything is written

Every bundle carries a `checksums.json` mapping each file to its SHA-256.

- On **import**, `codex-sync` verifies the checksum of every file in the bundle
  **before it writes a single byte** to your Codex home.
- If any checksum does not match — a corrupt download, a truncated copy, or a
  tampered bundle — the import **aborts with nothing changed**.

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

`codex-sync` works **only** with those JSONL files. It **never** opens, writes,
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

In v0.1, `codex-sync`:

- **Detects** this and **warns** you (especially with `--project`).
- Does **not** rewrite the recorded cwd or any other part of the JSONL.

**Recommendation:** use the same project path on both machines if you want
imported sessions to appear in the project-specific sidebar. Path rewriting is a
roadmap item and will only ship once it is proven safe.

---

## 8. Compressed sessions are copied byte-for-byte

Compressed rollout files (`.jsonl.zst`) are **never parsed or decompressed** by
`codex-sync` in v0.1. When included in a bundle they are copied **byte-for-byte**
and verified by checksum like any other file. Because their contents are not
read, their recorded cwd is unknown, so they are skipped by the `--project` cwd
filter on export (and reported).

---

## 9. Dry run

Use `codex-sync import bundle.codexbundle --dry-run` to validate a bundle and see
exactly what *would* happen — new vs. already-present vs. conflict — **without
writing anything**. This is the safe way to preview an import.

---

## 10. What codex-sync deliberately does NOT do

These are intentional non-goals. They keep the tool small, predictable, and safe:

- **Does not modify Codex's SQLite database.** Ever. It only reads/writes JSONL
  rollout files.
- **Does not rewrite the contents of any session.** JSONL files are copied
  verbatim; the recorded cwd and all other fields are left untouched.
- **Does not overwrite or merge existing sessions.** Conflicts are reported and
  skipped.
- **Does not decompress `.jsonl.zst` files.** They are copied byte-for-byte.
- **Does not upload anything.** No network calls, no cloud, no telemetry.
- **Does not require accounts, servers, or a background daemon.**
- **Does not scrub secrets from bundles.** It cannot tell what is sensitive — that
  responsibility stays with you (see §1).

## 11. Recommended safe workflow

1. On the source machine, run `codex-sync export --project .` from your project
   directory.
2. **Inspect the bundle** before moving it: `codex-sync inspect ./project.codexbundle`.
   Remember the JSONL inside contains the full session transcript.
3. Move the bundle over a channel you trust (USB, `scp`/`rsync` over SSH,
   Syncthing, an encrypted drive). Do **not** post it publicly.
4. On the destination machine, **dry-run first**:
   `codex-sync import ./project.codexbundle --dry-run`.
5. If the dry-run looks right, import for real:
   `codex-sync import ./project.codexbundle`.
6. **Restart the Codex App (or run Codex again)** so it scans and reconciles the
   imported files.
7. For best project-sidebar visibility, use the **same project path** on both
   machines.
8. **Delete the bundle** once you no longer need it.

## Summary

- Bundles can contain **prompts, code, terminal output, paths, and secrets** —
  do not share them publicly.
- Import **never** overwrites silently; conflicts are reported and skipped.
- Checksums are verified **before** any write; a bad bundle changes nothing.
- Path traversal and non-session entries are rejected.
- **SQLite is never touched**; Codex rebuilds its index itself.
- A **cwd mismatch** can hide a correctly-imported session from a project view.
