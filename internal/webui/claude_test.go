package webui

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// claudeTestServer builds a server with both a Codex home and a Claude home, plus
// one Claude transcript, so the ?tool=claude path can be exercised end-to-end.
func claudeTestServer(t *testing.T) (*Server, *httptest.Server, claudehome.Home) {
	t.Helper()
	codexHome, _ := codexhome.Detect(t.TempDir())
	clHome, _ := claudehome.Detect(t.TempDir())
	cwd := `C:\Users\example\dev\proj`
	dir := filepath.Join(clHome.ProjectsDir, claudehome.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"type":"user","sessionId":"abcd1234-1111-2222-3333-444455556666","cwd":` + string(cwdJSON) +
		`,"version":"2.1.170","timestamp":"2026-06-16T10:00:00Z","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "abcd1234-1111-2222-3333-444455556666.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{home: codexHome, claudeHome: clHome, token: testToken, out: io.Discard}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts, clHome
}

func TestClaudeDoctorAndSessionsEndpoints(t *testing.T) {
	_, ts, _ := claudeTestServer(t)

	res, data := do(t, ts, "GET", "/api/doctor?tool=claude", testToken, "")
	if res.StatusCode != 200 {
		t.Fatalf("doctor status %d: %s", res.StatusCode, data)
	}
	if !strings.Contains(string(data), "Claude Code home found") {
		t.Errorf("doctor did not report Claude home: %s", data)
	}

	res, data = do(t, ts, "GET", "/api/sessions?tool=claude", testToken, "")
	if res.StatusCode != 200 {
		t.Fatalf("sessions status %d: %s", res.StatusCode, data)
	}
	var sess struct {
		Count    int `json:"count"`
		Sessions []struct {
			ThreadID string `json:"thread_id"`
			CWD      string `json:"cwd"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sess.Count != 1 || sess.Sessions[0].ThreadID != "abcd1234-1111-2222-3333-444455556666" {
		t.Fatalf("unexpected sessions payload: %s", data)
	}

	// Codex view is independent and empty.
	_, data = do(t, ts, "GET", "/api/sessions?tool=codex", testToken, "")
	var codex struct {
		Count int `json:"count"`
	}
	json.Unmarshal(data, &codex)
	if codex.Count != 0 {
		t.Errorf("codex sessions = %d, want 0", codex.Count)
	}
}

func TestClaudeExportImportEndpoints(t *testing.T) {
	_, ts, clHome := claudeTestServer(t)
	out := filepath.Join(t.TempDir(), "c.codexbundle")

	body, _ := json.Marshal(map[string]any{
		"mode": "all", "tool": "claude", "output": out,
	})
	res, data := do(t, ts, "POST", "/api/export", testToken, string(body))
	if res.StatusCode != 200 {
		t.Fatalf("export status %d: %s", res.StatusCode, data)
	}

	// Remove the source transcript so the import actually writes (otherwise the
	// round-trip into the same home would be an idempotent skip).
	srcPath := filepath.Join(clHome.ProjectsDir, claudehome.EncodeCWD(`C:\Users\example\dev\proj`), "abcd1234-1111-2222-3333-444455556666.jsonl")
	if err := os.Remove(srcPath); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	// Import the Claude bundle: the server must route it to the Claude home even
	// though the request names no tool (the bundle's manifest decides).
	ibody, _ := json.Marshal(map[string]any{"path": out, "dry_run": false})
	res, data = do(t, ts, "POST", "/api/import", testToken, string(ibody))
	if res.StatusCode != 200 {
		t.Fatalf("import status %d: %s", res.StatusCode, data)
	}
	var ir struct {
		Imported int `json:"imported"`
	}
	json.Unmarshal(data, &ir)
	if ir.Imported != 1 {
		t.Fatalf("imported = %d, want 1", ir.Imported)
	}
	got := filepath.Join(clHome.ProjectsDir, claudehome.EncodeCWD(`C:\Users\example\dev\proj`), "abcd1234-1111-2222-3333-444455556666.jsonl")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("transcript not imported into Claude home: %v", err)
	}
}
