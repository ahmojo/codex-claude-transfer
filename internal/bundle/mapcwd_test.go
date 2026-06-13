package bundle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ahmojo/Codex_Sync/internal/sessions"
)

// Fixed non-session_meta lines used to prove they are preserved byte-for-byte.
const (
	mapEventLine = `{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"first message"}}`
	mapRespLine  = `{"timestamp":"z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`
)

// metaSessionJSONL builds a 3-line rollout: a session_meta line carrying cwd and
// an unknown field that must be preserved, plus two fixed non-meta lines.
func metaSessionJSONL(t *testing.T, cwd string) []byte {
	t.Helper()
	cwdJSON, _ := json.Marshal(cwd)
	meta := `{"timestamp":"x","type":"session_meta","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":` +
		string(cwdJSON) + `,"source":"vscode","model_provider":"openai","cli_version":"1.2.3","custom_unknown_field":"keep-me"}}`
	return []byte(meta + "\n" + mapEventLine + "\n" + mapRespLine + "\n")
}

func TestMapCWDRewritesMatchingSession(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Mapped != 1 {
		t.Fatalf("counts: imported=%d mapped=%d", res.Imported, res.Mapped)
	}

	scan, err := sessions.Scan(target, sessions.ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scan.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(scan.Sessions))
	}
	s := scan.Sessions[0]
	if s.CWD != "/new/proj" {
		t.Errorf("cwd = %q, want /new/proj", s.CWD)
	}
	if s.FirstUserMessage != "first message" {
		t.Errorf("first message = %q, want \"first message\"", s.FirstUserMessage)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	got, _ := os.ReadFile(dest)
	if !bytes.Contains(got, []byte("keep-me")) {
		t.Errorf("unknown session_meta field was not preserved: %s", got)
	}
}

func TestMapCWDNoMatchLeavesContentByteForByte(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/some/other/path", New: "/new/proj"}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Mapped != 0 {
		t.Fatalf("counts: imported=%d mapped=%d", res.Imported, res.Mapped)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Errorf("content changed despite no matching mapping")
	}
}

func TestMapCWDChangesOnlyCWDLine(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	got, _ := os.ReadFile(dest)
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(lines))
	}
	if lines[1] != mapEventLine {
		t.Errorf("event line changed:\n got %q\nwant %q", lines[1], mapEventLine)
	}
	if lines[2] != mapRespLine {
		t.Errorf("response line changed:\n got %q\nwant %q", lines[2], mapRespLine)
	}
	if !strings.Contains(lines[0], `"/new/proj"`) {
		t.Errorf("meta line missing new cwd: %q", lines[0])
	}
	if strings.Contains(lines[0], "/source/proj") {
		t.Errorf("meta line still contains old cwd: %q", lines[0])
	}
	if !strings.Contains(lines[0], "keep-me") {
		t.Errorf("meta line lost unknown field: %q", lines[0])
	}
}

func TestMapCWDResultStillParses(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	scan, err := sessions.Scan(target, sessions.ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Valid != 1 {
		t.Fatalf("valid sessions = %d, want 1 (mapped JSONL must still parse)", scan.Valid)
	}
}

func TestMapCWDCompressedSkipped(t *testing.T) {
	dir := t.TempDir()
	zstRel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-bbbb1111-2222-3333-4444-555566667777.jsonl.zst"
	data := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01, 0x02, 0x03}
	bundlePath := buildBundle(t, dir, zstRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Mapped != 0 || res.MappedCompressedSkipped != 1 {
		t.Fatalf("counts: imported=%d mapped=%d compressedSkipped=%d",
			res.Imported, res.Mapped, res.MappedCompressedSkipped)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(zstRel))
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Errorf("compressed session was not copied byte-for-byte")
	}
}

func TestMapCWDMultipleMappingsSecondApplies(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD: []CWDMapping{
			{Old: "/unrelated/a", New: "/x/a"},
			{Old: "/source/proj", New: "/new/proj"},
		},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Mapped != 1 {
		t.Fatalf("mapped = %d, want 1", res.Mapped)
	}
	scan, _ := sessions.Scan(target, sessions.ScanOptions{})
	if len(scan.Sessions) != 1 || scan.Sessions[0].CWD != "/new/proj" {
		t.Errorf("cwd not remapped by second mapping: %+v", scan.Sessions)
	}
}

func TestMapCWDDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		DryRun:     true,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.DryRun {
		t.Errorf("expected DryRun=true")
	}
	// Dry-run still plans the mapping so counts are reported.
	if res.Imported != 1 || res.Mapped != 1 {
		t.Fatalf("dry-run counts: imported=%d mapped=%d", res.Imported, res.Mapped)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote the mapped file")
	}
}

func TestMapCWDConflictNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	const existing = "EXISTING different content"
	if err := os.WriteFile(dest, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/new/proj"}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.Imported != 0 || res.Mapped != 0 {
		t.Fatalf("counts: conflicts=%d imported=%d mapped=%d", res.Conflicts, res.Imported, res.Mapped)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != existing {
		t.Errorf("conflict overwrote existing file: %q", got)
	}
}

func TestMapCWDCaseInsensitiveOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive cwd matching only applies on Windows")
	}
	dir := t.TempDir()
	data := metaSessionJSONL(t, `C:\Users\me\Proj`)
	bundlePath := buildBundle(t, dir, sampleRel, data, `C:\Users\me\Proj`, nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		// Different case for the OLD path; must still match on Windows.
		MapCWD: []CWDMapping{{Old: `c:\users\me\proj`, New: `D:\new\proj`}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Mapped != 1 {
		t.Fatalf("mapped = %d, want 1 (case-insensitive match)", res.Mapped)
	}
}

func TestParseCWDMappingsValid(t *testing.T) {
	got, err := ParseCWDMappings([]string{"/old/a=/new/a", "/old/b=/new/b"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("mappings = %d, want 2", len(got))
	}
	if got[0].Old != "/old/a" || got[0].New != "/new/a" {
		t.Errorf("first mapping = %+v", got[0])
	}
}

func TestParseCWDMappingsRejectsBadSyntax(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"no-equals-sign"}); err == nil {
		t.Fatalf("expected error for missing '='")
	}
}

func TestParseCWDMappingsRejectsEmptyOld(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"=/new/a"}); err == nil {
		t.Fatalf("expected error for empty OLD")
	}
}

func TestParseCWDMappingsRejectsEmptyNew(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"/old/a="}); err == nil {
		t.Fatalf("expected error for empty NEW")
	}
}

func TestParseCWDMappingsRejectsRelativeNew(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"/old/a=relative/new"}); err == nil {
		t.Fatalf("expected error for non-absolute NEW")
	}
}

func TestParseCWDMappingsRejectsSameOldNew(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"/same/path=/same/path"}); err == nil {
		t.Fatalf("expected error for OLD == NEW")
	}
}

func TestParseCWDMappingsRejectsDuplicateOld(t *testing.T) {
	if _, err := ParseCWDMappings([]string{"/old/a=/new/a", "/old/a=/new/b"}); err == nil {
		t.Fatalf("expected error for duplicate OLD path")
	}
}
