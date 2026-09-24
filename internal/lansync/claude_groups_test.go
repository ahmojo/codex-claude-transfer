package lansync

import (
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaudeGroupManifestTracksEveryMember(t *testing.T) {
	root := t.TempDir()
	makeSession := func(rel, body string) sessions.Session {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return sessions.Session{RelPath: rel, Path: p, ThreadID: "parent", CWD: "/project", SizeBytes: int64(len(body))}
	}
	parent := makeSession("-project/parent.jsonl", "parent")
	first := makeSession("-project/parent/subagents/agent-a.jsonl", "first")
	second := makeSession("-project/parent/subagents/agent-b.jsonl", "second")
	all := []sessions.Session{parent, first, second}
	m, err := buildClaudeManifest(all, "")
	if err != nil || len(m.Sessions) != 1 {
		t.Fatalf("group manifest: %+v %v", m, err)
	}
	reversed, err := buildClaudeManifest([]sessions.Session{second, first, parent}, "")
	if err != nil || !reflect.DeepEqual(m, reversed) {
		t.Fatalf("scan order changed fingerprint: %+v %v", reversed, err)
	}
	if len(computeOffer(m.Sessions, reversed.Sessions)) != 0 {
		t.Fatal("identical groups do not converge")
	}
	for _, s := range all {
		orig, err := os.ReadFile(s.Path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Path, append(orig, '!'), 0600); err != nil {
			t.Fatal(err)
		}
		grown, err := buildClaudeManifest(all, "")
		if err != nil || len(computeOffer(grown.Sessions, m.Sessions)) != 1 {
			t.Fatalf("member change was invisible: %s %v", s.RelPath, err)
		}
		if err := os.WriteFile(s.Path, orig, 0600); err != nil {
			t.Fatal(err)
		}
	}
	added := makeSession("-project/parent/subagents/agent-c.jsonl", "third")
	grown, err := buildClaudeManifest(append(all, added), "")
	if err != nil || len(computeOffer(grown.Sessions, m.Sessions)) != 1 {
		t.Fatalf("new subagent invisible: %v", err)
	}
	// Project folder encoding must not affect the member identity.
	moved := append([]sessions.Session(nil), all...)
	for i := range moved {
		moved[i].RelPath = "-moved" + moved[i].RelPath[len("-project"):]
	}
	mapped, err := buildClaudeManifest(moved, "")
	if err != nil || !reflect.DeepEqual(m, mapped) {
		t.Fatalf("project encoding changed group fingerprint: %v", err)
	}
}
