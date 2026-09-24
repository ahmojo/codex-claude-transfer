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

// writeClaudeGroupInSubfolder writes a conversation whose second subagent
// worked in a subfolder of the project, as real Claude Code subagents do after
// a cd. It returns the bundle of that conversation.
func writeClaudeGroupInSubfolder(t *testing.T) (bundlePath, cwd, subCWD string) {
	t.Helper()
	src := fakeClaudeHome(t)
	cwd, subCWD = "/source/project", "/source/project/sub"
	writeClaudeTranscript(t, src, cwd, claudeID, "parent text")
	base := filepath.Join(src.ProjectsDir, claudehome.EncodeCWD(cwd))
	for id, agentCWD := range map[string]string{"a1": cwd, "a2": subCWD} {
		p := filepath.Join(base, claudeID, "subagents", "agent-"+id+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]any{"type": "user", "sessionId": claudeID, "cwd": agentCWD, "agentId": id, "isSidechain": true, "message": map[string]string{"content": "child " + id}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bundlePath = filepath.Join(t.TempDir(), "group.zip")
	if _, err := Export(codexhome.Home{}, ExportOptions{Tool: agent.Claude, ClaudeHome: src, SessionID: claudeID, OutputPath: bundlePath}); err != nil {
		t.Fatal(err)
	}
	return bundlePath, cwd, subCWD
}

// groupFolders returns the project folders that hold any file of claudeID.
func groupFolders(t *testing.T, home claudehome.Home) []string {
	t.Helper()
	var folders []string
	entries, err := os.ReadDir(home.ProjectsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(home.ProjectsDir, e.Name(), claudeID)); err == nil {
			folders = append(folders, e.Name())
			continue
		}
		if _, err := os.Stat(filepath.Join(home.ProjectsDir, e.Name(), claudeID+".jsonl")); err == nil {
			folders = append(folders, e.Name())
		}
	}
	return folders
}

// A subagent that recorded a subfolder as its cwd must still land next to its
// parent when the project is remapped; Claude Code looks for it there.
func TestMapCWDKeepsSubagentInSubfolderWithParent(t *testing.T) {
	bundlePath, cwd, subCWD := writeClaudeGroupInSubfolder(t)
	dst := fakeClaudeHome(t)
	newCWD := "/moved/project"
	res, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: bundlePath, MapCWD: []CWDMapping{{Old: cwd, New: newCWD}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 {
		t.Fatalf("imported %d, want 3", res.Imported)
	}
	want := claudehome.EncodeCWD(newCWD)
	if got := groupFolders(t, dst); len(got) != 1 || got[0] != want {
		t.Fatalf("conversation spread over %v, want only %s", got, want)
	}
	sub, err := os.ReadFile(filepath.Join(dst.ProjectsDir, want, claudeID, "subagents", "agent-a2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Only exact cwd matches are rewritten, so the subfolder path stays as it was.
	subJSON, _ := json.Marshal(subCWD)
	if !strings.Contains(string(sub), `"cwd":`+string(subJSON)) {
		t.Errorf("subfolder subagent content changed: %s", sub)
	}
}

// Without a mapping, nothing moves: the whole conversation keeps its folder.
func TestSubagentInSubfolderStaysWithUnmappedParent(t *testing.T) {
	bundlePath, cwd, _ := writeClaudeGroupInSubfolder(t)
	dst := fakeClaudeHome(t)
	if _, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: bundlePath, MapCWD: []CWDMapping{{Old: "/some/other", New: "/elsewhere"}}}); err != nil {
		t.Fatal(err)
	}
	if got := groupFolders(t, dst); len(got) != 1 || got[0] != claudehome.EncodeCWD(cwd) {
		t.Fatalf("conversation spread over %v", got)
	}
}

// --map-cwd-here names the project by the parent transcripts, so a subagent in
// a subfolder does not make a single conversation look like two projects.
func TestMapCWDHereIgnoresSubagentSubfolders(t *testing.T) {
	bundlePath, _, _ := writeClaudeGroupInSubfolder(t)
	dst := fakeClaudeHome(t)
	here := "/new/here"
	if _, err := Import(claudeImportHome(dst), ImportOptions{BundlePath: bundlePath, MapCWDHere: true, HereDir: here}); err != nil {
		t.Fatalf("map-cwd-here: %v", err)
	}
	if got := groupFolders(t, dst); len(got) != 1 || got[0] != claudehome.EncodeCWD(here) {
		t.Fatalf("conversation spread over %v, want only %s", got, claudehome.EncodeCWD(here))
	}
}

func TestConversationSessions(t *testing.T) {
	parent := ManifestSession{BundlePath: "projects/-p/" + claudeID + ".jsonl", OriginalCWD: "/p"}
	child := ManifestSession{BundlePath: "projects/-p/" + claudeID + "/subagents/agent-a.jsonl", OriginalCWD: "/p/sub"}
	codex := ManifestSession{BundlePath: "sessions/2026/06/13/rollout-x.jsonl", OriginalCWD: "/c"}
	if got := ConversationSessions([]ManifestSession{parent, child, codex}); len(got) != 2 || got[0] != parent || got[1] != codex {
		t.Errorf("got %+v, want parent and codex entries", got)
	}
	if got := ConversationSessions([]ManifestSession{child}); len(got) != 1 {
		t.Errorf("a bundle of only subagents must keep them: %+v", got)
	}
}
