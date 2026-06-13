package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSession(t *testing.T, home, threadID, cwd string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":"` + cwd + `","source":"cli"}}
{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"Auth refactor"}}
`
	name := "rollout-2026-06-13T18-22-01-" + threadID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRunListWithFakeHome(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/Users/example/dev/project")

	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", home}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "Found 1 Codex session") {
		t.Errorf("missing session count: %s", s)
	}
	if !strings.Contains(s, "Auth refactor") {
		t.Errorf("missing preview title: %s", s)
	}
	if !strings.Contains(s, "/Users/example/dev/project") {
		t.Errorf("missing cwd: %s", s)
	}
}

func TestRunDoctorWithFakeHome(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "bbbb1111-2222-3333-4444-555566667777", "/x")

	var out, errOut bytes.Buffer
	code := Run([]string{"doctor", "--codex-home", home}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "SQLite will not be modified") {
		t.Errorf("doctor output missing SQLite safety line: %s", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"frobnicate"}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for unknown command, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("expected unknown command message")
	}
}

func TestRunNoArgsShowsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(nil, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Usage") {
		t.Errorf("expected usage text")
	}
}

func TestRunUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", t.TempDir(), "--bogus"}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for unknown flag, got %d", code)
	}
}

func TestRunEmptyHomeListsNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "No Codex sessions found") {
		t.Errorf("expected empty message, got: %s", out.String())
	}
}
