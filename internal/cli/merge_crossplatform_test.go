package cli

// End-to-end CLI regression test for issue #6: `import --merge` must not report
// conflicts for sessions that are semantically identical (or cleanly grown)
// after a cross-platform transfer where JSON key order differs.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawSession writes a session file with exact raw JSONL lines, so tests can
// control the serialization (key order) precisely.
func writeRawSession(t *testing.T, home, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "07", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-07-01T10-00-00-"+id+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRunImportMergeCrossPlatformKeyOrder simulates the issue's transfer: the
// exporting machine serialized the same records with a different key order than
// the importing one. One session is identical, one has an extra appended record.
// Both must resolve without conflicts.
func TestRunImportMergeCrossPlatformKeyOrder(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "mac-home")
	dst := filepath.Join(tmp, "wsl-home")

	idSame := "aaaa1111-2222-3333-4444-555566667777"
	idGrown := "bbbb1111-2222-3333-4444-555566667777"

	// Source machine ("macOS"): payload-first key order; the grown session has
	// one extra record the destination lacks.
	metaSrc := func(id string) string {
		return `{"payload":{"cwd":"/proj/x","id":"` + id + `"},"timestamp":"2026-07-01T10:00:00Z","type":"session_meta"}`
	}
	msgSrc := `{"payload":{"message":"hello","type":"user_message"},"timestamp":"2026-07-01T10:01:00Z","type":"event_msg"}`
	extraSrc := `{"payload":{"message":"new turn","type":"user_message"},"timestamp":"2026-07-01T10:02:00Z","type":"event_msg"}`
	writeRawSession(t, src, idSame, metaSrc(idSame), msgSrc)
	writeRawSession(t, src, idGrown, metaSrc(idGrown), msgSrc, extraSrc)

	// Destination machine ("WSL"): the SAME records, type-first key order.
	metaDst := func(id string) string {
		return `{"type":"session_meta","timestamp":"2026-07-01T10:00:00Z","payload":{"id":"` + id + `","cwd":"/proj/x"}}`
	}
	msgDst := `{"type":"event_msg","timestamp":"2026-07-01T10:01:00Z","payload":{"type":"user_message","message":"hello"}}`
	writeRawSession(t, dst, idSame, metaDst(idSame), msgDst)
	grownLocal := writeRawSession(t, dst, idGrown, metaDst(idGrown), msgDst)
	localBefore, _ := os.ReadFile(grownLocal)

	bundle := filepath.Join(tmp, "transfer.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", src, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit=%d stderr=%s", code, errOut.String())
	}

	// The reporter's exact invocation shape first: --merge --dry-run --json.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"import", bundle, "--merge", "--dry-run", "--json", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("dry-run exit=%d stderr=%s", code, errOut.String())
	}
	js := out.String()
	if !strings.Contains(js, `"conflicts": 0`) {
		t.Fatalf("dry-run JSON reports false conflicts (issue #6 regression):\n%s", js)
	}
	if !strings.Contains(js, `"updated": 1`) || !strings.Contains(js, `"already_ahead": 1`) {
		t.Fatalf("dry-run JSON plan wrong (want 1 updated + 1 already-ahead):\n%s", js)
	}

	// Real merge run: human output shows no conflicts, one appended session.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"import", bundle, "--merge", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
	}
	human := out.String()
	if !strings.Contains(human, "Conflicts: 0") {
		t.Fatalf("human output reports conflicts:\n%s", human)
	}
	if !strings.Contains(human, "Updated (new messages appended): 1") {
		t.Fatalf("human output missing the fast-forward:\n%s", human)
	}

	// The grown session kept its local serialization and gained the raw record.
	got, _ := os.ReadFile(grownLocal)
	if !bytes.HasPrefix(got, localBefore) {
		t.Errorf("local serialization was rewritten:\n%q", got)
	}
	if !strings.Contains(string(got), "new turn") {
		t.Errorf("appended record missing:\n%q", got)
	}

	// The merged file must still parse as a session (list finds both by id).
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"list", "--codex-home", dst}, &out, &errOut); code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), idGrown) || !strings.Contains(out.String(), idSame) {
		t.Fatalf("merged sessions no longer parse/list:\n%s", out.String())
	}
}
