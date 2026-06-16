package handoff

import (
	"strings"
	"testing"
)

func sampleIR(source string) AgentSession {
	return AgentSession{
		Format:      IRFormat,
		SourceAgent: source,
		ThreadID:    "11112222-3333-4444-5555-666677778888",
		CWD:         `C:\Users\example\dev\project`,
		Git:         &GitInfo{Branch: "main", Commit: "abc123", Remote: "https://example.com/r.git"},
		Conversation: []Turn{
			{Role: RoleUser, Text: "add a retry to the http client"},
			{Role: RoleAssistant, Text: "Sure — I'll wrap the call in a backoff loop."},
			{Role: RoleTool, Tool: "shell", Text: "ran shell: go test ./..."},
			{Role: RoleAssistant, Text: "Tests pass; done."},
		},
	}
}

func TestDeterministicIDStable(t *testing.T) {
	a := DeterministicID("codex:abc", "claude")
	b := DeterministicID("codex:abc", "claude")
	if a != b {
		t.Fatalf("not deterministic: %s vs %s", a, b)
	}
	if DeterministicID("codex:abc", "codex") == a {
		t.Fatal("target agent should change the id")
	}
	if len(a) != 36 {
		t.Fatalf("not uuid-shaped: %q", a)
	}
}

func TestToClaudeRoundTrips(t *testing.T) {
	ir := sampleIR("codex")
	data, id, err := ToClaude(ir)
	if err != nil {
		t.Fatalf("ToClaude: %v", err)
	}
	if id != DeterministicID("codex:"+ir.ThreadID, "claude") {
		t.Fatalf("unexpected id %s", id)
	}
	// Re-parse the synthesized transcript with our own Claude extractor.
	got, err := fromClaudeReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got.CWD != ir.CWD {
		t.Errorf("cwd = %q, want %q", got.CWD, ir.CWD)
	}
	// First turn is the preamble (a user turn) naming the source agent.
	if len(got.Conversation) == 0 || got.Conversation[0].Role != RoleUser ||
		!strings.Contains(got.Conversation[0].Text, "Codex") {
		t.Fatalf("missing handoff preamble as first user turn: %+v", first(got.Conversation))
	}
	// The original user prose survives somewhere in the transcript.
	if !conversationContains(got, "add a retry to the http client") {
		t.Error("user message not preserved")
	}
	if !conversationContains(got, "backoff loop") {
		t.Error("assistant message not preserved")
	}
	// The tool summary survives as a note.
	if !conversationContains(got, "go test") {
		t.Error("tool summary not preserved")
	}
}

