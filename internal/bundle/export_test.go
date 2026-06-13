package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
)

func fakeHome(t *testing.T) codexhome.Home {
	t.Helper()
	home, err := codexhome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return home
}

func writeSession(t *testing.T, dir, name, threadID, cwd string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":` + string(cwdJSON) + `,"source":"cli","model_provider":"openai","cli_version":"1.2.3"}}` + "\n" +
		`{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n"
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readBundle opens a .codexbundle ZIP and returns a map of file name -> bytes.
func readBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		out[f.Name] = data
	}
	return out
}

func TestExportCreatesValidBundle(t *testing.T) {
	home := fakeHome(t)
	project := "/Users/example/dev/project"
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", project)

	out := filepath.Join(t.TempDir(), "project.codexbundle")
	result, err := Export(home, ExportOptions{ProjectPath: project, OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if result.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1", result.IncludedCount)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("bundle not written: %v", err)
	}

	files := readBundle(t, out)
	if _, ok := files[ManifestName]; !ok {
		t.Errorf("missing %s", ManifestName)
	}
	if _, ok := files[ChecksumsName]; !ok {
		t.Errorf("missing %s", ChecksumsName)
	}
	wantSession := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl"
	if _, ok := files[wantSession]; !ok {
		t.Errorf("missing session file %q; have %v", wantSession, keys(files))
	}
}

func TestExportManifestCorrect(t *testing.T) {
	home := fakeHome(t)
	project := "/proj/x"
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-bbbb1111-2222-3333-4444-555566667777.jsonl",
		"bbbb1111-2222-3333-4444-555566667777", project)

	out := filepath.Join(t.TempDir(), "x.codexbundle")
	if _, err := Export(home, ExportOptions{ProjectPath: project, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}

	files := readBundle(t, out)
	var m Manifest
	if err := json.Unmarshal(files[ManifestName], &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.FormatVersion != FormatVersion {
		t.Errorf("format version = %q", m.FormatVersion)
	}
	if m.SourceCodexHome != home.Root {
		t.Errorf("source codex home = %q", m.SourceCodexHome)
	}
	if m.SourceProjectPath != project {
		t.Errorf("source project = %q", m.SourceProjectPath)
	}
	if m.CodexVersion != "1.2.3" {
		t.Errorf("codex version = %q", m.CodexVersion)
	}
	if len(m.Sessions) != 1 {
		t.Fatalf("sessions = %d", len(m.Sessions))
	}
	s := m.Sessions[0]
	if s.ThreadID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("thread id = %q", s.ThreadID)
	}
	if s.OriginalCWD != project {
		t.Errorf("original cwd = %q", s.OriginalCWD)
	}
	if s.ModelProvider != "openai" {
		t.Errorf("model provider = %q", s.ModelProvider)
	}
	if s.SHA256 == "" || s.SizeBytes == 0 {
		t.Errorf("missing checksum/size: %+v", s)
	}
}

func TestExportChecksumsCorrect(t *testing.T) {
	home := fakeHome(t)
	project := "/proj/y"
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	name := "rollout-2026-06-13T18-22-01-cccc1111-2222-3333-4444-555566667777.jsonl"
	writeSession(t, day, name, "cccc1111-2222-3333-4444-555566667777", project)

	out := filepath.Join(t.TempDir(), "y.codexbundle")
	if _, err := Export(home, ExportOptions{ProjectPath: project, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}

	files := readBundle(t, out)
	var sums Checksums
	if err := json.Unmarshal(files[ChecksumsName], &sums); err != nil {
		t.Fatalf("unmarshal checksums: %v", err)
	}
	// checksums.json must not reference itself.
	if _, ok := sums[ChecksumsName]; ok {
		t.Errorf("checksums.json should not include itself")
	}
	// Every other bundle file must have a matching, correct checksum.
	for name, data := range files {
		if name == ChecksumsName {
			continue
		}
		want, ok := sums[name]
		if !ok {
			t.Errorf("checksums missing entry for %q", name)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("checksum mismatch for %q: got %s want %s", name, got, want)
		}
	}
}

func TestExportCwdFilterExcludesOtherProjects(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-dddd1111-2222-3333-4444-555566667777.jsonl",
		"dddd1111-2222-3333-4444-555566667777", "/proj/wanted")
	writeSession(t, day, "rollout-2026-06-13T19-00-00-eeee1111-2222-3333-4444-555566667777.jsonl",
		"eeee1111-2222-3333-4444-555566667777", "/proj/other")

	out := filepath.Join(t.TempDir(), "wanted.codexbundle")
	result, err := Export(home, ExportOptions{ProjectPath: "/proj/wanted", OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if result.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1 (cwd filter)", result.IncludedCount)
	}
	if result.Manifest.Sessions[0].OriginalCWD != "/proj/wanted" {
		t.Errorf("wrong session exported: %q", result.Manifest.Sessions[0].OriginalCWD)
	}
}

func TestExportNoMatchingSessionsErrors(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-ffff1111-2222-3333-4444-555566667777.jsonl",
		"ffff1111-2222-3333-4444-555566667777", "/proj/a")

	out := filepath.Join(t.TempDir(), "none.codexbundle")
	_, err := Export(home, ExportOptions{ProjectPath: "/proj/does-not-match", OutputPath: out})
	if err == nil {
		t.Fatalf("expected error when no sessions match")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Errorf("no bundle should be written when nothing matches")
	}
}

func TestExportCompressedSkippedByCwdFilter(t *testing.T) {
	home := fakeHome(t)
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	// A plain session that matches, plus a compressed one (cwd unknown).
	writeSession(t, day, "rollout-2026-06-13T18-22-01-11110000-2222-3333-4444-555566667777.jsonl",
		"11110000-2222-3333-4444-555566667777", "/proj/z")
	zst := filepath.Join(day, "rollout-2026-06-13T19-00-00-22220000-2222-3333-4444-555566667777.jsonl.zst")
	if err := os.WriteFile(zst, []byte("opaque zstd bytes"), 0o644); err != nil {
		t.Fatalf("write zst: %v", err)
	}

	out := filepath.Join(t.TempDir(), "z.codexbundle")
	result, err := Export(home, ExportOptions{ProjectPath: "/proj/z", OutputPath: out})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if result.IncludedCount != 1 {
		t.Errorf("included = %d, want 1", result.IncludedCount)
	}
	if result.CompressedSkipped != 1 {
		t.Errorf("compressed skipped = %d, want 1", result.CompressedSkipped)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
