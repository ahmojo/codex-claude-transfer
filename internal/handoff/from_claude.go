package handoff

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// claudeLine is the subset of a Claude transcript line we read for translation.
type claudeLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	CWD       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

// FromClaudeTranscript reads a Claude Code transcript and extracts the neutral
// AgentSession: user/assistant prose, with tool_use blocks summarized to short
// text turns and internal "thinking" blocks dropped. Project context comes from
// the per-line cwd/gitBranch. Parsing is defensive.
func FromClaudeTranscript(path string) (AgentSession, error) {
	f, err := os.Open(path)
	if err != nil {
		return AgentSession{}, err
	}
	defer f.Close()
	return fromClaudeReader(f)
}

// FromClaudeBytes extracts the neutral AgentSession from in-memory Claude
// transcript bytes (e.g. a bundle entry).
func FromClaudeBytes(b []byte) (AgentSession, error) {
	return fromClaudeReader(bytes.NewReader(b))
}

func fromClaudeReader(r io.Reader) (AgentSession, error) {
	s := AgentSession{Format: IRFormat, SourceAgent: "claude"}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cl claudeLine
		if json.Unmarshal([]byte(line), &cl) != nil {
			continue
		}
		if s.ThreadID == "" && cl.SessionID != "" {
			s.ThreadID = cl.SessionID
		}
		if s.CWD == "" && cl.CWD != "" {
			s.CWD = cl.CWD
		}
		if s.CreatedAt == "" && cl.Timestamp != "" {
			s.CreatedAt = cl.Timestamp
		}
		if s.Git == nil && cl.GitBranch != "" && cl.GitBranch != "HEAD" {
			s.Git = &GitInfo{Branch: cl.GitBranch}
		}
		switch cl.Type {
		case "user":
			s.addTurn(RoleUser, "", claudeUserText(cl.Message))
		case "assistant":
			consumeClaudeAssistant(&s, cl.Message)
		}
	}
	return s, sc.Err()
}

// claudeUserText extracts text from a user message, whose content is a plain
// string or an array of content blocks. tool_result blocks (the user "turn" that
// carries a tool's output) are ignored — the tool call itself was already
// summarized on the assistant side.
func claudeUserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(msg.Content, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, strings.TrimSpace(b.Text))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// consumeClaudeAssistant turns an assistant message into one prose turn (joined
// text blocks) plus a tool turn for each tool_use block. "thinking" blocks are
// internal and dropped.
func consumeClaudeAssistant(s *AgentSession, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	// content may be a plain string (rare for assistant) or an array of blocks.
	var str string
	if json.Unmarshal(msg.Content, &str) == nil {
		s.addTurn(RoleAssistant, "", strings.TrimSpace(str))
		return
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return
	}
	var prose []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				prose = append(prose, strings.TrimSpace(b.Text))
			}
		case "tool_use":
			name := b.Name
			if name == "" {
				name = "tool"
			}
			text := "ran " + name
			if brief := briefClaudeInput(b.Input); brief != "" {
				text += ": " + brief
			}
			// Flush any pending prose before the tool turn to preserve order.
			if len(prose) > 0 {
				s.addTurn(RoleAssistant, "", strings.Join(prose, "\n"))
				prose = nil
			}
			s.addTurn(RoleTool, name, text)
		}
	}
	if len(prose) > 0 {
		s.addTurn(RoleAssistant, "", strings.Join(prose, "\n"))
	}
}

// briefClaudeInput pulls a short snippet from a tool_use input object.
func briefClaudeInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "cmd", "file_path", "path", "pattern", "query", "description"} {
		if v, ok := m[k]; ok {
			return clip(stringifyArg(v), 200)
		}
	}
	return ""
}
