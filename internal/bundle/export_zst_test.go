package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/Codex_Sync/internal/zstdcli"
)

// writeCompressedSession writes a real zstd-compressed rollout under the home's
// sessions dir, returning its path. name must end in .jsonl.zst.
func writeCompressedSession(t *testing.T, sessionsDir, name, body string) {
	t.Helper()
	plain := filepath.Join(t.TempDir(), "plain.jsonl")
	if err := os.WriteFile(plain, []byte(body), 0o644); err != nil {
		t.Fatalf("write plain: %v", err)
	}
	dir := filepath.Join(sessionsDir, "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dst := filepath.Join(dir, name)
	if out, err := exec.Command("zstd", "-q", "-f", "-o", dst, plain).CombinedOutput(); err != nil {
		t.Fatalf("zstd compress: %v: %s", err, out)
	}
}

func TestExportProjectIncludesCompressed(t *testing.T) {
	if !zstdcli.Available() {
		t.Skip("zstd not installed; skipping compressed --project inclusion test")
	}
	home := fakeHome(t)
	cwd := "/Users/example/dev/zstproj"
	id := "eeee1111-2222-3333-4444-555566667777"
	writeCompressedSession(t, home.SessionsDir,
		"rollout-2026-06-13T18-22-01-"+id+".jsonl.zst",
		string(metaSessionBytes(id, cwd)))

	out := filepath.Join(t.TempDir(), "p.codexbundle")
	res, err := Export(home, ExportOptions{ProjectPath: cwd, OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1 (compressed session whose cwd matches)", res.IncludedCount)
	}
	if res.CompressedSkipped != 0 {
		t.Errorf("compressed skipped = %d, want 0 (cwd was recovered)", res.CompressedSkipped)
	}
	// The bundle must contain the compressed entry, copied byte-for-byte.
	files := readBundle(t, out)
	var foundZst bool
	for name := range files {
		if strings.HasSuffix(name, ".jsonl.zst") {
			foundZst = true
		}
	}
	if !foundZst {
		t.Errorf("bundle does not contain the compressed session")
	}
	// Its manifest entry should carry the recovered cwd and be flagged compressed.
	if len(res.Manifest.Sessions) != 1 {
		t.Fatalf("manifest sessions = %d", len(res.Manifest.Sessions))
	}
	ms := res.Manifest.Sessions[0]
	if !ms.Compressed {
		t.Errorf("manifest session should be marked compressed")
	}
	if ms.OriginalCWD != cwd {
		t.Errorf("manifest cwd = %q, want %q", ms.OriginalCWD, cwd)
	}
}
