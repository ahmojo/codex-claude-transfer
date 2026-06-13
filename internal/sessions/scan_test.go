package sessions

import (
	"path/filepath"
	"testing"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
)

// fakeHome builds a Home rooted at a temp dir. It never touches the real
// ~/.codex.
func fakeHome(t *testing.T) codexhome.Home {
	t.Helper()
	root := t.TempDir()
	home, err := codexhome.Detect(root)
	if err != nil {
		t.Fatalf("detect fake home: %v", err)
	}
	return home
}

func TestScanParsesValidSession(t *testing.T) {
	home := fakeHome(t)
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-1111aaaa-2222-3333-4444-555566667777.jsonl",
		validRolloutLines("1111aaaa-2222-3333-4444-555566667777", "/Users/example/dev/project"))

	res, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Files != 1 || res.Valid != 1 || res.Invalid != 0 {
		t.Fatalf("counts: files=%d valid=%d invalid=%d", res.Files, res.Valid, res.Invalid)
	}
	s := res.Sessions[0]
	if s.ThreadID != "1111aaaa-2222-3333-4444-555566667777" {
		t.Errorf("thread id = %q", s.ThreadID)
	}
	if s.CWD != "/Users/example/dev/project" {
		t.Errorf("cwd = %q", s.CWD)
	}
	if s.Source != "cli" {
		t.Errorf("source = %q", s.Source)
	}
	if s.ModelProvider != "openai" {
		t.Errorf("model provider = %q", s.ModelProvider)
	}
	if s.GitBranch != "main" || s.GitSHA != "abc123" {
		t.Errorf("git = %q/%q", s.GitBranch, s.GitSHA)
	}
	if s.FirstUserMessage != "Starter terminal implementation" {
		t.Errorf("first user message = %q", s.FirstUserMessage)
	}
	if s.Preview == "" {
		t.Errorf("expected non-empty preview")
	}
	if !s.Parsed {
		t.Errorf("expected Parsed=true")
	}
}

func TestScanNestedDirectories(t *testing.T) {
	home := fakeHome(t)
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		validRolloutLines("aaaa1111-2222-3333-4444-555566667777", "/a"))
	writeRollout(t, home.SessionsDir, "2026/06/12",
		"rollout-2026-06-12T21-10-00-bbbb1111-2222-3333-4444-555566667777.jsonl",
		validRolloutLines("bbbb1111-2222-3333-4444-555566667777", "/b"))

	res, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Files != 2 || res.Valid != 2 {
		t.Fatalf("expected 2 valid sessions, got files=%d valid=%d", res.Files, res.Valid)
	}
}

func TestScanDetectsCompressedButDoesNotParse(t *testing.T) {
	home := fakeHome(t)
	// A .jsonl.zst file with arbitrary bytes — we should detect it without
	// trying to parse its (compressed) contents.
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-cccc1111-2222-3333-4444-555566667777.jsonl.zst",
		"not really zstd, must not be parsed as json")

	res, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Files != 1 || res.Compressed != 1 {
		t.Fatalf("expected 1 compressed file, got files=%d compressed=%d", res.Files, res.Compressed)
	}
	s := res.Sessions[0]
	if !s.Compressed {
		t.Errorf("expected Compressed=true")
	}
	if s.Parsed {
		t.Errorf("expected compressed file to be unparsed")
	}
	if s.CreatedAt != "2026-06-13T18-22-01" {
		t.Errorf("created_at from name = %q", s.CreatedAt)
	}
}

func TestScanInvalidJSONDoesNotCrash(t *testing.T) {
	home := fakeHome(t)
	body := "{ this is not valid json\n{\"timestamp\":\"x\",\"type\":\"session_meta\",\"payload\":{\"id\":\"dddd1111-2222-3333-4444-555566667777\",\"cwd\":\"/c\",\"source\":\"cli\"}}\nanother bad line\n"
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-dddd1111-2222-3333-4444-555566667777.jsonl", body)

	res, err := Scan(home, ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Files != 1 {
		t.Fatalf("expected 1 file, got %d", res.Files)
	}
	s := res.Sessions[0]
	// SessionMeta on a valid line should still be recovered despite bad lines.
	if !s.Parsed || s.ThreadID != "dddd1111-2222-3333-4444-555566667777" {
		t.Errorf("expected to recover meta from valid line; parsed=%v id=%q", s.Parsed, s.ThreadID)
	}
	if len(s.Warnings) == 0 {
		t.Errorf("expected warnings for invalid lines")
	}
}

func TestScanArchivedExcludedByDefault(t *testing.T) {
	home := fakeHome(t)
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-eeee1111-2222-3333-4444-555566667777.jsonl",
		validRolloutLines("eeee1111-2222-3333-4444-555566667777", "/active"))
	writeRollout(t, home.ArchivedSessionsDir, "2026/06/10",
		"rollout-2026-06-10T10-00-00-ffff1111-2222-3333-4444-555566667777.jsonl",
		validRolloutLines("ffff1111-2222-3333-4444-555566667777", "/archived"))

	def, _ := Scan(home, ScanOptions{})
	if def.Files != 1 {
		t.Fatalf("default scan should exclude archived; got %d files", def.Files)
	}

	withArch, _ := Scan(home, ScanOptions{IncludeArchived: true})
	if withArch.Files != 2 {
		t.Fatalf("expected 2 files when including archived; got %d", withArch.Files)
	}
	var sawArchived bool
	for _, s := range withArch.Sessions {
		if s.Archived {
			sawArchived = true
		}
	}
	if !sawArchived {
		t.Errorf("expected at least one session marked Archived")
	}
}

func TestScanIgnoresNonRolloutFiles(t *testing.T) {
	home := fakeHome(t)
	writeRollout(t, home.SessionsDir, "2026/06/13", "notes.txt", "ignore me")
	writeRollout(t, home.SessionsDir, "2026/06/13", "session.json", "{}")
	res, _ := Scan(home, ScanOptions{})
	if res.Files != 0 {
		t.Fatalf("expected non-rollout files to be ignored, got %d", res.Files)
	}
}

func TestScanUnknownFutureFieldsIgnored(t *testing.T) {
	home := fakeHome(t)
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"99991111-2222-3333-4444-555566667777","cwd":"/c","source":"cli","brand_new_future_field":{"nested":true},"another_unknown":42}}
{"timestamp":"y","type":"some_future_record","payload":{"whatever":true}}
{"timestamp":"z","type":"event_msg","payload":{"type":"user_message","message":"hello future"}}
`
	writeRollout(t, home.SessionsDir, "2026/06/13",
		"rollout-2026-06-13T18-22-01-99991111-2222-3333-4444-555566667777.jsonl", body)
	res, _ := Scan(home, ScanOptions{})
	if res.Valid != 1 {
		t.Fatalf("expected 1 valid session despite unknown fields, got valid=%d", res.Valid)
	}
	if res.Sessions[0].FirstUserMessage != "hello future" {
		t.Errorf("first user message = %q", res.Sessions[0].FirstUserMessage)
	}
}

func TestScanResultRelPath(t *testing.T) {
	home := fakeHome(t)
	name := "rollout-2026-06-13T18-22-01-77771111-2222-3333-4444-555566667777.jsonl"
	writeRollout(t, home.SessionsDir, "2026/06/13", name,
		validRolloutLines("77771111-2222-3333-4444-555566667777", "/x"))
	res, _ := Scan(home, ScanOptions{})
	want := filepath.ToSlash(filepath.Join("2026/06/13", name))
	if res.Sessions[0].RelPath != want {
		t.Errorf("relpath = %q, want %q", res.Sessions[0].RelPath, want)
	}
}
