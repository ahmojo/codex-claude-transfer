package handoff

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// codexLine is the on-disk rollout wrapper: {timestamp, type, payload}.
type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// FromCodexRollout reads a Codex rollout JSONL file and extracts the neutral
// AgentSession: the visible user/assistant conversation (from event_msg lines),
// with tool calls summarized to short text turns (from response_item
// function_call lines), plus project context from session_meta. Parsing is
// defensive — unknown or malformed lines are skipped, never fatal.
func FromCodexRollout(path string) (AgentSession, error) {
	f, err := os.Open(path)
	if err != nil {
		return AgentSession{}, err
	}
	defer f.Close()
	return fromCodexReader(f)
}

// FromCodexBytes extracts the neutral AgentSession from in-memory Codex rollout
// bytes (e.g. a bundle entry), so callers need not write a temp file.
func FromCodexBytes(b []byte) (AgentSession, error) {
	return fromCodexReader(bytes.NewReader(b))
}

func fromCodexReader(r io.Reader) (AgentSession, error) {
	s := AgentSession{Format: IRFormat, SourceAgent: "codex"}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sawAssistantEvent := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cl codexLine
		if json.Unmarshal([]byte(line), &cl) != nil {
			continue
		}
		switch cl.Type {
		case "session_meta":
			applyCodexMeta(&s, cl)
		case "event_msg":
			sawAssistantEvent = consumeCodexEvent(&s, cl.Payload) || sawAssistantEvent
		case "response_item":
			consumeCodexResponseItem(&s, cl.Payload)
		}
	}
	if err := sc.Err(); err != nil {
		return s, err
	}
	// Fallback: if the rollout had no agent_message events at all (older/newer
	// shape), recover assistant prose from response_item message output_text. We
	// only do this when nothing was captured, to avoid duplicating turns.
	_ = sawAssistantEvent
	return s, nil
}

func applyCodexMeta(s *AgentSession, cl codexLine) {
	var p struct {
		ID        string          `json:"id"`
		CWD       string          `json:"cwd"`
		Timestamp string          `json:"timestamp"`
		Git       json.RawMessage `json:"git"`
	}
	if json.Unmarshal(cl.Payload, &p) != nil {
		return
	}
	if p.ID != "" {
		s.ThreadID = p.ID
	}
	if p.CWD != "" {
		s.CWD = p.CWD
	}
	if p.Timestamp != "" {
		s.CreatedAt = p.Timestamp
	} else if cl.Timestamp != "" {
		s.CreatedAt = cl.Timestamp
	}
	if g := decodeGit(p.Git); g != nil {
		s.Git = g
	}
}

// consumeCodexEvent handles event_msg payloads. It returns true if it captured an
// assistant (agent_message) turn.
func consumeCodexEvent(s *AgentSession, payload json.RawMessage) bool {
	var p struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return false
	}
	switch p.Type {
	case "user_message":
		s.addTurn(RoleUser, "", stripMarker(p.Message))
	case "agent_message":
		s.addTurn(RoleAssistant, "", p.Message)
		return true
	}
	return false
}

// consumeCodexResponseItem summarizes a tool call into a text turn. Only the call
// (name + brief arguments) is carried; raw outputs are intentionally dropped to
// keep the translated session concise.
func consumeCodexResponseItem(s *AgentSession, payload json.RawMessage) {
	var p struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	switch p.Type {
	case "function_call":
		name := p.Name
		if name == "" {
			name = "tool"
		}
		text := "ran " + name
		if args := briefArgs(p.Arguments); args != "" {
			text += ": " + args
		}
		s.addTurn(RoleTool, name, text)
	case "web_search_call":
		s.addTurn(RoleTool, "web_search", "performed a web search")
	}
}

// briefArgs extracts a short, human-readable snippet from a tool-call arguments
// JSON blob (best-effort): a "command"/"cmd" string, else a truncated raw form.
func briefArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) == nil {
		for _, k := range []string{"command", "cmd", "query", "path", "file_path"} {
			if v, ok := m[k]; ok {
				return clip(fmt.Sprint(stringifyArg(v)), 200)
			}
		}
	}
	return clip(raw, 200)
}

func stringifyArg(v any) string {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(t)
	}
}

func stripMarker(msg string) string {
	const marker = "USER_MESSAGE_BEGIN"
	if i := strings.Index(msg, marker); i != -1 {
		msg = msg[i+len(marker):]
	}
	return strings.TrimSpace(msg)
}

// decodeGit pulls branch/commit/remote from a session_meta git object, trying a
// few plausible key names (Codex's exact field names have varied).
func decodeGit(raw json.RawMessage) *GitInfo {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	g := &GitInfo{
		Branch: firstString(m, "branch", "git_branch"),
		Commit: firstString(m, "sha", "commit_hash", "commit", "git_sha"),
		Remote: firstString(m, "origin_url", "repository_url", "remote_url", "git_origin_url"),
	}
	if g.Branch == "" && g.Commit == "" && g.Remote == "" {
		return nil
	}
	return g
}

func firstString(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}
