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
- **Encrypt the bundle** if it must travel over a channel you do not fully
  control: `codex-sync export … --encrypt-to <age-recipient>` (or
  `--passphrase`) produces a `.codexbundle.age` that only the holder of the key
  or passphrase can read (see §11).
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
| The session exists locally but **differs** | **Reported as a conflict and skipped** by default — your local file is left untouched. With `--replace-with-backup`, the local file is first backed up and then overwritten (see below). |

For normal imports, the effective import content is the byte-for-byte bundle
entry. For `--map-cwd` imports, the effective content is the safely rewritten
plain `.jsonl` file.

By default there is no force overwrite: a differing file is **never** replaced,
and if you see conflicts reported, your existing sessions were not modified.

### Opting in to replacing a conflict (`--replace-with-backup`)

A conflict means the same session exists on both machines but has diverged — for
example you continued the chat locally after a previous import. If you want the
bundle's version to win, pass `--replace-with-backup`. For each conflicting file
`codex-sync` then:

1. copies the existing local file to a sibling backup named
   `…jsonl.codexsync-bak-<timestamp>`. That suffix does **not** match Codex's
   `rollout-*.jsonl` pattern, so Codex ignores the backup on its next scan;
2. overwrites the local file with the bundle's version using the same atomic
   write (temp file + rename) as a normal import.

The previous content is therefore always recoverable from the backup. The flag
is opt-in, is reported as "Replaced (backup kept): N", and writes nothing under
`--dry-run`. Without the flag, the default never-overwrite behavior is unchanged.

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

`codex-sync` helps you find and fix this:

- **Discovery (read-only).** `inspect` lists the distinct project folders (cwds)
  recorded in a bundle and flags any that do not exist on the current machine;
  `import` shows the same summary when one or more are missing (including under
  `--dry-run`). This only reads the filesystem (`os.Stat`) and creates nothing.
  When something is missing, the output prints a ready-to-paste `--map-cwd` hint.
- Without `--map-cwd`, import detects the mismatch and warns, but imports
  byte-for-byte.
- With `--map-cwd OLD=NEW`, it can rewrite the recorded cwd for matching plain
  `.jsonl` sessions during import so they point at the destination machine's
  project path. (You can also simply create an empty folder at the recorded cwd
  and restart Codex.)

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

## 10. Git-assisted handoff is read-only and opt-in

`--with-git` (export) and `--clone` (import) help move the **project code** that
a session refers to, without weakening the safety model.

- **On export**, `--with-git` only **reads** git. It records the project's
  remote URL, branch, commit SHA, and whether the tree was dirty/unpushed into
  `manifest.json`. It never commits, pushes, or creates anything. The recorded
  remote URL and branch names become part of the bundle, so treat them as you
  would the rest of its contents (see §1).
- **On import**, with no `--clone` flag, codex-sync only **prints** the
  `git clone … && git checkout <commit>` commands. Nothing runs.
- **`import --clone <dir>`** is the single feature that makes an outbound network
  call: it runs `git clone <recorded-remote> <dir>` and then
  `git checkout <recorded-commit>`. It is explicit, it only fetches (never
  pushes), it is skipped under `--dry-run`, and it writes only inside the `<dir>`
  you name — never into your Codex home.

If a bundle came from an untrusted source, review the recorded remote URL with
`codex-sync inspect` before using `--clone`, since cloning executes git against
whatever URL the manifest contains.

---

## 11. Encryption is optional, opt-in, and external

