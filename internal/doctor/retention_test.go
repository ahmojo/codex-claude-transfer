package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
)

// claudeHomeWithTranscript builds a fake Claude Code home holding one transcript
// whose modification time is `ageDays` old, plus an optional settings.json.
func claudeHomeWithTranscript(t *testing.T, ageDays int, settings string) claudehome.Home {
	t.Helper()
	root := filepath.Join(t.TempDir(), "claude")
	cwd := filepath.Join(root, "proj")
	dir := filepath.Join(root, claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(map[string]any{
		"type": "user", "uuid": "aaaa1111-2222-3333-4444-555566667777",
		"sessionId": "aaaa1111-2222-3333-4444-555566667777", "cwd": cwd,
		"timestamp": "2026-06-13T10:00:00Z",
		"message":   map[string]any{"role": "user", "content": "hello"},
	})
	path := filepath.Join(dir, "aaaa1111-2222-3333-4444-555566667777.jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -ageDays)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return claudehome.Home{Root: root, ProjectsDir: filepath.Join(root, claudehome.ProjectsSubdir), Source: "flag"}
}

func messages(r Report) string {
	var b strings.Builder
	for _, c := range r.Checks {
		b.WriteString(c.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// The case the GitHub reports describe: no cleanupPeriodDays set, so the 30-day
// default silently applies and old transcripts are already deletable.
func TestDoctorWarnsAboutExpiredTranscripts(t *testing.T) {
	home := claudeHomeWithTranscript(t, 45, "")
	got := messages(RunClaude(home))
	if !strings.Contains(got, "already past Claude Code's cleanup window") {
		t.Fatalf("no warning about expired transcripts:\n%s", got)
	}
	if !strings.Contains(got, "its default of 30 days") {
		t.Errorf("the warning does not say the default is in force:\n%s", got)
	}
	if !strings.Contains(got, "cct export --all --tool claude") {
		t.Errorf("the warning does not name the archive command:\n%s", got)
	}
}

func TestDoctorWarnsBeforeTheDeadline(t *testing.T) {
	home := claudeHomeWithTranscript(t, 25, `{"cleanupPeriodDays": 30}`)
	got := messages(RunClaude(home))
	if !strings.Contains(got, "within 14 days") {
		t.Fatalf("no upcoming-deadline warning:\n%s", got)
	}
	if !strings.Contains(got, "cleanupPeriodDays=30 from settings.json") {
		t.Errorf("the warning does not cite the configured setting:\n%s", got)
	}
}

// Someone who set a long retention deliberately should hear nothing alarming.
func TestDoctorQuietUnderALongRetention(t *testing.T) {
	home := claudeHomeWithTranscript(t, 400, `{"cleanupPeriodDays": 3650}`)
	got := messages(RunClaude(home))
	if strings.Contains(got, "cleanup window") && strings.Contains(got, "archive them") {
		t.Fatalf("a 10-year retention produced a deletion warning:\n%s", got)
	}
	if !strings.Contains(got, "No transcript is due for Claude Code's cleanup") {
		t.Errorf("no reassuring line:\n%s", got)
	}
}

func TestDoctorReportsUnreadableSettings(t *testing.T) {
	home := claudeHomeWithTranscript(t, 5, `{oops`)
	got := messages(RunClaude(home))
	if !strings.Contains(got, "cannot read Claude Code's cleanup setting") {
		t.Fatalf("a broken settings.json was passed over silently:\n%s", got)
	}
}
