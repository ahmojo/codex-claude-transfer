# Claude conversation groups

Exports carry a parent transcript and its `SESSION/subagents/agent-*.jsonl`
companions as one conversation. Selecting a parent ID or a unique prefix includes
the full group. A match or modification-time filter that selects a member also
includes its companions. Cwd mapping preserves the nested directory layout.

LAN sync fingerprints all group members together. Both peers should run a version
with conversation-group support; older versions reject nested import paths.

`--import-as-copy` currently rejects bundles containing Claude subagent transcripts
before writing anything. To preserve a divergent group, import it into a separate
Claude home without that flag. Plain conversations still support import-as-copy.

This transfers transcripts, not running subagent processes or project files.
