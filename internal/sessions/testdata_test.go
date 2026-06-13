package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// validRolloutLines builds a minimal but realistic plain rollout file body with
// a SessionMeta line and a first user message event.
func validRolloutLines(threadID, cwd string) string {
	return `{"timestamp":"2026-06-13T18:22:01Z","type":"session_meta","payload":{"id":"` + threadID + `","timestamp":"2026-06-13T18:22:00Z","cwd":"` + cwd + `","originator":"codex_cli","cli_version":"1.2.3","source":"cli","model_provider":"openai","git":{"branch":"main","sha":"abc123","origin_url":"https://example.com/repo.git"}}}
{"timestamp":"2026-06-13T18:22:02Z","type":"event_msg","payload":{"type":"user_message","message":"Starter terminal implementation"}}
{"timestamp":"2026-06-13T18:22:03Z","type":"response_item","payload":{"type":"message","role":"assistant"}}
`
}

// writeRollout writes a rollout file at sessions/YYYY/MM/DD/<name> under home,
// creating directories as needed, and returns its absolute path.
func writeRollout(t *testing.T, root, datePath, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, datePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return full
}
