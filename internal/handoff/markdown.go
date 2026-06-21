package handoff

import (
	"fmt"
	"strings"
)

// ToMarkdown renders a parsed session as a human-readable Markdown document, for
// reading or sharing a conversation outside the agent (it is NOT re-importable).
// Tool/command turns are shown as fenced blocks; metadata is sanitized.
func ToMarkdown(s AgentSession) []byte {
	var b strings.Builder

	title := strings.TrimSpace(firstUserLine(s))
	if title == "" {
		title = "Session"
	}
	fmt.Fprintf(&b, "# %s\n\n", sanitizeMeta(title))

	meta := func(label, val string) {
		if strings.TrimSpace(val) != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, sanitizeMeta(val))
		}
	}
	meta("Agent", s.SourceAgent)
	meta("Project", s.CWD)
	meta("Created", s.CreatedAt)
	if s.Git != nil {
		meta("Git branch", s.Git.Branch)
		meta("Git commit", s.Git.Commit)
	}
	b.WriteString("\n---\n\n")

	for _, t := range s.Conversation {
		switch t.Role {
		case RoleUser:
			b.WriteString("## 🧑 User\n\n")
			b.WriteString(mdText(t.Text))
		case RoleAssistant:
			b.WriteString("## 🤖 Assistant\n\n")
			b.WriteString(mdText(t.Text))
		case RoleTool:
			name := t.Tool
			if name == "" {
				name = "tool"
			}
			fmt.Fprintf(&b, "### 🔧 %s\n\n", sanitizeMeta(name))
			fmt.Fprintf(&b, "```\n%s\n```\n", fenceSafe(t.Text))
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// firstUserLine returns the first user turn's text (a reasonable title).
func firstUserLine(s AgentSession) string {
	for _, t := range s.Conversation {
		if t.Role == RoleUser && strings.TrimSpace(t.Text) != "" {
			line := strings.SplitN(strings.TrimSpace(t.Text), "\n", 2)[0]
			if len(line) > 80 {
				line = line[:80] + "…"
			}
			return line
		}
	}
	return ""
}

// mdText emits conversation text as a paragraph, trailing with a blank line. The
// text is preserved verbatim (it is the user's own content), only ensuring it ends
// cleanly.
func mdText(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		s = "_(empty)_"
	}
	return s + "\n"
}

// fenceSafe prevents a tool block from breaking out of its code fence by neutering
// any embedded triple-backtick.
func fenceSafe(s string) string {
	return strings.ReplaceAll(s, "```", "ʼʼʼ")
}
