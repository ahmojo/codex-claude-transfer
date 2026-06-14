package bundle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportAllIncludesAllCwds(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	writeSession(t, day, "rollout-2026-06-13T19-00-00-bbbb1111-2222-3333-4444-555566667777.jsonl",
		"bbbb1111-2222-3333-4444-555566667777", "/proj/b")

	out := filepath.Join(t.TempDir(), "all.codexbundle")
	res, err := Export(home, ExportOptions{ProjectPath: "", OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 2 {
		t.Fatalf("included = %d, want 2 (export all ignores cwd)", res.IncludedCount)
	}
}

func TestExportAllIncludesCompressed(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-cccc1111-2222-3333-4444-555566667777.jsonl",
		"cccc1111-2222-3333-4444-555566667777", "/proj/c")
	zst := filepath.Join(day, "rollout-2026-06-13T19-00-00-dddd1111-2222-3333-4444-555566667777.jsonl.zst")
	if err := os.WriteFile(zst, []byte("opaque zstd bytes"), 0o644); err != nil {
		t.Fatalf("write zst: %v", err)
	}

	out := filepath.Join(t.TempDir(), "allz.codexbundle")
	// No ProjectPath = --all: compressed sessions (unknown cwd) are included.
	res, err := Export(home, ExportOptions{OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 2 {
		t.Fatalf("included = %d, want 2 (compressed included by --all)", res.IncludedCount)
	}
	if res.CompressedSkipped != 0 {
		t.Errorf("compressed skipped = %d, want 0 with --all", res.CompressedSkipped)
	}
}

func TestExportSinceFiltersOlder(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	oldName := "rollout-2026-06-13T18-22-01-eeee1111-2222-3333-4444-555566667777.jsonl"
	newName := "rollout-2026-06-13T19-00-00-ffff1111-2222-3333-4444-555566667777.jsonl"
	writeSession(t, day, oldName, "eeee1111-2222-3333-4444-555566667777", "/proj/a")
	writeSession(t, day, newName, "ffff1111-2222-3333-4444-555566667777", "/proj/a")

	// Backdate the "old" session well before the cutoff.
	oldPath := filepath.Join(day, oldName)
	oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	cutoff := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	out := filepath.Join(t.TempDir(), "since.codexbundle")
	res, err := Export(home, ExportOptions{OutputPath: out, Since: cutoff})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1 (since filter)", res.IncludedCount)
	}
	if res.Manifest.Sessions[0].ThreadID != "ffff1111-2222-3333-4444-555566667777" {
		t.Errorf("wrong session survived since filter: %q", res.Manifest.Sessions[0].ThreadID)
	}
}

func TestExportSessionByExactThreadID(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	writeSession(t, day, "rollout-2026-06-13T19-00-00-bbbb1111-2222-3333-4444-555566667777.jsonl",
		"bbbb1111-2222-3333-4444-555566667777", "/proj/b")

	out := filepath.Join(t.TempDir(), "one.codexbundle")
	res, err := Export(home, ExportOptions{SessionID: "bbbb1111-2222-3333-4444-555566667777", OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1", res.IncludedCount)
	}
	if res.Manifest.Sessions[0].ThreadID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("wrong session: %q", res.Manifest.Sessions[0].ThreadID)
	}
}

func TestExportSessionByPrefix(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	writeSession(t, day, "rollout-2026-06-13T19-00-00-bbbb1111-2222-3333-4444-555566667777.jsonl",
		"bbbb1111-2222-3333-4444-555566667777", "/proj/b")

	out := filepath.Join(t.TempDir(), "pref.codexbundle")
	res, err := Export(home, ExportOptions{SessionID: "bbbb1111", OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 || res.Manifest.Sessions[0].ThreadID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("prefix selection failed: %+v", res.Manifest.Sessions)
	}
}

func TestExportSessionNoMatchErrors(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", "/proj/a")

	out := filepath.Join(t.TempDir(), "nomatch.codexbundle")
	if _, err := Export(home, ExportOptions{SessionID: "zzzz", OutputPath: out}); err == nil {
		t.Fatalf("expected error for unmatched thread id")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("no bundle should be written when nothing matches")
	}
}

func TestExportSessionAmbiguousPrefixErrors(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-abcd1111-2222-3333-4444-555566667777.jsonl",
		"abcd1111-2222-3333-4444-555566667777", "/proj/a")
	writeSession(t, day, "rollout-2026-06-13T19-00-00-abcd2222-2222-3333-4444-555566667777.jsonl",
		"abcd2222-2222-3333-4444-555566667777", "/proj/b")

	out := filepath.Join(t.TempDir(), "ambig.codexbundle")
	if _, err := Export(home, ExportOptions{SessionID: "abcd", OutputPath: out}); err == nil {
		t.Fatalf("expected error for ambiguous prefix")
	}
}

func TestExportSinceWithProject(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	// Two sessions in the target project; one is backdated and must be dropped.
	keepName := "rollout-2026-06-13T19-00-00-11110000-2222-3333-4444-555566667777.jsonl"
	dropName := "rollout-2026-06-13T18-22-01-22220000-2222-3333-4444-555566667777.jsonl"
	writeSession(t, day, keepName, "11110000-2222-3333-4444-555566667777", "/proj/wanted")
	writeSession(t, day, dropName, "22220000-2222-3333-4444-555566667777", "/proj/wanted")
	// A third session in another project must be excluded by the cwd filter.
	writeSession(t, day, "rollout-2026-06-13T20-00-00-33330000-2222-3333-4444-555566667777.jsonl",
		"33330000-2222-3333-4444-555566667777", "/proj/other")

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(day, dropName), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	out := filepath.Join(t.TempDir(), "pw.codexbundle")
	res, err := Export(home, ExportOptions{
		ProjectPath: "/proj/wanted",
		OutputPath:  out,
		Since:       time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1 (since + project)", res.IncludedCount)
	}
	if res.Manifest.Sessions[0].ThreadID != "11110000-2222-3333-4444-555566667777" {
		t.Errorf("wrong session exported: %q", res.Manifest.Sessions[0].ThreadID)
	}
}
