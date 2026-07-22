package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCompressedSession drops a .jsonl.zst rollout into a home. Its bytes need
// not be valid zstd: sessions.Scan flags it compressed by extension, and every
// content-search path skips it — which is exactly what we want to surface.
func writeCompressedSession(t *testing.T, home, id string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+id+".jsonl.zst")
	if err := os.WriteFile(p, []byte("\x28\xb5\x2f\xfd not-real-zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunSearchReportsCompressedSkip confirms `cct search` visibly tells the user
// that compressed sessions were skipped (their text was unsearchable).
func TestRunSearchReportsCompressedSkip(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a") // plain, contains "Auth refactor"
	writeCompressedSession(t, home, "bbbb2222-2222-3333-4444-555566667777")

	var out, errOut bytes.Buffer
	if code := Run([]string{"search", "Auth", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("search exit=%d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "matched") {
		t.Errorf("expected a match:\n%s", s)
	}
	if !strings.Contains(s, "1 compressed session skipped") {
		t.Errorf("search did not surface the compressed skip:\n%s", s)
	}
	if !strings.Contains(s, "searchable text was unavailable") {
		t.Errorf("skip note missing the reason:\n%s", s)
	}
}

// TestRunSearchCompressedSkipJSON confirms the machine-readable count.
func TestRunSearchCompressedSkipJSON(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	writeCompressedSession(t, home, "bbbb2222-2222-3333-4444-555566667777")

	var out, errOut bytes.Buffer
	if code := Run([]string{"search", "Auth", "--json", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("search --json exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"compressed_skipped": 1`) {
		t.Errorf("search JSON missing compressed_skipped:\n%s", out.String())
	}
}

// TestRunScanReportsCompressedSkip confirms `cct scan` surfaces the same gap.
func TestRunScanReportsCompressedSkip(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	writeCompressedSession(t, home, "bbbb2222-2222-3333-4444-555566667777")

	var out, errOut bytes.Buffer
	if code := Run([]string{"scan", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("scan exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 compressed session skipped") {
		t.Errorf("scan did not surface the compressed skip:\n%s", out.String())
	}
}

// TestRunExportMatchReportsCompressedSkip confirms `export --match` surfaces the
// compressed sessions it could not search.
func TestRunExportMatchReportsCompressedSkip(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a") // matches "Auth"
	writeCompressedSession(t, home, "bbbb2222-2222-3333-4444-555566667777")

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--match", "Auth", "--codex-home", home, "-o", filepath.Join(tmp, "m.codexbundle")}, &out, &errOut)
	if code != 0 {
		t.Fatalf("export --match exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 compressed session skipped by --match") {
		t.Errorf("export --match did not surface the compressed skip:\n%s", out.String())
	}
}
