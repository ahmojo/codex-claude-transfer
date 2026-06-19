package handoff

import (
	"strings"
	"testing"
)

// TestPreambleSanitizesMetadata: attacker-controlled cwd/git metadata in a bundle
// must not carry terminal escape sequences into the generated handoff text.
func TestPreambleSanitizesMetadata(t *testing.T) {
	s := AgentSession{
		SourceAgent: "codex",
		CreatedAt:   "2026-01-01\x1b]52;c;ZXZpbA==\x07",
		CWD:         "/proj\x1b[2J/evil",
		Git: &GitInfo{
			Branch: "main\x1b[31m",
			Commit: "deadbeef\x07",
			Remote: "https://h/r\x1b]8;;http://evil\x07",
		},
		Conversation: []Turn{{Role: RoleUser, Text: "hello"}},
	}
	out := Preamble(s)
	for _, bad := range []rune{0x1b, 0x07} {
		if strings.ContainsRune(out, bad) {
			t.Errorf("preamble still contains control byte %#x: %q", bad, out)
		}
	}
}

func TestSanitizeMeta(t *testing.T) {
	if got := sanitizeMeta("clean/path"); got != "clean/path" {
		t.Errorf("clean value changed: %q", got)
	}
	if got := sanitizeMeta("a\x1bb\x07c\n"); strings.ContainsAny(got, "\x1b\x07\n") {
		t.Errorf("controls survived: %q", got)
	}
	if got := sanitizeMeta("a\tb"); got != "a b" {
		t.Errorf("tab not normalized: %q", got)
	}
}
