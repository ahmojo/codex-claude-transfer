package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
)

func TestResumeClaudePreservesPlanMode(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	home, cwd := t.TempDir(), t.TempDir()
	const id = "aaaa1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, cwd, id, "Plan this work")
	file := filepath.Join(home, "projects", claudehome.EncodeCWD(cwd), id+".jsonl")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"plan", "default", "bypassPermissions", "future-mode"} {
		data := append(append([]byte(nil), original...), []byte(`{"type":"permission-mode","permissionMode":"`+mode+`"}`+"\n")...)
		// Sidechain modes must not change the parent's launch permissions.
		data = append(data, []byte(`{"type":"permission-mode","permissionMode":"plan","isSidechain":true}`+"\n")...)
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		if code := Run([]string{"resume", id, "--tool", "claude", "--claude-home", home}, &out, &errs); code != 0 {
			t.Fatalf("resume: %d %s", code, errs.String())
		}
		if got := strings.Contains(out.String(), "--permission-mode plan"); got != (mode == "plan") {
			t.Fatalf("mode %s: %s", mode, out.String())
		}
	}
}

func TestClaudeResumeStateAfterOversizedRecord(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"type":"user","permissionMode":"plan"}` + "\n" +
		`{"type":"assistant","message":"` + strings.Repeat("x", 4*1024*1024) + `"}` + "\n" +
		`{"type":"permission-mode","permissionMode":"default"}` + "\n"
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if plan, err := claudesessions.ResumePlanMode(file); err != nil || plan {
		t.Fatalf("latest state lost after a large record: plan=%v, err=%v", plan, err)
	}
}
