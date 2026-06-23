package handoff

import (
	"strings"
	"testing"
)

func TestToHTMLStructureAndEscaping(t *testing.T) {
	s := AgentSession{
		SourceAgent: "codex",
		CWD:         "/home/u/proj",
		Conversation: []Turn{
			{Role: RoleUser, Text: "fix <script>alert(1)</script> please"},
			{Role: RoleAssistant, Text: "done"},
			{Role: RoleTool, Tool: "shell", Text: "echo hi && ls"},
		},
	}
	out := string(ToHTML(s))

	if !strings.HasPrefix(out, "<!doctype html>") {
		t.Fatal("missing doctype")
	}
	if !strings.Contains(out, "<title>") || !strings.Contains(out, "</html>") {
		t.Fatal("missing document shell")
	}
	// Untrusted session text must be escaped, never rendered as live markup.
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatal("unescaped script tag leaked into HTML")
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("expected escaped script text")
	}
	// Tool output rendered in a <pre> block.
	if !strings.Contains(out, "<pre>echo hi &amp;&amp; ls</pre>") {
		t.Fatalf("tool block not rendered as expected:\n%s", out)
	}
	// Roles present.
	for _, want := range []string{"🧑 User", "🤖 Assistant", "🔧 shell", "/home/u/proj"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestToHTMLEmptyConversation(t *testing.T) {
	out := string(ToHTML(AgentSession{SourceAgent: "claude"}))
	if !strings.Contains(out, "<title>Session</title>") {
		t.Fatal("empty session should fall back to 'Session' title")
	}
}