A `.codexbundle` is plaintext by default (see §1). When you must move one over a
channel you do not fully control, codex-sync can encrypt it for you. Like the git
integration, it does **not** embed a crypto library: it shells out to the
well-known [`age`](https://github.com/FiloSottile/age) tool, keeping codex-sync a
single, dependency-free binary.

- **On export**, `--encrypt-to <recipient>` (repeatable), `--recipients-file`,
  or `--passphrase` write the bundle to `<output>.age`. The intermediate
  plaintext bundle is **removed** afterward, so a clear copy is not left behind.
- **On import/inspect**, a `.age` input is auto-detected and decrypted to a
  **temporary file** (requiring `--identity <file>` or `--passphrase`). That
  temporary plaintext is deleted when the command finishes.
- `--passphrase` is mutually exclusive with `--encrypt-to`/`--recipients-file`
  (age cannot mix the two).
- If `age` is not installed, encryption/decryption **fails with install
  guidance** and changes nothing else.

Encryption only protects the bundle **in transit and at rest**. Once decrypted
for import, the sessions are written to your Codex home in the clear, exactly as
an unencrypted bundle would be. It does not scrub secrets from the transcript
(see §1); it only controls **who can open the file**. And it does not change the
"never uploads" guarantee — encrypting still happens entirely on your machine.

---

## 12. Dry run

Use `codex-sync import bundle.codexbundle --dry-run` to validate a bundle and see
exactly what *would* happen — new vs. already-present vs. conflict, and how many
sessions would be cwd-mapped — **without writing anything**. This is the safe way
to preview an import.

---

## 13. What codex-sync deliberately does NOT do

These are intentional non-goals. They keep the tool small, predictable, and safe:

- **Does not modify Codex's SQLite database.** Ever. It only works with rollout
  files.
- **Does not rewrite session content by default.** Normal import is byte-for-byte.
- **Does not globally rewrite paths.** `--map-cwd` only changes the canonical
  `cwd` field inside `session_meta` for matching plain `.jsonl` files.
- **Does not overwrite or merge existing sessions by default.** Conflicts are
  reported and skipped. The only way to overwrite is the opt-in
  `--replace-with-backup`, which keeps a recoverable backup of the local file
  first (see §2); even then nothing is merged.
- **Does not decompress `.jsonl.zst` files.** They are copied byte-for-byte.
- **Does not upload anything.** It never sends your sessions, code, or any other
  data off your machine — no cloud, no telemetry, no `git push`, no repo
  creation. The one outbound network action is `import --clone`, which you opt
  into explicitly and which only *fetches* the project code you asked for via
  `git clone` (see §10).
- **Does not require accounts, servers, or a background daemon.**
- **Does not scrub secrets from bundles.** It cannot tell what is sensitive — that
  responsibility stays with you (see §1). Optional `age` encryption (§11)
  controls *who can open* a bundle, but it does not remove secrets from the
  transcript inside.
- **Does not embed a crypto library.** Encryption shells out to the external
  `age` tool; without `age` installed, encryption simply errors.

---

## 14. Recommended safe workflow

1. On the source machine, run `codex-sync export --project .` from your project
   directory.
2. **Inspect the bundle** before moving it: `codex-sync inspect ./project.codexbundle`.
   Remember the JSONL inside contains the full session transcript.
3. Move the bundle over a channel you trust (USB, `scp`/`rsync` over SSH,
   Syncthing, an encrypted drive). Do **not** post it publicly. If the channel
   is not fully trusted, export with `--encrypt-to <recipient>` (or
   `--passphrase`) and move the resulting `.age` file instead (see §11).
4. On the destination machine, **dry-run first**:
   `codex-sync import ./project.codexbundle --dry-run`. Check the
   **Project folders (recorded cwd)** summary: any folder flagged `[missing]`
   will be hidden from Codex's sidebar until you create it or remap it.
5. If the project path differs, dry-run with an explicit mapping:
   `codex-sync import ./project.codexbundle --map-cwd "OLD=NEW" --dry-run`.
6. If the dry-run looks right, import for real:
   `codex-sync import ./project.codexbundle` or
   `codex-sync import ./project.codexbundle --map-cwd "OLD=NEW"`. If a session
   diverged on this machine and you want the bundle's version, add
   `--replace-with-backup` (a backup of the local file is kept).
7. **Restart the Codex App (or run Codex again)** so it scans and reconciles the
   imported files.
8. If the session needs the project's code on this machine, either export with
   `--with-git` and follow the printed `git clone …` commands, or import with
   `--clone <dir>` to fetch the recorded commit (review the remote URL with
   `inspect` first if the bundle is not from you).
9. **Delete the bundle** once you no longer need it.

## Summary

- Bundles can contain **prompts, code, terminal output, paths, and secrets** —
  do not share them publicly.
- Import **never** overwrites silently; conflicts are reported and skipped unless
  you opt in with `--replace-with-backup`, which keeps a recoverable backup.
- Checksums are verified **before** any write; a bad bundle changes nothing.
- Path traversal and non-session entries are rejected.
- **SQLite is never touched**; Codex rebuilds its index itself.
- A **cwd mismatch** can hide a correctly-imported session from a project view;
  `inspect`/`import` flag missing project folders so you can spot this.
- `--map-cwd` can fix path mismatch for plain `.jsonl` sessions, but only by
  rewriting the canonical `cwd` field in `session_meta`.
- Bundles can be **encrypted** with the external `age` tool (`--encrypt-to` /
  `--passphrase`); this controls who can open a bundle but does not scrub the
  secrets inside it, and codex-sync still never uploads anything.
