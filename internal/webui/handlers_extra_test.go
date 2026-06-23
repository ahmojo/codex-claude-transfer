package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// writeMsg writes a Codex session with a custom first user message and cwd.
func writeMsg(t *testing.T, home codexhome.Home, id, cwd, msg string) {
	t.Helper()
	dir := filepath.Join(home.SessionsDir, "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":` + string(cwdJSON) + `,"source":"cli"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"` + msg + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+id+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return m
}

func TestStatsAPI(t *testing.T) {
	s, ts := testServer(t)
	writeMsg(t, s.home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a", "one")
	writeMsg(t, s.home, "bbbb1111-2222-3333-4444-555566667777", "/proj/a", "two")
	res, data := do(t, ts, "GET", "/api/stats", testToken, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stats status %d: %s", res.StatusCode, data)
	}
	m := decode(t, data)
	if m["total"].(float64) != 2 {
		t.Fatalf("total = %v", m["total"])
	}
	if len(m["projects"].([]any)) != 1 {
		t.Fatalf("projects = %v", m["projects"])
	}
}

func TestSearchAPI(t *testing.T) {
	s, ts := testServer(t)
	writeMsg(t, s.home, "aaaa1111-2222-3333-4444-555566667777", "/p", "how to build a rate limiter")
	writeMsg(t, s.home, "bbbb1111-2222-3333-4444-555566667777", "/p", "unrelated topic")
	res, data := do(t, ts, "POST", "/api/search", testToken, `{"query":"rate limiter"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("search status %d: %s", res.StatusCode, data)
	}
	m := decode(t, data)
	if m["count"].(float64) != 1 {
		t.Fatalf("count = %v (%s)", m["count"], data)
	}
	first := m["matches"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(first["thread_id"].(string), "aaaa1111") {
		t.Fatalf("wrong match: %v", first)
	}
}

func TestScanAPI(t *testing.T) {
	s, ts := testServer(t)
	writeMsg(t, s.home, "aaaa1111-2222-3333-4444-555566667777", "/p", "deploy with AKIAIOSFODNN7EXAMPLE")
	res, data := do(t, ts, "GET", "/api/scan", testToken, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("scan status %d: %s", res.StatusCode, data)
	}
	m := decode(t, data)
	if m["secret_count"].(float64) < 1 {
		t.Fatalf("secret_count = %v", m["secret_count"])
	}
}

func TestResumeAPI(t *testing.T) {
	s, ts := testServer(t)
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeMsg(t, s.home, id, "/p", "hi")
	res, data := do(t, ts, "POST", "/api/resume", testToken, `{"session":"aaaa1111"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("resume status %d: %s", res.StatusCode, data)
	}
	m := decode(t, data)
	if m["command"].(string) != "codex resume "+id {
		t.Fatalf("command = %v", m["command"])
	}
}

func TestTagsAPI(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	_, ts := testServer(t)
	id := "aaaa1111-2222-3333-4444-555566667777"
	res, data := do(t, ts, "POST", "/api/tags", testToken, `{"session":"`+id+`","add_tags":["WIP"],"set_name":"my chat"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tags status %d: %s", res.StatusCode, data)
	}
	m := decode(t, data)
	if m["name"].(string) != "my chat" {
		t.Fatalf("name = %v", m["name"])
	}
	tags := m["tags"].([]any)
	if len(tags) != 1 || tags[0].(string) != "wip" {
		t.Fatalf("tags = %v", tags)
	}
}

func TestExportSecretGateAPI(t *testing.T) {
	s, ts := testServer(t)
	writeMsg(t, s.home, "aaaa1111-2222-3333-4444-555566667777", "/p", "deploy with AKIAIOSFODNN7EXAMPLE")
	out := filepath.Join(t.TempDir(), "b.codexbundle")
	outJSON, _ := json.Marshal(out)

	// Default: blocked with the structured secret-gate response.
	res, data := do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","output":`+string(outJSON)+`}`)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", res.StatusCode, data)
	}
	if decode(t, data)["secrets_blocked"] != true {
		t.Fatalf("missing secrets_blocked flag: %s", data)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("blocked export left a bundle behind")
	}

	// allow_secrets proceeds.
	res, data = do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","output":`+string(outJSON)+`,"allow_secrets":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("allow_secrets status %d: %s", res.StatusCode, data)
	}

	// redact proceeds and reports a redaction.
	out2 := filepath.Join(t.TempDir(), "b2.codexbundle")
	out2JSON, _ := json.Marshal(out2)
	res, data = do(t, ts, "POST", "/api/export", testToken, `{"mode":"all","output":`+string(out2JSON)+`,"redact":true}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("redact status %d: %s", res.StatusCode, data)
	}
	if decode(t, data)["secrets_redacted"].(float64) < 1 {
		t.Fatalf("expected secrets_redacted >= 1: %s", data)
	}
}
