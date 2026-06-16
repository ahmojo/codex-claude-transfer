package handoff

import (
	"bytes"
	"encoding/json"
	"time"
)

// codexTargetVersion identifies a translated rollout as cct-produced.
const codexTargetVersion = "cct-handoff"

// ToCodex synthesizes a Codex rollout (JSONL) from the neutral AgentSession. The
// result is a real, discoverable session: a session_meta line, then the handoff
// preamble and the prior conversation. Each turn is emitted both as the durable
// response_item message (what Codex replays to the model on resume) and, for
// user/assistant prose, as the event_msg the UI uses for the list preview and
// transcript. Tool turns become assistant text notes. It returns the JSONL bytes
// and the deterministic session id.
func ToCodex(s AgentSession) ([]byte, string, error) {
	sessionID := DeterministicID(handoffSourceKey(s), "codex")
	turns := turnsWithPreamble(s)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	base := baseTime(s)
	tsAt := func(i int) string { return base.Add(time.Duration(i) * time.Second).Format(time.RFC3339) }

	// session_meta
	meta := map[string]any{
		"id":             sessionID,
		"timestamp":      tsAt(0),
		"cwd":            s.CWD,
		"originator":     "cct",
		"cli_version":    codexTargetVersion,
		"source":         "cli",
		"model_provider": "",
	}
	if s.Git != nil {
		g := map[string]any{}
		if s.Git.Branch != "" {
			g["branch"] = s.Git.Branch
		}
		if s.Git.Commit != "" {
			g["commit_hash"] = s.Git.Commit
		}
		if s.Git.Remote != "" {
			g["origin_url"] = s.Git.Remote
		}
		if len(g) > 0 {
			meta["git"] = g
		}
	}
	if err := enc.Encode(rollout(tsAt(0), "session_meta", meta)); err != nil {
		return nil, "", err
	}

	for i, t := range turns {
		ts := tsAt(i + 1)
		text := renderTurnText(t)
		switch t.Role {
		case RoleUser:
			if err := enc.Encode(rollout(ts, "event_msg", map[string]any{"type": "user_message", "message": text})); err != nil {
				return nil, "", err
			}
			if err := enc.Encode(rollout(ts, "response_item", codexMessage("user", "input_text", text))); err != nil {
				return nil, "", err
			}
		case RoleAssistant:
			if err := enc.Encode(rollout(ts, "event_msg", map[string]any{"type": "agent_message", "message": text})); err != nil {
				return nil, "", err
			}
			if err := enc.Encode(rollout(ts, "response_item", codexMessage("assistant", "output_text", text))); err != nil {
				return nil, "", err
			}
		case RoleTool:
			// Tool summaries are model context only (no separate UI event).
			if err := enc.Encode(rollout(ts, "response_item", codexMessage("assistant", "output_text", text))); err != nil {
				return nil, "", err
			}
		}
	}
	return buf.Bytes(), sessionID, nil
}

// rollout wraps a payload in the on-disk {timestamp, type, payload} envelope.
func rollout(ts, typ string, payload map[string]any) map[string]any {
	return map[string]any{"timestamp": ts, "type": typ, "payload": payload}
}

// codexMessage builds a response_item "message" payload with one content block.
func codexMessage(role, blockType, text string) map[string]any {
	return map[string]any{
		"type":    "message",
		"role":    role,
		"content": []map[string]any{{"type": blockType, "text": text}},
	}
}
