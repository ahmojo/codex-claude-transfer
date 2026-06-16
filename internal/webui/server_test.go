package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

const testToken = "testtoken123"

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	home, err := codexhome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	s := &Server{home: home, token: testToken, out: io.Discard}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func writeSession(t *testing.T, home codexhome.Home, id, cwd string) {
	t.Helper()
	dir := filepath.Join(home.SessionsDir, "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","source":"cli"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+id+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func do(t *testing.T, ts *httptest.Server, method, path, token, body string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Cct-Token", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

func TestTokenGating(t *testing.T) {
	_, ts := testServer(t)

	res, _ := do(t, ts, "GET", "/api/doctor", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", res.StatusCode)
	}
	res, _ = do(t, ts, "GET", "/api/doctor", "wrong", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", res.StatusCode)
	}
	res, _ = do(t, ts, "GET", "/api/doctor", testToken, "")
	if res.StatusCode != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", res.StatusCode)
	}
}

func TestRejectsNonLoopbackHost(t *testing.T) {
	_, ts := testServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/doctor", nil)
	req.Header.Set("X-Cct-Token", testToken)
	req.Host = "evil.example.com"
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("non-loopback Host: got %d, want 403", res.StatusCode)
	}
}

func TestServesIndex(t *testing.T) {
	_, ts := testServer(t)
	res, data := do(t, ts, "GET", "/", "", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("index: got %d", res.StatusCode)
	}
	if !strings.Contains(string(data), "<title>cct</title>") {
		t.Errorf("index.html not served")
	}
	// Static assets serve too.
	if res, _ := do(t, ts, "GET", "/app.js", "", ""); res.StatusCode != http.StatusOK {
		t.Errorf("app.js: got %d", res.StatusCode)
	}
}

func TestSessionsEndpoint(t *testing.T) {
	s, ts := testServer(t)
	writeSession(t, s.home, "abcd1234-1111-2222-3333-444455556666", "/work/p")
	res, data := do(t, ts, "GET", "/api/sessions", testToken, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sessions: got %d (%s)", res.StatusCode, data)
	}
	var out struct {
		Count    int `json:"count"`
		Sessions []struct {
			ThreadID string `json:"thread_id"`
			Preview  string `json:"preview"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("bad json: %v\n%s", err, data)
	}
	if out.Count != 1 || len(out.Sessions) != 1 || out.Sessions[0].Preview != "hi" {
		t.Errorf("unexpected sessions: %+v", out)
	}
}

func TestExportInspectImportRoundTrip(t *testing.T) {
	src, srcTS := testServer(t)
	writeSession(t, src.home, "abcd1234-1111-2222-3333-444455556666", "/work/p")

	out := filepath.ToSlash(filepath.Join(t.TempDir(), "b.codexbundle"))

	// Export everything.
	res, data := do(t, srcTS, "POST", "/api/export", testToken, `{"mode":"all","output":"`+out+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", res.StatusCode, data)
	}
	if _, err := os.Stat(filepath.FromSlash(out)); err != nil {
		t.Fatalf("bundle not written: %v", err)
	}

	// Inspect it.
	res, data = do(t, srcTS, "POST", "/api/inspect", testToken, `{"path":"`+out+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("inspect: %d %s", res.StatusCode, data)
	}

	// Import into a second home (dry-run then real).
	dst, dstTS := testServer(t)
	res, data = do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+out+`","dry_run":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import dry-run: %d %s", res.StatusCode, data)
	}
	res, data = do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+out+`","dry_run":false}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import: %d %s", res.StatusCode, data)
	}
	var ir struct {
		Imported int `json:"imported"`
	}
	json.Unmarshal(data, &ir)
	if ir.Imported != 1 {
		t.Errorf("imported = %d, want 1", ir.Imported)
	}
	if _, err := os.Stat(filepath.Join(dst.home.SessionsDir, "2026", "06", "13", "rollout-2026-06-13T18-22-01-abcd1234-1111-2222-3333-444455556666.jsonl")); err != nil {
		t.Errorf("imported file missing: %v", err)
	}
}

func TestImportConflictingResolversRejected(t *testing.T) {
	_, ts := testServer(t)
	res, _ := do(t, ts, "POST", "/api/import", testToken,
		`{"path":"x.codexbundle","replace_with_backup":true,"import_as_copy":true}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for both resolvers, got %d", res.StatusCode)
	}
}
