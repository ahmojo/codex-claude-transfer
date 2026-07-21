package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, rfc3339 string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	return tt
}

// exportAllFrom exports every session in home to a bundle path and fails the test
// on error.
func exportAllFrom(t *testing.T, home, bundle string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", home, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit=%d stderr=%s", code, errOut.String())
	}
}

// TestRunUndoReversesImport imports a bundle into an empty home, then `cct undo`
// removes exactly the files that import created.
func TestRunUndoReversesImport(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, src, id, "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"import", bundle, "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
	}
	dest := filepath.Join(dst, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("import did not create the session: %v", err)
	}

	// Undo it.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 removed") {
		t.Errorf("undo did not report a removal:\n%s", out.String())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("undo did not remove the imported session")
	}

	// A second undo has nothing to reverse (journal was consumed).
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("second undo exit=%d", code)
	}
	if !strings.Contains(out.String(), "Nothing to undo") {
		t.Errorf("second undo should find nothing:\n%s", out.String())
	}
}

// TestRunUndoKeepsEditedFile confirms undo refuses to delete an imported file
// that was modified after the import.
func TestRunUndoKeepsEditedFile(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, src, id, "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"import", bundle, "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
	}
	dest := filepath.Join(dst, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	// Edit it after the import.
	if err := os.WriteFile(dest, []byte("edited-after-import\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d", code)
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Errorf("undo should skip the edited file:\n%s", out.String())
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "edited-after-import\n" {
		t.Errorf("undo clobbered an edited file: %q", got)
	}
}

// TestRunUndoDryRun previews the reversal without changing anything.
func TestRunUndoDryRun(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, src, id, "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	Run([]string{"import", bundle, "--codex-home", dst}, &out, &errOut)
	dest := filepath.Join(dst, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--dry-run", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("undo --dry-run exit=%d", code)
	}
	if !strings.Contains(out.String(), "Dry run") {
		t.Errorf("expected dry-run banner:\n%s", out.String())
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dry-run undo removed the file")
	}
}

// TestRunUndoReversesMerge confirms a --merge update is undone by restoring the
// pre-append original file.
func TestRunUndoReversesMerge(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	id := "aaaa1111-2222-3333-4444-555566667777"

	// Destination has the short (original) session; source has the grown one.
	writeRawSession(t, dst, id,
		`{"type":"session_meta","payload":{"id":"`+id+`","cwd":"/p"}}`,
		`{"type":"event_msg","payload":{"message":"one"}}`)
	before, _ := os.ReadFile(filepath.Join(dst, "sessions", "2026", "07", "01", "rollout-2026-07-01T10-00-00-"+id+".jsonl"))

	writeRawSession(t, src, id,
		`{"type":"session_meta","payload":{"id":"`+id+`","cwd":"/p"}}`,
		`{"type":"event_msg","payload":{"message":"one"}}`,
		`{"type":"event_msg","payload":{"message":"two"}}`)
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"import", bundle, "--merge", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("merge import exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Updated (new messages appended): 1") {
		t.Fatalf("expected a merge update:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 restored") {
		t.Errorf("undo did not restore the merged file:\n%s", out.String())
	}
	after, _ := os.ReadFile(filepath.Join(dst, "sessions", "2026", "07", "01", "rollout-2026-07-01T10-00-00-"+id+".jsonl"))
	if !bytes.Equal(after, before) {
		t.Errorf("undo did not restore original merge content:\n before=%q\n after=%q", before, after)
	}
}

// TestRunDiffPreview confirms `cct diff` reports new/grow/conflict without
// writing anything.
func TestRunDiffPreview(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	idNew := "1111aaaa-2222-3333-4444-555566667777"
	idGrow := "2222bbbb-2222-3333-4444-555566667777"

	// dst already has the short grow-session; src has a grown copy + a brand-new one.
	writeRawSession(t, dst, idGrow,
		`{"type":"session_meta","payload":{"id":"`+idGrow+`","cwd":"/p"}}`,
		`{"type":"event_msg","payload":{"message":"one"}}`)
	writeRawSession(t, src, idGrow,
		`{"type":"session_meta","payload":{"id":"`+idGrow+`","cwd":"/p"}}`,
		`{"type":"event_msg","payload":{"message":"one"}}`,
		`{"type":"event_msg","payload":{"message":"two"}}`)
	writeRawSession(t, src, idNew,
		`{"type":"session_meta","payload":{"id":"`+idNew+`","cwd":"/p"}}`,
		`{"type":"event_msg","payload":{"message":"fresh"}}`)
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"diff", bundle, "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("diff exit=%d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "new        1") {
		t.Errorf("diff missing the new session:\n%s", s)
	}
	if !strings.Contains(s, "grow       1") {
		t.Errorf("diff missing the grown session:\n%s", s)
	}
	if !strings.Contains(s, "Read-only") {
		t.Errorf("diff should state it is read-only:\n%s", s)
	}
	if !strings.Contains(s, "+1 line") {
		t.Errorf("diff should show the grow line count:\n%s", s)
	}
	// Read-only: the new session must NOT be written to dst.
	gotNew := filepath.Join(dst, "sessions", "2026", "07", "01", "rollout-2026-07-01T10-00-00-"+idNew+".jsonl")
	if _, err := os.Stat(gotNew); !os.IsNotExist(err) {
		t.Errorf("diff wrote a file; it must be read-only")
	}
}

// TestRunDiffJSON confirms the machine-readable diff.
func TestRunDiffJSON(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	writeSessionCWD(t, src, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"diff", bundle, "--json", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("diff --json exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"new": 1`) {
		t.Errorf("diff JSON missing new count:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"change": "new"`) {
		t.Errorf("diff JSON missing change detail:\n%s", out.String())
	}
}

// TestRunImportSinceFilter confirms --since restricts a CLI import to recent
// sessions in the bundle.
func TestRunImportSinceFilter(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	idOld := "1111aaaa-2222-3333-4444-555566667777"
	idNew := "2222bbbb-2222-3333-4444-555566667777"
	writeSessionCWD(t, src, idOld, "/proj/a")
	writeSessionCWD(t, src, idNew, "/proj/a")
	// Age the "old" one well into the past.
	oldPath := filepath.Join(src, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+idOld+".jsonl")
	old := mustTime(t, "2026-01-01T00:00:00Z")
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(tmp, "p.codexbundle")
	exportAllFrom(t, src, bundle)

	var out, errOut bytes.Buffer
	if code := Run([]string{"import", bundle, "--since", "2026-06-01", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("import --since exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "New sessions: 1") {
		t.Errorf("expected only the recent session imported:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Skipped (excluded by filter): 1") {
		t.Errorf("expected the old session excluded:\n%s", out.String())
	}
}
