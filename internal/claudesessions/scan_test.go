package claudesessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
)

// writeTranscript creates a Claude Code transcript at projects/<encoded>/<uuid>.jsonl.
func writeTranscript(t *testing.T, home claudehome.Home, cwd, sessionID, firstUser string) string {
	t.Helper()
	dir := filepath.Join(home.ProjectsDir, claudehome.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	// A realistic-ish pair of lines: a user message then an assistant reply.
	body := `{"type":"user","uuid":"u1","sessionId":"` + sessionID + `","cwd":` + string(cwdJSON) +
		`,"version":"2.1.170","gitBranch":"main","timestamp":"2026-06-16T10:00:00Z","message":{"role":"user","content":"` + firstUser + `"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"` + sessionID + `","cwd":` + string(cwdJSON) +
		`,"timestamp":"2026-06-16T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}` + "\n"
	path := filepath.Join(dir, sessionID+claudehome.SessionExt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func fakeClaudeHome(t *testing.T) claudehome.Home {
	t.Helper()
	home, err := claudehome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return home
}

func TestScanRecoversMetadata(t *testing.T) {
	home := fakeClaudeHome(t)
	cwd := `C:\Users\example\dev\project`
	writeTranscript(t, home, cwd, "aaaa1111-2222-3333-4444-555566667777", "build the thing")

	scan, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Files != 1 || scan.Valid != 1 {
		t.Fatalf("files=%d valid=%d, want 1/1", scan.Files, scan.Valid)
	}
	s := scan.Sessions[0]
	if s.ThreadID != "aaaa1111-2222-3333-4444-555566667777" {
		t.Errorf("thread id = %q", s.ThreadID)
	}
	if s.CWD != cwd {
		t.Errorf("cwd = %q, want %q", s.CWD, cwd)
	}
	if s.CLIVersion != "2.1.170" {
		t.Errorf("version = %q", s.CLIVersion)
	}
	if s.GitBranch != "main" {
		t.Errorf("branch = %q", s.GitBranch)
	}
	if s.Preview != "build the thing" {
		t.Errorf("preview = %q", s.Preview)
	}
	wantRel := filepath.ToSlash(filepath.Join(claudehome.EncodeCWD(cwd), "aaaa1111-2222-3333-4444-555566667777.jsonl"))
	if s.RelPath != wantRel {
		t.Errorf("relpath = %q, want %q", s.RelPath, wantRel)
	}
}

func TestScanFallbackIDFromFilename(t *testing.T) {
	home := fakeClaudeHome(t)
	dir := filepath.Join(home.ProjectsDir, "some-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A line with cwd but no sessionId: thread id falls back to the filename.
	body := `{"type":"user","cwd":"/tmp/x","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "11112222-3333-4444-5555-666677778888.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	scan, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scan.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(scan.Sessions))
	}
	if scan.Sessions[0].ThreadID != "11112222-3333-4444-5555-666677778888" {
		t.Errorf("fallback id = %q", scan.Sessions[0].ThreadID)
	}
}

func TestScanMissingProjectsDir(t *testing.T) {
	home := fakeClaudeHome(t) // nothing created
	scan, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Files != 0 || len(scan.Sessions) != 0 {
		t.Fatalf("expected empty scan, got files=%d", scan.Files)
	}
}
