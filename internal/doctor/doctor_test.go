package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
)

func fakeHome(t *testing.T) codexhome.Home {
	t.Helper()
	home, err := codexhome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return home
}

func writeRollout(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func sessionBody(threadID, cwd string) string {
	// JSON-encode the cwd so Windows paths (with backslashes) are escaped
	// correctly, matching how Codex's serde serializer writes them.
	cwdJSON, _ := json.Marshal(cwd)
	return `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":` + string(cwdJSON) + `,"source":"cli"}}
{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"hi"}}
`
}

func hasMessage(report Report, status Status, substr string) bool {
	for _, c := range report.Checks {
		if c.Status == status && strings.Contains(c.Message, substr) {
			return true
		}
	}
	return false
}

func TestDoctorReportsCounts(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeRollout(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		sessionBody("aaaa1111-2222-3333-4444-555566667777", t.TempDir()))

	report := Run(home)
	if !hasMessage(report, StatusOK, "Codex home found") {
		t.Errorf("expected codex home found check")
	}
	if !hasMessage(report, StatusOK, "1 rollout files detected") {
		t.Errorf("expected 1 rollout file detected")
	}
	if !hasMessage(report, StatusOK, "1 valid sessions") {
		t.Errorf("expected 1 valid session")
	}
	if !hasMessage(report, StatusOK, "SQLite will not be modified") {
		t.Errorf("expected SQLite safety check")
	}
}

func TestDoctorWarnsOnMissingCwd(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	// cwd points at a path that does not exist on this device.
	writeRollout(t, day, "rollout-2026-06-13T18-22-01-bbbb1111-2222-3333-4444-555566667777.jsonl",
		sessionBody("bbbb1111-2222-3333-4444-555566667777", "/no/such/path/on/this/device"))

	report := Run(home)
	if !hasMessage(report, StatusWarn, "cwd paths that do not exist") {
		t.Errorf("expected cwd-mismatch warning")
	}
}

func TestDoctorWarnsOnMissingHome(t *testing.T) {
	// Point at a non-existent directory inside a temp dir.
	home, err := codexhome.Detect(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	report := Run(home)
	if !hasMessage(report, StatusWarn, "Codex home not found") {
		t.Errorf("expected missing-home warning")
	}
}

func TestDoctorReportsOptionalTools(t *testing.T) {
	home := fakeHome(t)
	report := Run(home)
	// Each optional tool must be reported exactly once, as either "found" (ok) or
	// "not found" (info) depending on the environment — never silently omitted.
	for _, name := range []string{"git", "age", "zstd"} {
		want := "Optional tool '" + name + "'"
		var seen int
		for _, c := range report.Checks {
			if strings.Contains(c.Message, want) {
				seen++
			}
		}
		if seen != 1 {
			t.Errorf("expected exactly one report line for %q, got %d", name, seen)
		}
	}
}

func TestDoctorCompressedDetected(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeRollout(t, day, "rollout-2026-06-13T18-22-01-cccc1111-2222-3333-4444-555566667777.jsonl.zst",
		"compressed bytes")
	report := Run(home)
	if !hasMessage(report, StatusInfo, "compressed (.jsonl.zst)") {
		t.Errorf("expected compressed-detected info line")
	}
}
