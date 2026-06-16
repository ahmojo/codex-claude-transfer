package handoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// claudeTargetVersion is the version string stamped on synthesized Claude lines.
// It is deliberately not a real Claude Code version so a translated transcript is
// identifiable as such.
const claudeTargetVersion = "cct-handoff"

// ToClaude synthesizes a Claude Code transcript (JSONL) from the neutral
// AgentSession. The result is a real, discoverable session: it opens with the
// handoff preamble (as the first user message) and then replays the conversation
// as plain user/assistant text turns, chained by parentUuid. Tool turns become
// short assistant text notes (never replayed tool_use blocks, whose ids would not
// match anything). It returns the JSONL bytes and the deterministic session id.
func ToClaude(s AgentSession) ([]byte, string, error) {
	sessionID := DeterministicID(handoffSourceKey(s), "claude")
	turns := turnsWithPreamble(s)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	base := baseTime(s)
	parent := "" // first line has null parentUuid
	for i, t := range turns {
		uuid := lineUUID(sessionID, i)
		ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		line := map[string]any{
			"type":        claudeRole(t.Role),
			"uuid":        uuid,
			"sessionId":   sessionID,
			"cwd":         s.CWD,
			"version":     claudeTargetVersion,
			"timestamp":   ts,
			"userType":    "external",
			"isSidechain": false,
			"entrypoint":  "cct",
			"message":     claudeMessage(t),
		}
		if parent == "" {
			line["parentUuid"] = nil
		} else {
			line["parentUuid"] = parent
		}
		if s.Git != nil && s.Git.Branch != "" {
			line["gitBranch"] = s.Git.Branch
		}
		if err := enc.Encode(line); err != nil {
			return nil, "", err
		}
		parent = uuid
	}
	return buf.Bytes(), sessionID, nil
}

// claudeRole maps an IR role to a Claude line type. A tool turn is rendered as an
// assistant note.
func claudeRole(r Role) string {
	if r == RoleUser {
		return "user"
	}
	return "assistant"
}

// claudeMessage builds the message object for a turn. User content is a plain
// string (matching real transcripts); assistant content is a single text block.
func claudeMessage(t Turn) map[string]any {
	text := renderTurnText(t)
	if t.Role == RoleUser {
		return map[string]any{"role": "user", "content": text}
	}
	return map[string]any{
		"role":    "assistant",
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

// handoffSourceKey is the stable identity used to derive the target session id:
// the source thread id when known, else a hash of the conversation so re-running
// is still idempotent.
func handoffSourceKey(s AgentSession) string {
	if s.ThreadID != "" {
		return s.SourceAgent + ":" + s.ThreadID
	}
	return s.SourceAgent + ":" + s.CWD + ":" + fmt.Sprint(len(s.Conversation))
}

// lineUUID derives a stable per-line uuid from the session id and line index, so
// a synthesized transcript is byte-identical across runs (idempotent re-import).
func lineUUID(sessionID string, i int) string {
	return DeterministicID(sessionID, fmt.Sprintf("line-%d", i))
}
