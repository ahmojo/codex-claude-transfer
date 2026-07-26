# Codex post-import discovery investigation

Last verified: 2026-07-23, Codex app-server 0.144.6 on Windows. All experiments
used a throwaway `CODEX_HOME` and synthetic rollout; no real user session or
database was inspected or modified.

The original field report came from Codex Desktop session metadata identifying
0.145.0-alpha.30, but that installation was not used for destructive or index
experiments. Compatibility with that alpha remains capability-probed at runtime,
not claimed from the 0.144.6 result.

## Observed boundary

A valid rollout file and Codex's ordinary recent-thread discovery are separate
states. When app-server was started against an empty synthetic home, allowed to
finish its initial scan, and then a valid rollout was written under
`sessions/2026/07/23/`, the exact sequence was:

1. `thread/list` with `useStateDbOnly: true` did not contain the new thread.
2. `thread/read` with the exact thread ID succeeded from the rollout slow path.
3. A second state-only `thread/list` contained the thread.
4. Codex logged its own `read_repair_rollout_path: upsert_needed` warning.

This reproduces the important part of the Desktop report: file restoration can
be correct while the already-running process's state-backed recent list remains
stale. It does not establish that every Codex version or UI uses the same list
parameters.

## Capability findings

The 0.144.6 generated app-server schema exposes:

- `thread/read { threadId, includeTurns }`;
- `thread/list { ..., useStateDbOnly }`, where false/omitted permits Codex's own
  scan-and-repair behavior;
- `thread/search`, although the minimal synthetic rollout was not returned by
  full-text search before read-repair.

The app-server surface is explicitly experimental, so cct does not make version
alone an authorization gate. `--reconcile` records the version reported during
`initialize`, probes state-only `thread/list`, falls back to native default
scan-and-repair list when that field is rejected, uses exact `thread/read`, and
verifies the resulting list. It also rejects an initialized `codexHome` that
does not match the requested target.

## Safety conclusion

Direct SQLite or `session_index.jsonl` writes remain outside cct's boundary.
The file importer completes and records its undo journal first. Reconciliation
then delegates to Codex's own process and is best-effort: a missing binary,
protocol drift, timeout, read error, or failed verification preserves the
imported rollout and produces restart / `cct resume <thread-id> --run` guidance.
