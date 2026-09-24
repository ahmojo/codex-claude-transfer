package bundle

import (
	"encoding/json"
	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeClaudeGroup(t *testing.T) (claudehome.Home, string, []string) {
	t.Helper()
	home := fakeClaudeHome(t)
	cwd := "/source/project"
	writeClaudeTranscript(t, home, cwd, claudeID, "parent text")
	base := filepath.Join(home.ProjectsDir, claudehome.EncodeCWD(cwd))
	paths := []string{filepath.Join(base, claudeID+".jsonl")}
	for _, id := range []string{"a1", "a2"} {
		p := filepath.Join(base, claudeID, "subagents", "agent-"+id+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]any{"type": "user", "sessionId": claudeID, "cwd": cwd, "agentId": id, "uuid": "message-" + id, "isSidechain": true, "message": map[string]string{"content": "child marker " + id}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return home, cwd, paths
}

func TestClaudeConversationGroupRoundTrip(t *testing.T) {
	src, cwd, paths := writeClaudeGroup(t)
	writeClaudeTranscript(t, src, cwd, "bbbb-unrelated", "unrelated")
	// A deeper arbitrary JSONL file is not a transferable Claude transcript.
	stray := filepath.Join(src.ProjectsDir, claudehome.EncodeCWD(cwd), "arbitrary", "dump.jsonl")
	if err := os.MkdirAll(filepath.Dir(stray), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte(`{"sessionId":"aaaa-stray","cwd":"/source/project"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{claudeID, "aaaa1111"} {
		out := filepath.Join(t.TempDir(), "group.zip")
		exp, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, SessionID: prefix, OutputPath: out})
		if err != nil {
			t.Fatal(err)
		}
		if exp.IncludedCount != 3 {
			t.Fatalf("export included %d, want parent and two children", exp.IncludedCount)
		}
		dst := fakeClaudeHome(t)
		imp, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: out, SessionIDs: []string{claudeID}})
		if err != nil {
			t.Fatal(err)
		}
		if imp.Imported != 3 {
			t.Fatalf("imported %d, want 3", imp.Imported)
		}
		for _, p := range paths {
			rel, err := filepath.Rel(src.Root, p)
			if err != nil {
				t.Fatal(err)
			}
			if !sameFile(t, p, filepath.Join(dst.Root, rel)) {
				t.Fatalf("lost group member %s", rel)
			}
		}
	}
	// Selecting all must still exclude the arbitrary nested transcript.
	exp, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, OutputPath: filepath.Join(t.TempDir(), "all.zip")})
	if err != nil || exp.IncludedCount != 4 {
		t.Fatalf("all export: %d %v", exp.IncludedCount, err)
	}
}

func TestClaudeConversationGroupIncrementalSelection(t *testing.T) {
	src, _, paths := writeClaudeGroup(t)
	old := time.Now().Add(-time.Hour)
	for _, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(paths[2], now, now); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []ExportOptions{{Since: now.Add(-time.Minute)}, {Match: "child marker a2"}} {
		opts.Tool = agent.Claude
		opts.ClaudeHome = src
		opts.OutputPath = filepath.Join(t.TempDir(), "group.zip")
		exp, err := Export(codexhome.Home{}, opts)
		if err != nil || exp.IncludedCount != 3 {
			t.Fatalf("partial conversation: %d %v", exp.IncludedCount, err)
		}
	}
}

func TestClaudeConversationGroupMapAndCopyGuard(t *testing.T) {
	src, cwd, paths := writeClaudeGroup(t)
	out := filepath.Join(t.TempDir(), "group.zip")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	dst := fakeClaudeHome(t)
	newCWD := "/destination/project"
	opts := ImportOptions{BundlePath: out, MapCWD: []CWDMapping{{Old: cwd, New: newCWD}}}
	imp, err := Import(claudeImportHome(dst), opts)
	if err != nil || imp.Imported != 3 {
		t.Fatalf("map import: %+v %v", imp, err)
	}
	for _, p := range paths {
		rel, _ := filepath.Rel(filepath.Join(src.ProjectsDir, claudehome.EncodeCWD(cwd)), p)
		got, err := os.ReadFile(filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(newCWD), rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range nonEmptyLines(got) {
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				t.Fatal(err)
			}
			if obj["cwd"] != newCWD || obj["sessionId"] != claudeID {
				t.Fatalf("wrong identity/cwd: %s", line)
			}
		}
	}
	empty := fakeClaudeHome(t)
	opts.ImportAsCopy = true
	if _, err := Import(claudeImportHome(empty), opts); err == nil || !strings.Contains(err.Error(), "subagent") {
		t.Fatalf("expected explicit group copy rejection, got %v", err)
	}
	if _, err := os.Stat(empty.ProjectsDir); !os.IsNotExist(err) {
		t.Fatalf("rejected import wrote files: %v", err)
	}
}

func TestClaudeMappedImportCopyDestination(t *testing.T) {
	src := fakeClaudeHome(t)
	cwd := "/old/project"
	next := "/new/project"
	writeClaudeTranscript(t, src, cwd, claudeID, "source")
	out := filepath.Join(t.TempDir(), "plain.zip")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	dst := fakeClaudeHome(t)
	writeClaudeTranscript(t, dst, next, claudeID, "different local")
	res, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: out, ImportAsCopy: true, MapCWD: []CWDMapping{{Old: cwd, New: next}}})
	if err != nil || res.ImportedCopies != 1 {
		t.Fatalf("copy: %+v %v", res, err)
	}
	for _, it := range res.Items {
		if it.Copied && filepath.Dir(it.DestPath) != filepath.Join(dst.ProjectsDir, claudehome.EncodeCWD(next)) {
			t.Fatalf("copy went to old project: %s", it.DestPath)
		}
	}
}
