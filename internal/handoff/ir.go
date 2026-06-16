// Package handoff translates a coding-agent session from one agent's on-disk
// format into another's, via a small neutral intermediate representation (the
// AgentSession IR).
//
// This is deliberately NOT a perfect clone. Codex and Claude Code do not share
// message ids, tool-call schemas, model metadata, or runtime/permission state, so
// a faithful byte-level conversion is impossible. Instead, handoff carries the
// useful *semantic* content — the conversation (user/assistant prose, with tool
// calls and command output flattened to short text summaries) plus project
// context (cwd, git) — and the target synthesizer emits a real, discoverable
// session in the destination agent that opens with a plain-language handoff
// preamble and then replays that conversation as text turns. The result is honest:
// a translated, continue-from-here session, not a claim of native fidelity.
//
// Flow:
//
//	Codex rollout  ─┐                      ┌─► Claude transcript (ToClaude)
//	                ├─► AgentSession IR ───┤
//	Claude transcript ┘                    └─► Codex rollout (ToCodex)
package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// IRFormat is the version tag stamped into an AgentSession.
const IRFormat = "agent-session-v1"

// Role is the speaker of a conversation turn.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool is a flattened, text-summarized tool call or command (e.g. a shell
	// command and a short note about its result). It is rendered as context, never
	// replayed as a real tool call (the target agent's tools differ).
	RoleTool Role = "tool"
)

// Turn is one conversation entry in the neutral representation.
type Turn struct {
	Role Role   `json:"role"`
	Text string `json:"text"`
	// Tool is the tool/command name when Role is RoleTool (e.g. "shell").
	Tool string `json:"tool,omitempty"`
}

// GitInfo is the project's git context, carried so the handoff preamble can tell
// the destination agent how to get the same code.
type GitInfo struct {
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
	Remote string `json:"remote,omitempty"`
}

// AgentSession is the neutral, agent-independent representation of a session.
type AgentSession struct {
	Format       string   `json:"format"`       // IRFormat
	SourceAgent  string   `json:"source_agent"` // "codex" | "claude"
	ThreadID     string   `json:"thread_id,omitempty"`
	CWD          string   `json:"cwd,omitempty"`
	Git          *GitInfo `json:"git,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	Conversation []Turn   `json:"conversation"`
}

// maxTurnText bounds a single carried turn so a runaway tool dump cannot bloat the
// translated session. Long text is truncated with an explicit marker.
const maxTurnText = 4000

// addTurn appends a turn, trimming/bounding the text and skipping empties.
func (s *AgentSession) addTurn(role Role, tool, text string) {
	text = clip(text, maxTurnText)
	if text == "" {
		return
	}
	s.Conversation = append(s.Conversation, Turn{Role: role, Tool: tool, Text: text})
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Truncate on a rune boundary near max.
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + " …[truncated]"
}

// DeterministicID derives a stable target session id from a source id and the
// target agent, so translating the same source twice yields the same destination
// session (idempotent re-import) instead of piling up duplicates. It is formatted
// as a UUID (version nibble 5) computed from a SHA-256 of the inputs.
func DeterministicID(sourceID, targetAgent string) string {
	sum := sha256.Sum256([]byte("cct-handoff:" + targetAgent + ":" + sourceID))
	var b [16]byte
	copy(b[:], sum[:16])
	b[6] = (b[6] & 0x0f) | 0x50 // version 5-style
	b[8] = (b[8] & 0x3f) | 0x80 // RFC-4122 variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}