func TestToCodexRoundTrips(t *testing.T) {
	ir := sampleIR("claude")
	data, id, err := ToCodex(ir)
	if err != nil {
		t.Fatalf("ToCodex: %v", err)
	}
	if id != DeterministicID("claude:"+ir.ThreadID, "codex") {
		t.Fatalf("unexpected id %s", id)
	}
	got, err := fromCodexReader(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got.CWD != ir.CWD {
		t.Errorf("cwd = %q, want %q", got.CWD, ir.CWD)
	}
	if got.ThreadID != id {
		t.Errorf("thread id = %q, want synthesized %q", got.ThreadID, id)
	}
	if len(got.Conversation) == 0 || !strings.Contains(got.Conversation[0].Text, "Claude Code") {
		t.Fatalf("missing handoff preamble naming source")
	}
	if !conversationContains(got, "add a retry to the http client") {
		t.Error("user message not preserved")
	}
	if !conversationContains(got, "backoff loop") {
		t.Error("assistant message not preserved")
	}
}

func TestFromCodexExtractsConversation(t *testing.T) {
	rollout := strings.Join([]string{
		`{"timestamp":"t0","type":"session_meta","payload":{"id":"sid","cwd":"/p","git":{"branch":"main"}}}`,
		`{"timestamp":"t1","type":"event_msg","payload":{"type":"user_message","message":"hello there"}}`,
		`{"timestamp":"t2","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"go\",\"build\"]}"}}`,
		`{"timestamp":"t3","type":"event_msg","payload":{"type":"agent_message","message":"done building"}}`,
	}, "\n")
	ir, err := fromCodexReader(strings.NewReader(rollout))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ir.ThreadID != "sid" || ir.CWD != "/p" || ir.Git == nil || ir.Git.Branch != "main" {
		t.Fatalf("meta not extracted: %+v", ir)
	}
	wantRoles := []Role{RoleUser, RoleTool, RoleAssistant}
	if len(ir.Conversation) != 3 {
		t.Fatalf("turns = %d, want 3: %+v", len(ir.Conversation), ir.Conversation)
	}
	for i, w := range wantRoles {
		if ir.Conversation[i].Role != w {
			t.Errorf("turn %d role = %q, want %q", i, ir.Conversation[i].Role, w)
		}
	}
	if !strings.Contains(ir.Conversation[1].Text, "go build") {
		t.Errorf("tool args not summarized: %q", ir.Conversation[1].Text)
	}
}

func TestFromClaudeExtractsConversation(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"queue-operation","sessionId":"sid"}`,
		`{"type":"user","sessionId":"sid","cwd":"/p","gitBranch":"feature","message":{"role":"user","content":"fix the bug"}}`,
		`{"type":"assistant","sessionId":"sid","cwd":"/p","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"On it."},{"type":"tool_use","name":"Bash","input":{"command":"npm test"}}]}}`,
	}, "\n")
	ir, err := fromClaudeReader(strings.NewReader(transcript))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ir.ThreadID != "sid" || ir.CWD != "/p" || ir.Git == nil || ir.Git.Branch != "feature" {
		t.Fatalf("meta not extracted: %+v", ir)
	}
	if !conversationContains(ir, "fix the bug") || !conversationContains(ir, "On it.") || !conversationContains(ir, "npm test") {
		t.Fatalf("conversation not extracted: %+v", ir.Conversation)
	}
	// "thinking" blocks are dropped.
	if conversationContains(ir, "hmm") {
		t.Error("thinking block leaked into translation")
	}
}

func TestTranslateIsDeterministic(t *testing.T) {
	rollout := strings.Join([]string{
		`{"timestamp":"2026-06-16T10:00:00Z","type":"session_meta","payload":{"id":"sid","cwd":"C:\\p\\proj","timestamp":"2026-06-16T10:00:00Z"}}`,
		`{"timestamp":"2026-06-16T10:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`,
		`{"timestamp":"2026-06-16T10:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"hello"}}`,
	}, "\n")
	a, err := Translate([]byte(rollout), "codex", "claude")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	b, _ := Translate([]byte(rollout), "codex", "claude")
	if string(a.Content) != string(b.Content) {
		t.Fatal("translation is not byte-deterministic across runs")
	}
	wantRel := "projects/C--p-proj/" + a.SessionID + ".jsonl"
	if a.RelPath != wantRel {
		t.Errorf("relpath = %q, want %q", a.RelPath, wantRel)
	}
}

func TestTranslateToCodexPathShape(t *testing.T) {
	transcript := `{"type":"user","sessionId":"sid","cwd":"/home/x/p","timestamp":"2026-06-16T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","sessionId":"sid","cwd":"/home/x/p","message":{"role":"assistant","content":[{"type":"text","text":"yo"}]}}`
	tr, err := Translate([]byte(transcript), "claude", "codex")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !strings.HasPrefix(tr.RelPath, "sessions/2026/06/16/rollout-") || !strings.HasSuffix(tr.RelPath, tr.SessionID+".jsonl") {
		t.Errorf("unexpected codex relpath %q", tr.RelPath)
	}
}

func TestTranslateSameAgentRejected(t *testing.T) {
	if _, err := Translate([]byte(`{}`), "codex", "codex"); err == nil {
		t.Fatal("expected error translating to the same agent")
	}
}

func conversationContains(s AgentSession, sub string) bool {
	for _, t := range s.Conversation {
		if strings.Contains(t.Text, sub) {
			return true
		}
	}
	return false
}

func first(ts []Turn) any {
	if len(ts) == 0 {
		return nil
	}
	return ts[0]
}
