package zstdcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// compress writes a zstd-compressed copy of src at dst using the real zstd CLI.
func compress(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("zstd", "-q", "-f", "-o", dst, src).CombinedOutput(); err != nil {
		t.Fatalf("zstd compress: %v: %s", err, out)
	}
}

func TestDecompressHeadRoundTrip(t *testing.T) {
	if !Available() {
		t.Skip("zstd not installed; skipping round-trip")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.txt")
	content := strings.Repeat("hello world\n", 2000)
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "in.txt.zst")
	compress(t, src, dst)

	// A bounded head must return only the requested prefix.
	head, err := DecompressHead(dst, 64)
	if err != nil {
		t.Fatalf("decompress head: %v", err)
	}
	if len(head) > 64 {
		t.Fatalf("head longer than requested: %d", len(head))
	}
	if !strings.HasPrefix(content, string(head)) {
		t.Fatalf("head is not a prefix of the original: %q", head)
	}

	// maxBytes <= 0 falls back to the default and reads the whole small file.
	full, err := DecompressHead(dst, 0)
	if err != nil {
		t.Fatalf("decompress full: %v", err)
	}
	if string(full) != content {
		t.Fatalf("full decompression mismatch")
	}
}

func TestDecompressHeadRejectsGarbage(t *testing.T) {
	if !Available() {
		t.Skip("zstd not installed; skipping garbage check")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl.zst")
	if err := os.WriteFile(bad, []byte("this is definitely not a zstd frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecompressHead(bad, 0); err == nil {
		t.Fatalf("expected an error decompressing non-zstd bytes")
	}
}
