package bundle

import (
	"path/filepath"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

func TestTranslateCodexBundleToClaude(t *testing.T) {
	// Build a Codex bundle with one session.
	src := fakeHome(t)
	project := `C:\Users\example\dev\proj`
	day := filepath.Join(src.SessionsDir, "2026", "06", "16")
	writeSession(t, day, "rollout-2026-06-16T10-00-00-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", project)
	out := filepath.Join(t.TempDir(), "cx.codexbundle")
	if _, err := Export(src, ExportOptions{ProjectPath: project, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Translate it into a fresh Claude home.
	ch, _ := claudehome.Detect(t.TempDir())
	clHome := codexhome.Home{Root: ch.Root, SessionsDir: ch.ProjectsDir}
	res, err := TranslateImport(clHome, TranslateOptions{BundlePath: out, TargetTool: agent.Claude})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if res.SourceTool != agent.Codex || res.TargetTool != agent.Claude {
		t.Fatalf("tools = %s -> %s", res.SourceTool, res.TargetTool)
	}
	if res.Translated != 1 {
		t.Fatalf("translated = %d, want 1", res.Translated)
	}

	// The synthesized transcript must be a discoverable Claude session.
	scan, err := claudesessions.Scan(ch, claudesessions.ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scan.Sessions) != 1 {
		t.Fatalf("claude sessions = %d, want 1", len(scan.Sessions))
	}
	got := scan.Sessions[0]
	if got.CWD != project {
		t.Errorf("translated cwd = %q, want %q", got.CWD, project)
	}
	if got.Preview == "" {
		t.Error("translated session has no preview")
	}

	// Re-translating is an idempotent skip (deterministic output).
	res2, err := TranslateImport(clHome, TranslateOptions{BundlePath: out, TargetTool: agent.Claude})
	if err != nil {
		t.Fatalf("retranslate: %v", err)
	}
	if res2.Translated != 0 || res2.SkippedExisting != 1 {
		t.Fatalf("retranslate translated=%d skipped=%d, want 0/1", res2.Translated, res2.SkippedExisting)
	}
}

func TestTranslateClaudeBundleToCodex(t *testing.T) {
	src := fakeClaudeHome(t)
	cwd := `C:\Users\example\dev\proj`
	writeClaudeTranscript(t, src, cwd, "bbbb1111-2222-3333-4444-555566667777", "build a parser")
	out := filepath.Join(t.TempDir(), "cl.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, ProjectPath: cwd, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}

	cxHome, _ := codexhome.Detect(t.TempDir())
	res, err := TranslateImport(cxHome, TranslateOptions{BundlePath: out, TargetTool: agent.Codex})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if res.Translated != 1 {
		t.Fatalf("translated = %d, want 1", res.Translated)
	}
	scan, err := sessions.Scan(cxHome, sessions.ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan.Valid != 1 {
		t.Fatalf("valid codex sessions = %d, want 1", scan.Valid)
	}
	if scan.Sessions[0].CWD != cwd {
		t.Errorf("translated cwd = %q, want %q", scan.Sessions[0].CWD, cwd)
	}
}

func TestTranslateRejectsSameTool(t *testing.T) {
	src := fakeHome(t)
	project := `C:\p`
	day := filepath.Join(src.SessionsDir, "2026", "06", "16")
	writeSession(t, day, "rollout-2026-06-16T10-00-00-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", project)
	out := filepath.Join(t.TempDir(), "cx.codexbundle")
	if _, err := Export(src, ExportOptions{ProjectPath: project, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := TranslateImport(src, TranslateOptions{BundlePath: out, TargetTool: agent.Codex}); err == nil {
		t.Fatal("expected error translating Codex -> Codex")
	}
}
