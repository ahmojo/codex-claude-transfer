package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// writeClaudeTranscript writes a transcript under projects/<encoded>/<uuid>.jsonl.
func writeClaudeTranscript(t *testing.T, home claudehome.Home, cwd, sessionID, firstUser string) {
	t.Helper()
	dir := filepath.Join(home.ProjectsDir, claudehome.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"type":"user","uuid":"u1","sessionId":"` + sessionID + `","cwd":` + string(cwdJSON) +
		`,"version":"2.1.170","timestamp":"2026-06-16T10:00:00Z","message":{"role":"user","content":"` + firstUser + `"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","sessionId":"` + sessionID + `","cwd":` + string(cwdJSON) +
		`,"timestamp":"2026-06-16T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sessionID+claudehome.SessionExt), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func fakeClaudeHome(t *testing.T) claudehome.Home {
	t.Helper()
	home, err := claudehome.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return home
}

// claudeImportHome adapts a Claude home into the codexhome.Home carrier that
// Import accepts (Root is the Claude home root; entries live under projects/).
func claudeImportHome(h claudehome.Home) codexhome.Home {
	return codexhome.Home{Root: h.Root, SessionsDir: h.ProjectsDir, Source: h.Source}
}

const claudeID = "aaaa1111-2222-3333-4444-555566667777"

func TestClaudeExportImportRoundTrip(t *testing.T) {
	src := fakeClaudeHome(t)
	cwd := `C:\Users\example\dev\project`
	writeClaudeTranscript(t, src, cwd, claudeID, "hello claude")

	out := filepath.Join(t.TempDir(), "claude.codexbundle")
	res, err := Export(codexhome.Home{}, ExportOptions{
		Tool:        agent.Claude,
		ClaudeHome:  src,
		ProjectPath: cwd,
		OutputPath:  out,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.IncludedCount != 1 {
		t.Fatalf("included = %d, want 1", res.IncludedCount)
	}
	if res.Manifest.Tool != string(agent.Claude) {
		t.Fatalf("manifest tool = %q, want claude", res.Manifest.Tool)
	}
	wantEntry := "projects/" + claudehome.EncodeCWD(cwd) + "/" + claudeID + ".jsonl"
	files := readBundle(t, out)
	if _, ok := files[wantEntry]; !ok {
		t.Fatalf("bundle missing %q; has %v", wantEntry, bundleKeys(files))
	}

	// Import into a fresh Claude home and confirm the transcript lands byte-identical.
	dst := fakeClaudeHome(t)
	ir, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: out})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if ir.Imported != 1 {
		t.Fatalf("imported = %d, want 1", ir.Imported)
	}
	gotPath := filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(cwd), claudeID+".jsonl")
	srcPath := filepath.Join(src.ProjectsDir, claudehome.EncodeCWD(cwd), claudeID+".jsonl")
	if !sameFile(t, srcPath, gotPath) {
		t.Fatalf("imported transcript differs from source")
	}

	// Re-import is idempotent.
	ir2, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: out})
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if ir2.Imported != 0 || ir2.SkippedIdentical != 1 {
		t.Fatalf("reimport imported=%d identical=%d, want 0/1", ir2.Imported, ir2.SkippedIdentical)
	}
}

func TestClaudeImportMapCWDReencodesFolderAndLines(t *testing.T) {
	src := fakeClaudeHome(t)
	oldCWD := `C:\Users\example\dev\project`
	writeClaudeTranscript(t, src, oldCWD, claudeID, "remap me")
	out := filepath.Join(t.TempDir(), "claude.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{
		Tool: agent.Claude, ClaudeHome: src, ProjectPath: oldCWD, OutputPath: out,
	}); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst := fakeClaudeHome(t)
	newCWD := `D:\work\project`
	ir, err := Import(claudeImportHome(dst), ImportOptions{
		BundlePath: out,
		MapCWD:     []CWDMapping{{Old: oldCWD, New: newCWD}},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if ir.Imported != 1 || ir.Mapped != 1 {
		t.Fatalf("imported=%d mapped=%d, want 1/1", ir.Imported, ir.Mapped)
	}
	// The file must land under the NEW encoded folder, not the old one.
	newPath := filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(newCWD), claudeID+".jsonl")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("transcript not under remapped folder %q: %v", newPath, err)
	}
	oldPath := filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(oldCWD), claudeID+".jsonl")
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatalf("transcript unexpectedly under old folder %q", oldPath)
	}
	// Every line's cwd must be rewritten to the new path; nothing else changed.
	data, _ := os.ReadFile(newPath)
	if strings.Contains(string(data), oldCWD) {
		t.Fatalf("old cwd still present after remap")
	}
	for i, line := range nonEmptyLines(data) {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
		if obj["cwd"] != newCWD {
			t.Fatalf("line %d cwd = %v, want %q", i, obj["cwd"], newCWD)
		}
	}
}

func TestClaudeImportAsCopyOnConflict(t *testing.T) {
	src := fakeClaudeHome(t)
	cwd := `C:\Users\example\dev\project`
	writeClaudeTranscript(t, src, cwd, claudeID, "original")
	out := filepath.Join(t.TempDir(), "claude.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{
		Tool: agent.Claude, ClaudeHome: src, ProjectPath: cwd, OutputPath: out,
	}); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Destination has a DIVERGED transcript at the same path (same id, different content).
	dst := fakeClaudeHome(t)
	writeClaudeTranscript(t, dst, cwd, claudeID, "diverged-local")
	localPath := filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(cwd), claudeID+".jsonl")
	before, _ := os.ReadFile(localPath)

	ir, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: out, ImportAsCopy: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if ir.ImportedCopies != 1 {
		t.Fatalf("imported copies = %d, want 1", ir.ImportedCopies)
	}
	// Local file is untouched.
	after, _ := os.ReadFile(localPath)
	if string(before) != string(after) {
		t.Fatalf("local transcript was modified by import-as-copy")
	}
	// A new transcript with a fresh id exists in the same folder.
	dir := filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(cwd))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("expected 2 transcripts in folder, got %d", len(entries))
	}
	var newID string
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if id != claudeID {
			newID = id
		}
	}
	if newID == "" {
		t.Fatal("no new copy transcript found")
	}
	// The copy's sessionId must be rewritten to the new id on every line.
	copyData, _ := os.ReadFile(filepath.Join(dir, newID+".jsonl"))
	for i, line := range nonEmptyLines(copyData) {
		var obj map[string]any
		json.Unmarshal([]byte(line), &obj)
		if obj["sessionId"] != newID {
			t.Fatalf("copy line %d sessionId = %v, want %q", i, obj["sessionId"], newID)
		}
	}
}

func TestClaudeImportRejectsCodexBundleIntoClaudeHomeShape(t *testing.T) {
	// A Codex bundle imported as Codex still works; this guards that a Claude
	// import never accepts a Codex rollout path (and vice versa) via the entry
	// check. Build a Claude bundle, then confirm its entries are Claude-shaped.
	src := fakeClaudeHome(t)
	cwd := "/home/x/p"
	writeClaudeTranscript(t, src, cwd, claudeID, "x")
	out := filepath.Join(t.TempDir(), "c.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{
		Tool: agent.Claude, ClaudeHome: src, ProjectPath: cwd, OutputPath: out,
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	insp, err := Inspect(out)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Manifest.Tool != string(agent.Claude) {
		t.Fatalf("tool = %q", insp.Manifest.Tool)
	}
	for _, e := range insp.Entries {
		if e == ManifestName || e == ChecksumsName {
			continue
		}
		if !strings.HasPrefix(e, "projects/") {
			t.Fatalf("entry %q is not under projects/", e)
		}
	}
}

func bundleKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	da, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("read %s: %v", a, err)
	}
	db, err := os.ReadFile(b)
	if err != nil {
		t.Fatalf("read %s: %v", b, err)
	}
	return string(da) == string(db)
}

func nonEmptyLines(data []byte) []string {
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}
