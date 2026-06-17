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

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

const testToken = "testtoken123"

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	home, err := codexhome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	clHome, err := claudehome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect claude: %v", err)
	}
	s := &Server{home: home, claudeHome: clHome, token: testToken, out: io.Discard}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts
}

// appendLine appends one JSONL line to an existing session file, simulating a
// session that grew (more turns) on the source device.
func appendLine(t *testing.T, home codexhome.Home, id, line string) {
	t.Helper()
	p := filepath.Join(home.SessionsDir, "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
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

const sessID = "abcd1234-1111-2222-3333-444455556666"

// exportAll exports every session from the given server to a fresh bundle path.
func exportAll(t *testing.T, ts *httptest.Server, name string) string {
	t.Helper()
	out := filepath.ToSlash(filepath.Join(t.TempDir(), name))
	res, data := do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","output":"`+out+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export %s: %d %s", name, res.StatusCode, data)
	}
	return out
}

// TestImportMergeViaAPI exercises the headline feature over HTTP: a session that
// grew on the source is a conflict without merge, and is appended-to with merge.
func TestImportMergeViaAPI(t *testing.T) {
	src, srcTS := testServer(t)
	writeSession(t, src.home, sessID, "/work/p")
	b1 := exportAll(t, srcTS, "b1.codexbundle")

	dst, dstTS := testServer(t)
	if res, data := do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+b1+`","dry_run":false}`); res.StatusCode != http.StatusOK {
		t.Fatalf("seed import: %d %s", res.StatusCode, data)
	}
	_ = dst

	// The session grows on the source, then is re-exported.
	appendLine(t, src.home, sessID, `{"type":"event_msg","payload":{"type":"user_message","message":"a later question"}}`)
	b2 := exportAll(t, srcTS, "b2.codexbundle")

	// Without merge: reported as a conflict, nothing written.
	_, data := do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+b2+`","dry_run":false}`)
	var noMerge struct {
		Conflicts int `json:"conflicts"`
		Updated   int `json:"updated"`
	}
	json.Unmarshal(data, &noMerge)
	if noMerge.Conflicts != 1 || noMerge.Updated != 0 {
		t.Fatalf("without merge: conflicts=%d updated=%d (%s)", noMerge.Conflicts, noMerge.Updated, data)
	}

	// With merge: the new line is appended in place.
	_, data = do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+b2+`","dry_run":false,"merge":true}`)
	var merged struct {
		Updated    int `json:"updated"`
		LinesAdded int `json:"lines_added"`
		Conflicts  int `json:"conflicts"`
	}
	json.Unmarshal(data, &merged)
	if merged.Updated != 1 || merged.LinesAdded != 1 || merged.Conflicts != 0 {
		t.Fatalf("with merge: updated=%d lines=%d conflicts=%d (%s)", merged.Updated, merged.LinesAdded, merged.Conflicts, data)
	}
}

// TestExportSessionMode exports a single session by id prefix.
func TestExportSessionMode(t *testing.T) {
	src, ts := testServer(t)
	writeSession(t, src.home, sessID, "/work/p")
	out := filepath.ToSlash(filepath.Join(t.TempDir(), "one.codexbundle"))
	res, data := do(t, ts, "POST", "/api/export", testToken, `{"mode":"session","session":"abcd1234","output":"`+out+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export session: %d %s", res.StatusCode, data)
	}
	var out1 struct {
		Included int `json:"included"`
	}
	json.Unmarshal(data, &out1)
	if out1.Included != 1 {
		t.Errorf("included = %d, want 1 (%s)", out1.Included, data)
	}

	// A missing session id is a 400, not a silent empty bundle.
	if res, _ := do(t, ts, "POST", "/api/export", testToken, `{"mode":"session","session":""}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("empty session id: got %d, want 400", res.StatusCode)
	}
}

// TestExportSinceAccepted confirms the --since grammar is wired (a bad value 400s).
func TestExportSinceAccepted(t *testing.T) {
	src, ts := testServer(t)
	writeSession(t, src.home, sessID, "/work/p")
	out := filepath.ToSlash(filepath.Join(t.TempDir(), "s.codexbundle"))
	if res, data := do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","since":"7d","output":"`+out+`"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("since 7d: %d %s", res.StatusCode, data)
	}
	if res, _ := do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","since":"not-a-date"}`); res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad since: want 400")
	}
}

// TestTranslateViaAPI does a cross-agent handoff (Codex bundle -> Claude) dry-run.
func TestTranslateViaAPI(t *testing.T) {
	src, srcTS := testServer(t)
	writeSession(t, src.home, sessID, "/work/p")
	b := exportAll(t, srcTS, "codex.codexbundle")

	_, dstTS := testServer(t)
	res, data := do(t, dstTS, "POST", "/api/import", testToken, `{"path":"`+b+`","translate_to":"claude","dry_run":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("translate: %d %s", res.StatusCode, data)
	}
	var tr struct {
		Translated bool   `json:"translated"`
		Written    int    `json:"written"`
		Target     string `json:"target_tool"`
	}
	json.Unmarshal(data, &tr)
	if !tr.Translated || tr.Written != 1 {
		t.Fatalf("translate result: %s", data)
	}
}

// TestEncryptedBundleNeedsIdentity: a .age bundle without an identity is rejected
// up front (the browser cannot do passphrase prompts), for both import and inspect.
func TestEncryptedBundleNeedsIdentity(t *testing.T) {
	_, ts := testServer(t)
	for _, path := range []string{"/api/import", "/api/inspect"} {
		res, data := do(t, ts, "POST", path, testToken, `{"path":"secret.codexbundle.age"}`)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s without identity: got %d, want 400 (%s)", path, res.StatusCode, data)
		}
	}
}
