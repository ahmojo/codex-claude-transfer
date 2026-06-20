package repair

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	contentLast = "2026-06-14T15:48:00Z" // newest timestamp inside the fixture
	importMtime = "2026-06-17T23:51:00Z" // simulated cct import time (too new)
)

func writeRollout(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-06-14T15:00:00Z","type":"session_meta","payload":{"id":"x"}}` + "\n" +
		`{"timestamp":"` + contentLast + `","type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n"
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func setMtime(t *testing.T, path, rfc3339 string) {
	t.Helper()
	ts, _ := time.Parse(time.RFC3339, rfc3339)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// TestRepairResetsImportedMtime: a file whose mtime is days after its newest
// internal timestamp (the import signature) is reset to that content time.
func TestRepairResetsImportedMtime(t *testing.T) {
	root := t.TempDir()
	sdir := filepath.Join(root, "sessions", "2026", "06", "14")
	p := writeRollout(t, sdir, "rollout-2026-06-14T15-00-00-aaaa1111-2222-3333-4444-555566667777.jsonl")
	setMtime(t, p, importMtime)

	res, err := RepairTimes([]string{filepath.Join(root, "sessions")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 1 || res.Fixed != 1 {
		t.Fatalf("scanned=%d fixed=%d, want 1/1", res.Scanned, res.Fixed)
	}
	want, _ := time.Parse(time.RFC3339, contentLast)
	info, _ := os.Stat(p)
	if !info.ModTime().Equal(want) {
		t.Errorf("mtime = %v, want %v", info.ModTime().UTC(), want)
	}
}

// TestRepairLeavesNativeFileAlone: a file whose mtime already matches its content
// (a normal native session) is not touched.
func TestRepairLeavesNativeFileAlone(t *testing.T) {
	root := t.TempDir()
	sdir := filepath.Join(root, "sessions", "2026", "06", "14")
	p := writeRollout(t, sdir, "rollout-2026-06-14T15-00-00-bbbb1111-2222-3333-4444-555566667777.jsonl")
	setMtime(t, p, contentLast) // mtime == content's last timestamp

	res, err := RepairTimes([]string{filepath.Join(root, "sessions")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fixed != 0 {
		t.Errorf("fixed=%d, want 0 (native file should be untouched)", res.Fixed)
	}
	want, _ := time.Parse(time.RFC3339, contentLast)
	info, _ := os.Stat(p)
	if !info.ModTime().Equal(want) {
		t.Errorf("native mtime changed: %v", info.ModTime().UTC())
	}
}

// TestRepairDryRunReportsButDoesNotWrite: dry-run counts the candidate but leaves
// the file's mtime untouched.
func TestRepairDryRunReportsButDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	sdir := filepath.Join(root, "sessions", "2026", "06", "14")
	p := writeRollout(t, sdir, "rollout-2026-06-14T15-00-00-cccc1111-2222-3333-4444-555566667777.jsonl")
	setMtime(t, p, importMtime)

	res, err := RepairTimes([]string{filepath.Join(root, "sessions")}, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fixed != 1 {
		t.Fatalf("dry-run fixed=%d, want 1 (would-fix count)", res.Fixed)
	}
	orig, _ := time.Parse(time.RFC3339, importMtime)
	info, _ := os.Stat(p)
	if !info.ModTime().Equal(orig) {
		t.Errorf("dry-run changed the mtime to %v; should have left %v", info.ModTime().UTC(), orig)
	}
}
