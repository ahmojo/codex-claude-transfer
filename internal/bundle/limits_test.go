package bundle

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
)

func TestSessionByteLimitDefaultAndHardCap(t *testing.T) {
	if limit, err := SessionByteLimit(0); err != nil || limit != 100<<20 {
		t.Fatalf("default = %d, %v", limit, err)
	}
	if limit, err := SessionByteLimit(150 << 20); err != nil || limit != 150<<20 {
		t.Fatalf("override = %d, %v", limit, err)
	}
	for _, limit := range []int64{-1, HardMaxSessionBytes + 1} {
		if _, err := SessionByteLimit(limit); err == nil {
			t.Fatalf("invalid limit accepted: %d", limit)
		}
		if _, err := Import(fakeHome(t), ImportOptions{MaxSessionBytes: limit}); err == nil {
			t.Fatal("import accepted an invalid limit")
		}
	}
	zr := &zip.Reader{File: []*zip.File{{FileHeader: zip.FileHeader{Name: sampleRel, UncompressedSize64: HardMaxSessionBytes + 1}}}}
	if err := verifyBundle(zr, Checksums{}, HardMaxSessionBytes); err == nil {
		t.Fatal("entry above hard cap accepted")
	}
}

func TestImportSessionByteLimit(t *testing.T) {
	data := []byte("session line one\nsession line two\n")
	path := buildBundle(t, t.TempDir(), sampleRel, data, "/project", nil)
	home := fakeHome(t)
	if _, err := Import(home, ImportOptions{BundlePath: path, MaxSessionBytes: int64(len(data) - 1)}); err == nil {
		t.Fatal("oversize session accepted")
	}
	if entries, err := os.ReadDir(home.Root); err != nil || len(entries) != 0 {
		t.Fatalf("rejected import wrote to destination: %v, %v", entries, err)
	}
	if _, err := Import(home, ImportOptions{BundlePath: path, MaxSessionBytes: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home.Root, filepath.FromSlash(sampleRel)))
	if err != nil || string(got) != string(data) {
		t.Fatalf("import changed bytes: %q, %v", got, err)
	}
	// An override belongs to one call, not the process or agent home.
	larger := buildBundle(t, t.TempDir(), sampleRel, append(append([]byte{}, data...), data...), "/project", nil)
	if _, err := Import(fakeHome(t), ImportOptions{BundlePath: larger}); err != nil {
		t.Fatal(err)
	}
}

func TestRaisedSessionLimitDryRunWarning(t *testing.T) {
	data := []byte(`{"type":"session_meta","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":"/project"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n")
	path := buildBundle(t, t.TempDir(), sampleRel, data, "/project", nil)
	home := fakeHome(t)
	res, err := Import(home, ImportOptions{BundlePath: path, DryRun: true, MaxSessionBytes: 150 << 20})
	if err != nil || !strings.Contains(strings.Join(res.Warnings, "\n"), "memory and disk") {
		t.Fatalf("dry-run warning missing: %v, %v", res.Warnings, err)
	}
	tr, err := TranslateImport(home, TranslateOptions{BundlePath: path, TargetTool: agent.Claude, DryRun: true, MaxSessionBytes: 150 << 20})
	if err != nil || tr.Translated != 1 || len(tr.Warnings) != 1 {
		t.Fatalf("translation dry run = %+v, %v", tr, err)
	}
	if _, err := TranslateImport(home, TranslateOptions{BundlePath: path, TargetTool: agent.Claude, MaxSessionBytes: int64(len(data) - 1)}); err == nil {
		t.Fatal("translation ignored the limit")
	}
	if entries, err := os.ReadDir(home.Root); err != nil || len(entries) != 0 {
		t.Fatalf("preview/rejection wrote to destination: %v, %v", entries, err)
	}
}

func TestImportUnknownHistoryStructureWritesNothing(t *testing.T) {
	for _, base := range []string{`"unknown"`, `[]`, `{"future_position":123}`} {
		bundle := buildMultiBundle(t, t.TempDir(), []multiSession{
			{id: "11111111-1111-4111-8111-111111111111", data: []byte(`{"type":"session_meta","payload":{"id":"valid"}}` + "\n")},
			{id: "22222222-2222-4222-8222-222222222222", data: []byte(`{"type":"session_meta","payload":{"id":"unknown","history_base":` + base + `}}` + "\n")},
		})
		home := fakeHome(t)
		if _, err := Import(home, ImportOptions{BundlePath: bundle}); err == nil {
			t.Fatalf("unknown history accepted: %s", base)
		}
		entries, err := os.ReadDir(home.Root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("failed import mutated destination: %v, %v", entries, err)
		}
	}
}

func TestReadCapped(t *testing.T) {
	if _, err := readCapped(strings.NewReader("hello"), 100, "x"); err != nil {
		t.Fatalf("within limit: %v", err)
	}
	if _, err := readCapped(strings.NewReader("abc"), 3, "x"); err != nil {
		t.Fatalf("exactly at limit should be ok: %v", err)
	}
	if _, err := readCapped(strings.NewReader("toolong"), 3, "x"); err == nil {
		t.Fatal("over-limit read should error (bomb guard)")
	}
}

func TestCheckDeclaredSize(t *testing.T) {
	over := &zip.File{FileHeader: zip.FileHeader{Name: "big", UncompressedSize64: uint64(MaxSessionBytes) + 1}}
	if err := checkDeclaredSize(over, MaxSessionBytes, "big"); err == nil {
		t.Fatal("declared-oversize entry should be rejected")
	}
	ok := &zip.File{FileHeader: zip.FileHeader{Name: "ok", UncompressedSize64: 10}}
	if err := checkDeclaredSize(ok, MaxSessionBytes, "ok"); err != nil {
		t.Fatalf("normal entry should pass: %v", err)
	}
}
