---
name: cct-session-sync
description: Carry a project's Codex or Claude Code session history between machines inside the project's own git repository, using the cct CLI. Use when the user wants to continue this project's agent sessions on another machine, asks to save/checkpoint/publish the session history, mentions a .codexbundle, or has just cloned a repo that already contains a .cct/ folder.
---

# cct session sync

Keep this project's agent session history in the project's own git repo as a
single bundle under `.cct/`, so a clone on another machine can restore the
history and continue the conversation there.

Two moves, both run from the project root:

- **Save** — export the project's sessions into `.cct/`, then commit.
- **Restore** — after a clone, import `.cct/` into the local agent home.

The tool is [`cct`](https://github.com/ahmojo/codex-claude-transfer). Check it is
installed with `cct version`; if it is missing, stop and tell the user to run
`go install github.com/ahmojo/codex-claude-transfer/cmd/cct@latest` or download a
release binary. Do not try to reimplement any of this by hand: never copy,
rename, or edit files under `~/.codex` or `~/.claude` yourself.

## Rules

These are not suggestions. Follow them even when the user seems to be in a hurry.

1. **A bundle is sensitive.** It contains prompts, code, command output, file
   paths, and anything else that was on screen. Committing it puts all of that
   in the repo's history for everyone with access, permanently.
2. **Plain mode requires a private repo.** Before the first commit of a plain
   (unencrypted) bundle, run `git remote -v`, show the remote to the user, and
   get an explicit confirmation that the repo is private. If they cannot confirm
   it, use encrypted mode or stop.
3. **Never pass `--allow-secrets`.** If export refuses because it found a likely
   credential, that is the feature working. Show the user `cct scan --project .`
   and let them decide between fixing the source, `--redact`, or stopping. Only
   the user may choose `--allow-secrets`, and only after seeing the findings.
4. **Never `git push` on your own.** Commit, show the user what is staged, and
   ask before pushing.
5. **Never edit the agent's index.** That is Codex's SQLite database and Claude
   Code's `~/.claude.json`. `cct` deliberately leaves both alone; each agent
   rebuilds its index by rescanning the session files.
6. **Do not paste bundle contents into chat, issues, or PR descriptions.**

## Setup (once per user)

Check the saved mode first:

```bash
cct config get repo-sync
```

If it prints a value (`plain` or `encrypted`), setup is done — skip to Save or
Restore. If it is empty, ask the user which they want and say what each means:

- **plain** — the bundle is committed as-is. Simple, works everywhere, and only
  acceptable in a private repo.
- **encrypted** — the bundle is committed as `age`-encrypted `.age` bytes.
  Safe even if the repo is public, but the [`age`](https://github.com/FiloSottile/age)
  binary must be installed on every machine and the private key must be
  available to decrypt (it must never be committed). `cct doctor` reports
  whether `age` is on PATH.

Then save the choice:

```bash
cct config set repo-sync plain
# or, for encrypted mode, also record the recipient to encrypt to:
cct config set repo-sync encrypted
cct config set repo-sync-recipient age1yourrecipientkey...
```

Per repo, once: confirm the remote (rule 2), and make sure `.cct/` is not
git-ignored — `git check-ignore -v .cct` should print nothing.

## Save (end of a working session)

Pick the tool the session belongs to: `--tool claude` for Claude Code,
`--tool codex` for Codex. The file name follows the tool so both can coexist.

Plain mode:

```bash
cct export --project . --tool claude -o .cct/claude.codexbundle
```

Encrypted mode (writes `.cct/claude.codexbundle.age` and leaves no plaintext
bundle behind):

```bash
cct export --project . --tool claude -o .cct/claude.codexbundle \
  --encrypt-to "$(cct config get repo-sync-recipient)"
```

Then check what changed and commit — but do not push (rule 4):

```bash
git add .cct
git status --short .cct
git commit -m "Update .cct session bundle"
```

Notes:

- Export rewrites the bundle from the sessions currently on this machine, so the
  committed file is always the full history for this project, not a delta. Each
  commit stores a new copy of it; if the repo grows uncomfortably, tell the user
  and offer `--since 30d` to bundle only recent sessions.
- `--with-git` additionally records the project's remote, branch, and commit in
  the bundle, which helps when the code and the sessions are restored together.
- If export refuses over a likely secret, go to rule 3.

## Restore (after cloning on another machine)

Look for `.cct/*.codexbundle` or `.cct/*.codexbundle.age` in the repo. Preview
first — `diff` is read-only and writes nothing:

```bash
cct diff .cct/claude.codexbundle --map-cwd-here
```

`--map-cwd-here` rewrites the sessions' recorded working directory to the
current folder, which is what makes them show up under this clone's path. It
matters: an agent only groups a session under a project whose path matches the
recorded one exactly.

If the preview looks right, import:

```bash
cct import .cct/claude.codexbundle --merge --map-cwd-here
```

`--merge` makes repeat imports incremental instead of conflicting: a session
that already exists locally and only grew is extended, and identical ones are
skipped. Import never silently overwrites.

For an encrypted bundle, add the key: `--identity ~/.config/age/key.txt`, or
`--passphrase` if it was encrypted with one.

Then tell the user to **restart the agent** so it rescans its session files, and
show them how to pick a session back up:

```bash
cct list --tool claude
cct resume <thread-id>
```

## When something goes wrong

- **Sessions imported but not visible in the agent.** Almost always the recorded
  working directory. Confirm with `cct list`, then re-import with
  `--map-cwd-here` (or `--map-cwd "<old>=<new>"`), and restart the agent.
- **The agent re-parses everything on each open, or ordering looks wrong.** Run
  `cct repair-times` once; it only fixes imported files' modification times.
- **Conflicts reported on import.** Re-run `cct diff` to see them. Do not reach
  for `--replace-with-backup` or `--import-as-copy` without asking the user
  which resolution they want.
- **The import was a mistake.** `cct undo --dry-run` shows what would be
  reversed, `cct undo` reverses it.
- **The project folder moved.** `cct relocate <old> <new> [--tool claude]`
  rewrites the recorded path; `--dry-run` previews it.

## Multiple machines

Both sides run the same two commands: Save before you stop, Restore after you
pull. Because import is a merge and never overwrites, pulling a bundle that is
older than what is on this machine is harmless — it is skipped. The one thing to
avoid is committing a bundle without pulling first, which just creates a normal
git conflict on a binary file; resolve it by taking either side and re-exporting.
