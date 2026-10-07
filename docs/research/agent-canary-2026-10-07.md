# Agent compatibility canary — 2026-10-07

Verified locally on Windows with the official Codex CLI
[0.161.0](https://github.com/openai/codex/releases/tag/rust-v0.161.0), Claude Code
[2.1.292](https://github.com/anthropics/claude-code/releases/tag/v2.1.292), and
Claude Agent SDK **0.3.292**. All inputs were synthetic; homes,
projects, config, and API endpoints were disposable. Model replies came from a
loopback mock server, so this does not establish real-provider or desktop UI
compatibility. CCT did not write agent databases or user configuration.

| Check | Result |
| --- | --- |
| Codex legacy and paginated export/import | Two native committed turns preserved; latest user/assistant messages reached the resumed model request. |
| Codex reconcile | `--reconcile` verified each imported legacy, paginated, and native subagent thread via app-server `thread/read`. |
| Codex fork | Archived parent included; resumed model request contained inherited history. Copy, cwd remap, missing parent, redaction, image stripping, and translation rejected before writes. |
| Codex native subagent | Export/import, native read/resume, and Markdown recovery passed. |
| Claude parent and subagent | Transcripts imported byte-for-byte; parent resumed with the latest committed turn; `SendMessage` continued the imported child with its previous user/assistant history. |
| Claude persisted tasks | Session task JSON transferred unchanged; native `TaskList` matched the source; creating another task allocated ID `2` and preserved task `1`. |
| Claude task undo | CLI undo removed newly imported tasks and restored a replaced task from its backup, while reversing the associated transcript import. |
| Claude interactive plan mode | Actual `cct resume --run` launched with `--permission-mode plan`; both source and imported chats committed their next user turn in plan mode. |
| Claude `/branch` and `/clear` | Source and imported chats kept plan mode but both returned an empty task list after these commands. This behavior reproduced before transfer in the isolated safe-mode test. |
| Claude → Codex | Provider omitted from translated metadata; native continuation used the destination provider without changing `config.toml`. |
| Unknown Codex `history_base` | String, array, and object without `thread_id` rejected before destination writes. |
| 141 MiB input | Default rejected without writes; explicit 150 MiB import preserved SHA-256; dry run warned and wrote nothing; limit above hard cap rejected. |

The requested Codex parser regressions, `TestClaudeTasksRoundTrip`, and
`TestResumeClaudePreservesPlanMode` failed before their fixes and passed
afterward. Size-limit, unknown-history, task-conflict, selection, malformed-task,
secret-redaction, and oversized-record regressions also passed.

## State boundaries

Codex paginated turns populated after `thread/resume`; the empty fork's turn
list did not expose its inherited prefix. Continuation was verified against the
actual model request, including the latest committed parent turn.

Claude bundles now include numeric task JSON under `tasks/<session-id>/`, bound
to the selected parent transcript and validated before any import writes.
Differing local tasks remain conflicts unless backup/replacement is requested;
task-bearing bundles reject copy imports. Locks and external/shared task lists
are excluded. These checks establish session-bound task restoration, rather
than complete runtime-state transfer.

Direct headless/native CLI resume without CCT's plan flag used `default` mode
in both the original and imported homes. CCT's launcher now restores recorded
plan mode explicitly. Interactive terminals used Claude's safe mode, synthetic
credentials, and a loopback API; task and permission state after `/branch` and
`/clear` matched the original chat. Their empty task lists remain a native
behavior observed in this setup. Elevated permissions are never restored.
Desktop UI and SQLite corruption recovery were not verified.

## Local validation

```text
go fmt ./...
go build ./...
go vet ./...
go test ./... -count=1
python -B -m unittest discover -s scripts -p 'test_*.py' -v
python -m compileall -q scripts
```

These functional checks passed with Go **1.26.8**; all 14 Python tests passed.
Python bytecode went to a disposable cache. Windows sandbox atomic renames were
denied; the fake-home checks passed outside that sandbox. Hosted CI results are
recorded separately in the pull-request checks.

All five interactive state assertions passed and the runner exited 0. Its
Windows `node-pty` teardown helper logged `AttachConsole failed` after the
native terminals exited; these cleanup diagnostics remain in the probe log.

## Additional security check

The original Go 1.26.4 binary scan found four standard-library advisories:
GO-2026-6090, GO-2026-6089, GO-2026-5972, and GO-2026-5856. The project now
selects the patched [Go 1.26.8 toolchain](https://go.dev/dl/). The host's Go
installation was unchanged; the downloaded Windows archive was verified
against the official SHA-256.

The source scanner v1.4.0 encountered the documented
[`x/tools` panic](https://github.com/golang/go/issues/80059). CI now pins the
compatible scanner v1.3.0 and checks both source and the built executable:

```text
govulncheck ./...
govulncheck -mode=binary cct-final.exe
```

Both final scans exited **0**, reporting **0 affected vulnerabilities**. They
still list one package and two module advisories that the code does not appear
to call; this is reachability evidence, not a claim that every dependency is
advisory-free. CI and release use `go-version-file: go.mod`, which selects the
new toolchain. These local scans do not establish hosted CI status.
