# Demo

A visual tour of `cct`. Everything below runs against throwaway demo sessions —
never a real `~/.codex` or `~/.claude`, so no real prompts, code, or paths appear.

### Move sessions between machines, incrementally

Export on one machine, sync onto the other — only what's new is appended,
nothing is re-pasted or overwritten.

![Overview: doctor, grouped list, export](clips/01-overview.gif)
![Incremental sync with import --merge](clips/02-sync.gif)

### Sync over your local network

Same Wi-Fi? Skip the file — `cct sync` pairs the two devices with a one-time code
and moves new/grown sessions both ways. Peer-to-peer, no server, opt-in.

![LAN sync front door](clips/15-sync.gif)

### Find a past conversation

Full-text search across your sessions, then export just what matches.

![cct search](clips/11-search.gif)

### Check for secrets before sharing

`cct scan` flags likely API keys/tokens; `export --redact` replaces them with
placeholders.

![cct scan and export --redact](clips/12-secrets.gif)

### Save a session as readable Markdown

`export --format md` turns a conversation into a shareable document.

![export --format md](clips/13-markdown.gif)

### Fix slow-opening imported sessions

`doctor` spots imported files whose timestamps confuse the agent; `repair-times`
fixes them (timestamps only, never content).

![doctor + repair-times](clips/14-repair-times.gif)

### Desktop app

The same features in a local browser GUI (`cct app`).

![The cct desktop WebUI](clips/10-webui.gif)

### Cross-agent handoff

Translate Codex sessions into Claude Code (or back).

![Codex → Claude Code handoff](clips/03-claude-handoff.gif)

### Encryption

Encrypt a bundle to an age key; decrypt it with your private key.

![age encryption round trip](clips/04-encryption.gif)

### When a session changed on both machines

Replace and keep a backup, keep both copies, or redirect the project path.

![Conflict resolution and cwd remap](clips/05-conflicts-remap.gif)

### Export only what you need

By project, by single session, or by "changed since".

![Export filters](clips/06-export-filters.gif)

### Carry the matching code too

Record the project's git remote/commit, then clone it on the other side.

![Git handoff](clips/07-git-handoff.gif)

### Guided, if you prefer menus over flags

![The cct ui wizard](clips/08-cli-ui.gif)

### Compressed sessions

Codex stores older sessions as `.jsonl.zst`; with `zstd` installed, `cct` reads
them like any other.

![Reading compressed .jsonl.zst sessions](clips/09-compressed.gif)

---

<details>
<summary>How these are recorded (for contributors)</summary>

The rendered GIFs live in `clips/`; the scripts that produce them are in
`recording/`. Terminal clips are rendered with
[VHS](https://github.com/charmbracelet/vhs); the WebUI clip with
[Playwright](https://playwright.dev). Nothing here touches a real agent home —
`recording/gen_demo_home.py` writes fake Codex homes under a temp dir and the
clips point `CODEX_HOME`/`CLAUDE_HOME` at them.

- `recording/gen_demo_home.py <base>` — fake Codex homes (`laptop/`, `pc/`) with
  demo sessions. `CCT_DEMO_BASE` overrides the project-path prefix (Windows paths
  for the WebUI clip).
- `recording/prep.sh <scenario>` — per-tape setup: `base`, `claude`, `enc`,
  `conflict`, `mapcwd`, `git`, `zstd`, `secrets` (injects fake EXAMPLE keys), and
  `stale` (a session whose mtime runs ahead of its content).
- `recording/*.tape` — VHS scripts (typed commands + timing).
- `recording/record.sh` — renders all terminal GIFs into `clips/` in one shot.
- `webui-rec/` — the Playwright project that records `clips/10-webui.gif`.

**Terminal clips (VHS, needs Linux + `ttyd` + `ffmpeg`; on Windows use WSL):**

```bash
wsl -d Ubuntu -- bash -lc 'bash /path/to/repo/demo/recording/record.sh'
```

`record.sh` is self-bootstrapping (no sudo): it fetches the headless-Chromium NSS
libs locally, installs `age` if missing, and builds `cct` from source. One-time:
`vhs` (`go install github.com/charmbracelet/vhs@latest`) and a static `ttyd`
binary on `PATH`.

**WebUI clip (Playwright, runs on Windows where Chrome has its libraries):**

```powershell
cd demo/webui-rec
npm install && npx playwright install chromium
$env:CCT_HOME = "C:\path\to\cct-webui-demo\laptop\codex-home"
node record-webui.mjs            # writes out/<hash>.webm
# convert webm -> gif with ffmpeg (palette flags as in record.sh)
```

</details>
