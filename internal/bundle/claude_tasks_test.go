package bundle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

func writeClaudeTask(t *testing.T, root, sessionID, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(root, "tasks", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeTaskImportGuards(t *testing.T) {
	src := fakeClaudeHome(t)
	writeClaudeTranscript(t, src, "/p", claudeID, "selected")
	const otherID = "bbbb1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, src, "/other", otherID, "unrelated")
	task := []byte(`{"id":"1","subject":"Task","status":"pending"}`)
	writeClaudeTask(t, src.Root, claudeID, "1.json", task)
	archive := filepath.Join(t.TempDir(), "task.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, OutputPath: archive}); err != nil {
		t.Fatal(err)
	}
	rel := "tasks/" + claudeID + "/1.json"
	t.Run("selection", func(t *testing.T) {
		dst := fakeClaudeHome(t)
		if _, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: archive, SessionIDs: []string{otherID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dst.Root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("deselected conversation's task imported: %v", err)
		}
	})
	t.Run("copy", func(t *testing.T) {
		dst := fakeClaudeHome(t)
		if _, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: archive, ImportAsCopy: true}); err == nil {
			t.Fatal("copy accepted without remapping task identity")
		}
		if len(listFilesRel(t, dst.Root)) != 0 {
			t.Fatal("rejected copy wrote files")
		}
	})
	t.Run("conflict", func(t *testing.T) {
		dst := fakeClaudeHome(t)
		local := []byte(`{"id":"1","subject":"Local task"}`)
		writeClaudeTask(t, dst.Root, claudeID, "1.json", local)
		res, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: archive})
		if err != nil || res.TaskConflicts != 1 {
			t.Fatalf("conflict result: %+v, %v", res, err)
		}
		if got, _ := os.ReadFile(filepath.Join(dst.Root, filepath.FromSlash(rel))); !bytes.Equal(got, local) {
			t.Fatal("local task overwritten")
		}
		res, err = Import(claudeImportHome(dst), ImportOptions{BundlePath: archive, ReplaceWithBackup: true})
		if err != nil || res.TasksImported != 1 {
			t.Fatalf("replace result: %+v, %v", res, err)
		}
		for _, item := range res.Items {
			if item.Task {
				if backup, err := os.ReadFile(item.BackupPath); err != nil || !bytes.Equal(backup, local) {
					t.Fatalf("task backup missing: %v", err)
				}
			}
		}
	})
	cases := []struct {
		name, body string
		mutate     func(*Manifest)
	}{
		{name: "array", body: `[]`},
		{name: "missing id", body: `{"subject":"missing id"}`},
		{name: "wrong id", body: `{"id":"2"}`},
		{name: "hidden", mutate: func(m *Manifest) { m.Tasks = nil }},
		{name: "unbound", mutate: func(m *Manifest) { m.Tasks[0].SessionID = otherID }},
		{name: "duplicate", mutate: func(m *Manifest) { m.Tasks = append(m.Tasks, m.Tasks[0]) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := readBundle(t, archive)
			var manifest Manifest
			if err := json.Unmarshal(files[ManifestName], &manifest); err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				files[rel] = []byte(tc.body)
				manifest.Tasks[0].SHA256 = sha256Hex(files[rel])
			}
			if tc.mutate != nil {
				tc.mutate(&manifest)
			}
			files[ManifestName], _ = json.Marshal(manifest)
			checksums := Checksums{}
			var entries []rawEntry
			for name, data := range files {
				if name != ChecksumsName {
					checksums[name] = sha256Hex(data)
					entries = append(entries, rawEntry{name, data})
				}
			}
			data, _ := json.Marshal(checksums)
			entries = append(entries, rawEntry{ChecksumsName, data})
			broken := filepath.Join(t.TempDir(), "invalid.codexbundle")
			writeBundleZip(t, broken, entries)
			dst := fakeClaudeHome(t)
			if _, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: broken}); err == nil {
				t.Fatal("unsupported task accepted")
			}
			if len(listFilesRel(t, dst.Root)) != 0 {
				t.Fatal("invalid task partially imported")
			}
		})
	}
}

func TestClaudeTaskSecretsAreScannedAndRedacted(t *testing.T) {
	src := fakeClaudeHome(t)
	writeClaudeTranscript(t, src, "/p", claudeID, "clean transcript")
	writeClaudeTask(t, src.Root, claudeID, "1.json", []byte(`{"id":"1","description":"AKIAIOSFODNN7EXAMPLE"}`))
	opts := ExportOptions{Tool: agent.Claude, ClaudeHome: src, OutputPath: filepath.Join(t.TempDir(), "task.codexbundle")}
	for _, redact := range []bool{false, true} {
		opts.Redact = redact
		if _, err := Export(codexhome.Home{}, opts); err != nil {
			t.Fatal(err)
		}
		scan, err := ScanBundleSecrets(opts.OutputPath)
		if err != nil || scan.Any() == redact {
			t.Fatalf("redact=%v: secret scan %+v, %v", redact, scan, err)
		}
		data := readBundle(t, opts.OutputPath)["tasks/"+claudeID+"/1.json"]
		if !json.Valid(data) || (redact && strings.Contains(string(data), "AKIAIOSFODNN7EXAMPLE")) {
			t.Fatalf("invalid redacted task: %s", data)
		}
	}
}

func TestClaudeTasksRoundTrip(t *testing.T) {
	src := fakeClaudeHome(t)
	writeClaudeTranscript(t, src, "/project", claudeID, "task context")
	task := []byte(`{"id":"1","subject":"Keep this task","status":"pending","blocks":[],"blockedBy":[],"futureField":true}`)
	writeClaudeTask(t, src.Root, claudeID, "1.json", task)
	writeClaudeTask(t, src.Root, claudeID, ".lock", nil)
	writeClaudeTask(t, src.Root, "unrelated", "1.json", task)
	archive := filepath.Join(t.TempDir(), "tasks.codexbundle")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, SessionID: claudeID, OutputPath: archive}); err != nil {
		t.Fatal(err)
	}
	rel := "tasks/" + claudeID + "/1.json"
	entries := readBundle(t, archive)
	if !bytes.Equal(entries[rel], task) {
		t.Fatal("selected conversation's task was not bundled unchanged")
	}
	for _, extra := range []string{"tasks/" + claudeID + "/.lock", "tasks/unrelated/1.json"} {
		if _, ok := entries[extra]; ok {
			t.Fatalf("export included %s", extra)
		}
	}
	dst := fakeClaudeHome(t)
	opts := ImportOptions{BundlePath: archive, DryRun: true, MapCWD: []CWDMapping{{Old: "/project", New: "/mapped"}}}
	if _, err := Import(claudeImportHome(dst), opts); err != nil {
		t.Fatal(err)
	}
	if got := listFilesRel(t, dst.Root); len(got) != 0 {
		t.Fatalf("dry run wrote %v", got)
	}
	opts.DryRun = false
	if _, err := Import(claudeImportHome(dst), opts); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst.Root, filepath.FromSlash(rel)))
	if err != nil || !bytes.Equal(got, task) {
		t.Fatalf("task state lost on import: %s, %v", got, err)
	}
	if _, err := Import(claudeImportHome(dst), opts); err != nil {
		t.Fatal(err)
	}
}
