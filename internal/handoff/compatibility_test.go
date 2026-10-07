package handoff

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCodexResponseUserRecoveredWithAssistantEvent(t *testing.T) {
	s, err := FromCodexBytes([]byte(
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}` + "\n" +
			`{"type":"event_msg","payload":{"type":"agent_message","message":"done"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Turn{{Role: RoleUser, Text: "hello"}, {Role: RoleAssistant, Text: "done"}}
	if !reflect.DeepEqual(s.Conversation, want) {
		t.Fatalf("conversation = %+v, want %+v", s.Conversation, want)
	}
}

func TestCodexInheritedMetadataKeepsChildIdentity(t *testing.T) {
	s, err := FromCodexBytes([]byte(
		`{"timestamp":"child-time","type":"session_meta","payload":{"id":"child","cwd":"/child","timestamp":"child-time","git":{"branch":"child-branch"}}}` + "\n" +
			`{"timestamp":"parent-time","type":"session_meta","payload":{"id":"parent","cwd":"/parent","timestamp":"parent-time","git":{"branch":"parent-branch"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.ThreadID != "child" || s.CWD != "/child" || s.CreatedAt != "child-time" || s.Git == nil || s.Git.Branch != "child-branch" {
		t.Fatalf("child identity overwritten: %+v", s)
	}
}

func TestCodexHandoffOmitsProvider(t *testing.T) {
	data, _, err := ToCodex(sampleIR("claude"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(string(data), "\n", 2)[0]), &meta); err != nil {
		t.Fatal(err)
	}
	if _, present := meta.Payload["model_provider"]; present {
		t.Fatal("handoff must use the destination's default provider")
	}
}

func TestCodexPaginatedAndResponseMessages(t *testing.T) {
	const user = `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`
	const assistant = `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`
	const developer = `{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"private instruction"}]}}`
	for _, events := range []string{
		"",
		`{"type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n" + `{"type":"event_msg","payload":{"type":"agent_message","message":"answer"}}`,
		`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"hello"}]}}}` + "\n" + `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"Text","text":"answer"}]}}}`,
	} {
		s, err := FromCodexBytes([]byte(strings.Join([]string{user, assistant, developer, events}, "\n")))
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Conversation) != 2 || s.Conversation[0].Text != "hello" || s.Conversation[1].Text != "answer" {
			t.Fatalf("missing or duplicated conversation: %+v", s.Conversation)
		}
	}
	_, err := FromCodexBytes([]byte(`{"type":"session_meta","payload":{"history_base":{"thread_id":"parent"}}}`))
	if err == nil {
		t.Fatal("fork translation must not silently discard inherited history")
	}
}

func TestClaudeActiveChain(t *testing.T) {
	data := strings.Join([]string{
		`{"type":"user","uuid":"root","parentUuid":null,"message":{"content":"question"}}`,
		`{"type":"assistant","uuid":"abandoned","parentUuid":"root","message":{"content":"abandoned answer"}}`,
		`{"type":"assistant","uuid":"active","parentUuid":"root","message":{"content":"outdated answer"}}`,
		`{"type":"assistant","uuid":"active","parentUuid":"root","message":{"content":"current answer"}}`,
		`{"type":"assistant","uuid":"side","parentUuid":"active","isSidechain":true,"message":{"content":"sidechain answer"}}`,
	}, "\n")
	s, err := FromClaudeBytes([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Conversation) != 2 || s.Conversation[0].Text != "question" || s.Conversation[1].Text != "current answer" {
		t.Fatalf("wrong active chain: %+v", s.Conversation)
	}
}
