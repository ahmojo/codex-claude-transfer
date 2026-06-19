package handoff

import (
	"fmt"
	"strings"
	"time"
)

// baseTime returns a deterministic base timestamp for a synthesized session,
// derived from the source's recorded creation time so that translating the same
// session twice produces byte-identical output (idempotent re-import). When the
// source time is absent or unparseable, a fixed epoch is used.
func baseTime(s AgentSession) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s.CreatedAt); err == nil {
			return t.UTC()
		}
	}
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
}

// sourceLabel is a human name for the agent a session was translated from.
func sourceLabel(agent string) string {
	switch agent {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	default:
		return agent
	}
}

// sanitizeMeta strips control characters (C0/C1, including ESC, CR, and LF) from a
// structured metadata value before it is embedded into the generated handoff
// preamble. These fields (cwd, git branch/commit/remote, timestamp) come from an
// untrusted bundle and must never carry terminal escape/OSC sequences into the
// session text the destination agent later displays. The conversation messages
// themselves are preserved verbatim (faithful translation); only cct's own
// formatted metadata is cleaned. See Finding 5 in docs/security/audit.md.
func sanitizeMeta(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// Preamble builds the plain-language handoff message that leads a translated
// session. It tells the destination agent that this is an imported, best-effort
// translation (not a native replay), restates the project/git context so the code
// can be recovered, and asks it to continue from where the prior conversation left
// off. It is emitted as the first user turn of the synthesized session.
func Preamble(s AgentSession) string {
	var b strings.Builder
	b.WriteString("[Session handed off by cct — translated from ")
	b.WriteString(sourceLabel(s.SourceAgent))
	if s.CreatedAt != "" {
		fmt.Fprintf(&b, " (originally %s)", sanitizeMeta(s.CreatedAt))
	}
	b.WriteString("]\n\n")
	b.WriteString("This conversation was carried over from another coding agent. It is a ")
	b.WriteString("best-effort text translation: prior user and assistant messages are preserved, ")
	b.WriteString("but tool calls and command output are summarized rather than replayed, and no ")
	b.WriteString("model/runtime state crossed over. Read it as context.\n\n")

	if s.CWD != "" {
		fmt.Fprintf(&b, "Project directory: %s\n", sanitizeMeta(s.CWD))
	}
	if s.Git != nil {
		if s.Git.Branch != "" {
			fmt.Fprintf(&b, "Git branch: %s\n", sanitizeMeta(s.Git.Branch))
		}
		if s.Git.Commit != "" {
			fmt.Fprintf(&b, "Git commit: %s\n", sanitizeMeta(s.Git.Commit))
		}
		if s.Git.Remote != "" {
			fmt.Fprintf(&b, "Git remote: %s\n", sanitizeMeta(s.Git.Remote))
			if s.Git.Commit != "" {
				fmt.Fprintf(&b, "  (to get the code: git clone %s <dir> && cd <dir> && git checkout %s)\n", sanitizeMeta(s.Git.Remote), sanitizeMeta(s.Git.Commit))
			}
		}
	}
	b.WriteString("\nThe previous conversation follows. Please continue from where it left off.")
	return b.String()
}

// turnsWithPreamble returns the conversation with the handoff preamble prepended
// as the first user turn, so both synthesizers share identical ordering.
func turnsWithPreamble(s AgentSession) []Turn {
	out := make([]Turn, 0, len(s.Conversation)+1)
	out = append(out, Turn{Role: RoleUser, Text: Preamble(s)})
	out = append(out, s.Conversation...)
	return out
}

// renderTurnText is the visible text for a turn. Tool turns are prefixed so they
// read as a note about an action rather than as the assistant's own prose.
func renderTurnText(t Turn) string {
	if t.Role == RoleTool {
		return "🔧 " + t.Text
	}
	return t.Text
}
