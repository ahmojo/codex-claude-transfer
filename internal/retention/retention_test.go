package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

func writeSettings(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SettingsFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A home without settings.json is the case most reports of surprise deletion
// were in: the default applies and the user never chose it.
func TestLoadPolicyDefaultsWhenUnset(t *testing.T) {
	root := t.TempDir()
	p, err := LoadPolicy(root)
	if err != nil {
		t.Fatalf("missing settings.json must not be an error: %v", err)
	}
	if p.Days != DefaultDays || p.Configured {
		t.Fatalf("policy = %+v, want the %d-day default and Configured=false", p, DefaultDays)
	}

	writeSettings(t, root, `{"model":"opus","env":{"FOO":"bar"}}`)
	if p, err = LoadPolicy(root); err != nil || p.Days != DefaultDays || p.Configured {
		t.Fatalf("settings without the key: policy = %+v err=%v", p, err)
	}
}

func TestLoadPolicyReadsTheSetting(t *testing.T) {
	root := t.TempDir()
	// A realistic file: the retention number sits among values cct must ignore.
	writeSettings(t, root, `{"cleanupPeriodDays": 3650, "env": {"ANTHROPIC_API_KEY": "sk-secret"}}`)
	p, err := LoadPolicy(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Days != 3650 || !p.Configured {
		t.Fatalf("policy = %+v, want 3650 days, Configured=true", p)
	}
	if filepath.Base(p.SettingsPath) != SettingsFileName {
		t.Errorf("SettingsPath = %q", p.SettingsPath)
	}
}

// A file cct cannot parse means cct cannot tell which rule is in force, and
// saying nothing would be worse than saying so.
func TestLoadPolicyReportsUnusableSettings(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, `{not json`)
	if _, err := LoadPolicy(root); err == nil {
		t.Error("invalid JSON was accepted")
	}
	writeSettings(t, root, `{"cleanupPeriodDays": 0}`)
	if _, err := LoadPolicy(root); err == nil {
		t.Error("a zero retention period was accepted")
	}
	writeSettings(t, root, `{"cleanupPeriodDays": -5}`)
	if _, err := LoadPolicy(root); err == nil {
		t.Error("a negative retention period was accepted")
	}
}

func TestAssessSplitsExpiredFromSoon(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	policy := Policy{Days: 30, Configured: true}
	age := func(days int) time.Time { return now.AddDate(0, 0, -days) }

	list := []sessions.Session{
		{ThreadID: "gone", ModTime: age(40)},        // 10 days past the window
		{ThreadID: "edge", ModTime: age(31)},        // just past it
		{ThreadID: "soon", ModTime: age(25)},        // 5 days left
		{ThreadID: "later", ModTime: age(5)},        // 25 days left
		{ThreadID: "undated", ModTime: time.Time{}}, // no mtime: not guessed at
	}

	rep := Assess(list, policy, now, 14*24*time.Hour)
	if rep.Total != 5 {
		t.Fatalf("Total = %d", rep.Total)
	}
	if len(rep.Expired) != 2 || rep.Expired[0].ThreadID != "gone" {
		t.Fatalf("expired = %+v, want gone then edge (oldest first)", rep.Expired)
	}
	if len(rep.Soon) != 1 || rep.Soon[0].ThreadID != "soon" {
		t.Fatalf("soon = %+v, want just 'soon'", rep.Soon)
	}
	if rep.Soon[0].DaysLeft != 5 {
		t.Errorf("DaysLeft = %d, want 5", rep.Soon[0].DaysLeft)
	}
	if want := age(25).AddDate(0, 0, 30); !rep.NextDeadline.Equal(want) {
		t.Errorf("NextDeadline = %v, want %v", rep.NextDeadline, want)
	}
	if !rep.Any() {
		t.Error("Any() = false with expired and soon items")
	}
}

// A long retention period is the whole point of setting one: nothing should be
// reported as at risk.
func TestAssessQuietUnderALongPolicy(t *testing.T) {
	now := time.Now()
	policy := Policy{Days: 3650, Configured: true}
	list := []sessions.Session{
		{ThreadID: "old", ModTime: now.AddDate(0, 0, -400)},
		{ThreadID: "new", ModTime: now},
	}
	rep := Assess(list, policy, now, 14*24*time.Hour)
	if rep.Any() {
		t.Fatalf("a 10-year policy reported risk: %+v", rep)
	}
}

func TestCutoff(t *testing.T) {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	if got := (Policy{Days: 30}).Cutoff(now); !got.Equal(time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Cutoff = %v", got)
	}
}
