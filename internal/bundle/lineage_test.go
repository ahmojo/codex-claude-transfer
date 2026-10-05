package bundle

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexForkDependencies(t *testing.T) {
	const parentID = "11111111-1111-4111-8111-111111111111"
	const forkID = "22222222-2222-4222-8222-222222222222"
	const rootID = "33333333-3333-4333-8333-333333333333"
	source := fakeHome(t)
	parentName := "rollout-2026-10-05T00-00-00-" + parentID + ".jsonl"
	forkName := "rollout-2026-10-05T00-00-01-" + forkID + ".jsonl"
	parentRel := "archived_sessions/" + parentName
	forkRel := "sessions/2026/10/05/" + forkName
	writeSession(t, source.Root, parentRel, "logical-parent-id", "/old")
	writeSession(t, source.Root, "sessions/2026/10/05/rollout-2026-10-05T00-00-00-"+rootID+".jsonl", rootID, "/old")
	parentPath := filepath.Join(source.Root, filepath.FromSlash(parentRel))
	parent, err := os.ReadFile(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	parent = bytes.Replace(parent, []byte(`"cwd":`), []byte(`"history_base":{"thread_id":"`+rootID+`","end_byte_offset":123,"end_ordinal_exclusive":2},"cwd":`), 1)
	if err := os.WriteFile(parentPath, parent, 0o644); err != nil {
		t.Fatal(err)
	}
	fork := []byte(`{"type":"session_meta","payload":{"id":"` + forkID + `","cwd":"/old","history_base":{"thread_id":"` + parentID + `","end_byte_offset":123,"end_ordinal_exclusive":2}}}` + "\n")
	if err := os.MkdirAll(filepath.Join(source.Root, "sessions/2026/10/05"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.Root, filepath.FromSlash(forkRel)), fork, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fork.codexbundle")
	res, err := Export(source, ExportOptions{SessionID: forkID, OutputPath: output})
	if err != nil || res.IncludedCount != 3 {
		t.Fatalf("dependency export: %+v, %v", res, err)
	}
	for _, opts := range []ImportOptions{
		{}, {IncludeArchived: true, ImportAsCopy: true},
		{IncludeArchived: true, MapCWD: []CWDMapping{{Old: "/old", New: "/new"}}},
		{IncludeArchived: true, SessionIDs: []string{forkID}},
	} {
		target := fakeHome(t)
		opts.BundlePath = output
		if _, err := Import(target, opts); err == nil {
			t.Fatalf("unsafe fork import accepted: %+v", opts)
		}
		if _, err := os.Stat(filepath.Join(target.Root, filepath.FromSlash(forkRel))); !os.IsNotExist(err) {
			t.Fatal("unsafe import wrote a fork")
		}
	}
	target := fakeHome(t)
	if res, err := Import(target, ImportOptions{BundlePath: output, IncludeArchived: true}); err != nil || res.Imported != 3 {
		t.Fatalf("complete fork import: %+v, %v", res, err)
	}
	if got, _ := os.ReadFile(filepath.Join(target.Root, filepath.FromSlash(forkRel))); !bytes.Equal(got, fork) {
		t.Fatal("fork bytes changed")
	}
	conflicted := fakeHome(t)
	writeSession(t, conflicted.Root, parentRel, parentID, "/different")
	if _, err := Import(conflicted, ImportOptions{BundlePath: output, IncludeArchived: true}); err == nil {
		t.Fatal("divergent parent accepted")
	}
	if _, err := os.Stat(filepath.Join(conflicted.Root, filepath.FromSlash(forkRel))); !os.IsNotExist(err) {
		t.Fatal("conflict wrote a fork")
	}
	for _, opts := range []ExportOptions{{Redact: true}, {StripImages: true}} {
		opts.SessionID, opts.OutputPath = forkID, filepath.Join(t.TempDir(), "unsafe.codexbundle")
		if _, err := Export(source, opts); err == nil {
			t.Fatal("unsafe history transformation accepted")
		}
		if _, err := os.Stat(opts.OutputPath); !os.IsNotExist(err) {
			t.Fatal("unsafe export wrote a bundle")
		}
	}
}

func TestMapCodexRuntimeSettings(t *testing.T) {
	original := []byte(strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"id","cwd":"/old","runtime_workspace_roots":["/old","/other"],"unknown":true}}`,
		`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"cwd":"/old","runtime_workspace_roots":["/old","/other"],"model":"keep"}}}`,
		mapRespLine,
	}, "\r\n"))
	mapped, changed, err := rewriteSessionMetaCWD(original, "/old", "/new")
	if err != nil || !changed {
		t.Fatalf("rewrite: %v", err)
	}
	if err := validateMappedJSONL(original, mapped, "/new"); err != nil {
		t.Fatal(err)
	}
	lines := splitKeepTerminators(mapped)
	for _, line := range lines[:2] {
		if bytes.Contains(line.text, []byte(`"/old"`)) || !bytes.Contains(line.text, []byte(`"/other"`)) {
			t.Fatalf("wrong runtime paths: %s", line.text)
		}
	}
	if !bytes.Equal(lines[2].text, []byte(mapRespLine)) {
		t.Fatal("conversation changed")
	}
	var meta map[string]any
	if err := json.Unmarshal(lines[0].text, &meta); err != nil {
		t.Fatal(err)
	}
	if err := validateMappedJSONL(original, bytes.ReplaceAll(mapped, []byte("keep"), []byte("changed")), "/new"); err == nil {
		t.Fatal("unrelated settings mutation accepted")
	}
}
