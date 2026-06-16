package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// metaSessionBytes builds a minimal but realistic plain-JSONL rollout with a
// session_meta line carrying id+cwd plus a trailing user_message line.
func metaSessionBytes(id, cwd string) []byte {
	return []byte(`{"timestamp":"2026-06-13T18:22:01Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","source":"cli","model_provider":"openai"}}` + "\n" +
		`{"timestamp":"2026-06-13T18:22:02Z","type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n")
}

const sampleID = "aaaa1111-2222-3333-4444-555566667777"

func TestRewriteSessionMetaID(t *testing.T) {
	orig := metaSessionBytes(sampleID, "/proj/x")
	out, oldID, changed, err := rewriteSessionMetaID(orig, "11112222-3333-4444-5555-666677778888")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed")
	}
	if oldID != sampleID {
		t.Fatalf("oldID = %q, want %q", oldID, sampleID)
	}
	if err := validateCopiedJSONL(orig, out, "11112222-3333-4444-5555-666677778888"); err != nil {
		t.Fatalf("validate: %v", err)
	}
	// The cwd and the user message must survive byte-for-byte.
	if !strings.Contains(string(out), `"cwd":"/proj/x"`) {
		t.Errorf("cwd not preserved: %s", out)
	}
	if !strings.Contains(string(out), "user_message") {
		t.Errorf("user message line not preserved: %s", out)
	}
}

func TestRewriteSessionMetaIDNoID(t *testing.T) {
	// A session_meta without an id cannot be reassigned.
	body := []byte(`{"type":"session_meta","payload":{"cwd":"/proj/x"}}` + "\n")
	_, _, changed, err := rewriteSessionMetaID(body, "new-id")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed {
		t.Fatalf("expected no change when there is no id")
	}
}

func TestCopyDestRel(t *testing.T) {
	rel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-" + sampleID + ".jsonl"
	newID := "11112222-3333-4444-5555-666677778888"
	got := copyDestRel(rel, sampleID, newID)
	want := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-" + newID + ".jsonl"
	if got != want {
		t.Fatalf("copyDestRel = %q, want %q", got, want)
	}
	if !safety.IsSessionEntry(got) {
		t.Fatalf("copy dest %q is not a valid session entry", got)
	}
	// Fallback when the old id is not embedded in the filename.
	rel2 := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-deadbeef.jsonl"
	got2 := copyDestRel(rel2, sampleID, newID)
	if !strings.Contains(got2, newID) || !safety.IsSessionEntry(got2) {
		t.Fatalf("fallback copyDestRel = %q invalid", got2)
	}
}

func TestImportAsCopyOnConflict(t *testing.T) {
	dir := t.TempDir()
	bundleData := metaSessionBytes(sampleID, "/proj/x")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)

	// Pre-create the target with diverged (different) content for the same path.
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	local := append([]byte{}, bundleData...)
	local = append(local, []byte(`{"type":"event_msg","payload":{"type":"user_message","message":"local-only edit"}}`+"\n")...)
	if err := os.WriteFile(dest, local, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ImportAsCopy: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ImportedCopies != 1 || res.Conflicts != 0 || res.Imported != 0 || res.Replaced != 0 {
		t.Fatalf("counts: copies=%d conflicts=%d imported=%d replaced=%d", res.ImportedCopies, res.Conflicts, res.Imported, res.Replaced)
	}

	// The diverged local file is untouched.
	if got, _ := os.ReadFile(dest); string(got) != string(local) {
		t.Errorf("local file was modified by import-as-copy")
	}

	// The copy was written to a new path, with a new id, and otherwise identical.
	var copied *ImportItem
	for i := range res.Items {
		if res.Items[i].Action == ActionImportCopy {
			copied = &res.Items[i]
		}
	}
	if copied == nil {
		t.Fatalf("no ActionImportCopy item recorded")
	}
	if copied.NewThreadID == "" || copied.NewThreadID == sampleID {
		t.Fatalf("expected a fresh thread id, got %q", copied.NewThreadID)
	}
	if copied.DestPath == dest {
		t.Fatalf("copy wrote to the conflicting path instead of a new one")
	}
	got, err := os.ReadFile(copied.DestPath)
	if err != nil {
		t.Fatalf("copy not written: %v", err)
	}
	if !strings.Contains(string(got), copied.NewThreadID) {
		t.Errorf("copy does not carry the new id")
	}
	if strings.Contains(string(got), sampleID) {
		t.Errorf("copy still carries the old id")
	}
	// The new file must be a valid, importable session path.
	rel, _ := filepath.Rel(target.Root, copied.DestPath)
	if !safety.IsSessionEntry(filepath.ToSlash(rel)) {
		t.Errorf("copy path %q is not a session entry", rel)
	}
}

func TestImportAsCopyDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	bundleData := metaSessionBytes(sampleID, "/proj/x")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)

	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("diverged local content"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := listFilesRel(t, target.Root)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ImportAsCopy: true, DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ImportedCopies != 1 {
		t.Fatalf("planned copies = %d, want 1", res.ImportedCopies)
	}
	if after := listFilesRel(t, target.Root); len(after) != len(before) {
		t.Errorf("dry-run wrote files: before=%v after=%v", before, after)
	}
}

func TestImportAsCopyCompressedStaysConflict(t *testing.T) {
	dir := t.TempDir()
	zstRel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-bbbb1111-2222-3333-4444-555566667777.jsonl.zst"
	data := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01}
	bundlePath := buildBundle(t, dir, zstRel, data, "/proj/z", nil)
	target := fakeHome(t)

	dest := filepath.Join(target.Root, filepath.FromSlash(zstRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte{0x28, 0xb5, 0x2f, 0xfd, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ImportAsCopy: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.ImportedCopies != 0 {
		t.Fatalf("compressed conflict: conflicts=%d copies=%d", res.Conflicts, res.ImportedCopies)
	}
}
