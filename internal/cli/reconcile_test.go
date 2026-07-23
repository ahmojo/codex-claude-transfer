package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexreconcile"
)

func TestParseReconcileFlag(t *testing.T) {
	f, err := parseFlags([]string{"bundle.codexbundle", "--reconcile"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.reconcile {
		t.Fatal("--reconcile was not parsed")
	}
}

func TestRunImportRejectsReconcileDryRunBeforeOpeningBundle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"import", "missing.codexbundle", "--reconcile", "--dry-run"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot be combined") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestAffectedCodexThreadIDs(t *testing.T) {
	root := t.TempDir()
	const (
		actualA    = "aaaaaaaa-1111-4111-8111-111111111111"
		actualCopy = "cccccccc-3333-4333-8333-333333333333"
	)
	writeSession(t, root, actualA, "/project/a")
	writeSession(t, root, actualCopy, "/project/copy")
	home, err := codexhome.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	pathFor := func(id string) string {
		return filepath.Join(root, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	}
	res := bundle.ImportResult{
		Manifest: bundle.Manifest{Sessions: []bundle.ManifestSession{
			// Deliberately wrong: reconciliation must trust the imported rollout's
			// session_meta.id, not attacker-controlled manifest metadata.
			{BundlePath: "sessions/a.jsonl", ThreadID: "manifest-a"},
			{BundlePath: "sessions/b.jsonl", ThreadID: "manifest-b"},
			{BundlePath: "sessions/legacy.jsonl"},
		}},
		Items: []bundle.ImportItem{
			{BundlePath: "sessions/a.jsonl", DestPath: pathFor(actualA), Action: bundle.ActionImport},
			{BundlePath: "sessions/a.jsonl", DestPath: pathFor(actualA), Action: bundle.ActionUpdate},
			{BundlePath: "sessions/b.jsonl", Action: bundle.ActionConflict},
			{BundlePath: "sessions/copy.jsonl", DestPath: pathFor(actualCopy), Action: bundle.ActionImportCopy, NewThreadID: "manifest-copy"},
			{BundlePath: "sessions/legacy.jsonl", DestPath: filepath.Join(root, "missing.jsonl"), Action: bundle.ActionReplace},
		},
	}
	changed, err := codexreconcile.ThreadsChangedByImport(home, res)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(changed.IDs, ","); got != actualA+","+actualCopy {
		t.Fatalf("ids = %q", got)
	}
	if changed.Unknown != 1 {
		t.Fatalf("unknown = %d", changed.Unknown)
	}
}

func TestHelpDocumentsReconcileSafetyBoundary(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d", code)
	}
	text := out.String()
	for _, want := range []string{"--reconcile", "short-lived native Codex app-server", "never writes SQLite/session_index"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q", want)
		}
	}
}

func TestPrintPostImportReconcileFallback(t *testing.T) {
	var out bytes.Buffer
	printPostImportReconcile(&out, postImportReconcile{
		Requested: true,
		CodexHome: `C:\synthetic codex`,
		ThreadIDs: []string{"aaaa1111-2222-3333-4444-555566667777"},
		Result:    codexreconcile.Result{Version: "0.145.0-alpha.30"},
		Err:       errors.New("method not found"),
	})
	text := out.String()
	for _, want := range []string{
		"imported rollout files are intact",
		"restart the Codex App",
		"cct resume aaaa1111-2222-3333-4444-555566667777 --run",
		`--codex-home "C:\synthetic codex"`,
		"did not write Codex SQLite or session_index.jsonl",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fallback missing %q:\n%s", want, text)
		}
	}
}

func TestImportReconcileWithInstalledCodexIntegration(t *testing.T) {
	if os.Getenv("CCT_CODEX_INTEGRATION") != "1" {
		t.Skip("set CCT_CODEX_INTEGRATION=1 to test an installed Codex app-server")
	}
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	const id = "33333333-3333-4333-8333-333333333333"
	writeReconcileCLIFixture(t, sourceHome, id)
	bundlePath := filepath.Join(t.TempDir(), "reconcile.codexbundle")

	var exportOut, exportErr bytes.Buffer
	if code := Run([]string{"export", "--session", id, "--codex-home", sourceHome, "--output", bundlePath}, &exportOut, &exportErr); code != 0 {
		t.Fatalf("export code = %d, stderr = %s", code, exportErr.String())
	}
	sentinelPath := filepath.Join(targetHome, "codex-state.sqlite")
	if err := os.WriteFile(sentinelPath, []byte("DO NOT MODIFY"), 0o644); err != nil {
		t.Fatal(err)
	}

	var importOut, importErr bytes.Buffer
	if code := Run([]string{"import", bundlePath, "--codex-home", targetHome, "--reconcile"}, &importOut, &importErr); code != 0 {
		t.Fatalf("import code = %d, stdout = %s, stderr = %s", code, importOut.String(), importErr.String())
	}
	if !strings.Contains(importOut.String(), "Codex discovery: verified 1/1") {
		t.Fatalf("missing reconcile verification: %s", importOut.String())
	}
	if !strings.Contains(importOut.String(), "cct did not write Codex SQLite or session_index.jsonl") {
		t.Fatalf("missing safety boundary: %s", importOut.String())
	}
	got, err := os.ReadFile(sentinelPath)
	if err != nil || string(got) != "DO NOT MODIFY" {
		t.Fatalf("SQLite sentinel changed: %q, %v", got, err)
	}
}

func writeReconcileCLIFixture(t *testing.T, home, id string) {
	t.Helper()
	day := filepath.Join(home, "sessions", "2026", "07", "23")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "rollout-2026-07-23T11-00-00-"+id+".jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(file)
	lines := []map[string]any{
		{
			"timestamp": "2026-07-23T11:00:00Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id": id, "timestamp": "2026-07-23T11:00:00Z", "cwd": home,
				"originator": "cct-test", "cli_version": "test", "source": "cli", "model_provider": "openai",
			},
		},
		{
			"timestamp": "2026-07-23T11:00:01Z",
			"type":      "event_msg",
			"payload":   map[string]any{"type": "user_message", "message": "synthetic CLI reconcile"},
		},
		{
			"timestamp": "2026-07-23T11:00:01Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message", "role": "user",
				"content": []map[string]string{{"type": "input_text", "text": "synthetic CLI reconcile"}},
			},
		},
	}
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
