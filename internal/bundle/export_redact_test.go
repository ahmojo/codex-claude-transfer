package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
)

const awsKey = "AKIAIOSFODNN7EXAMPLE"

// --redact must remove secrets from the manifest too, not only from the
// transcripts: the manifest keeps a copy of the first user message.
func TestRedactCoversManifestText(t *testing.T) {
	cases := map[string]string{
		"whole key":             "deploy with " + awsKey,
		"key across the cut":    strings.Repeat("word ", 18) + awsKey + " and more text after it",
		"key after the preview": strings.Repeat("filler ", 20) + awsKey,
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			home := fakeHome(t)
			dir := filepath.Join(home.SessionsDir, "2026", "06", "13")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			msgJSON, _ := json.Marshal(message)
			body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + claudeID + `","cwd":"/work/p","source":"cli"}}` + "\n" +
				`{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":` + string(msgJSON) + `}}` + "\n"
			if err := os.WriteFile(filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+claudeID+".jsonl"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "red.codexbundle")
			res, err := Export(home, ExportOptions{Tool: agent.Codex, OutputPath: out, Redact: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.SecretsRedacted == 0 {
				t.Fatal("the transcript secret was not redacted")
			}
			files := readBundle(t, out)
			for entry, data := range files {
				// Any 10-character piece of the key is a leak, including the
				// half that survives a cut in the preview.
				if strings.Contains(string(data), awsKey[:10]) {
					t.Errorf("%s still contains part of the key", entry)
				}
			}
			var m Manifest
			if err := json.Unmarshal(files[ManifestName], &m); err != nil {
				t.Fatal(err)
			}
			s := m.Sessions[0]
			if !strings.Contains(s.FirstUserMessage, "[REDACTED:") {
				t.Errorf("first_user_message = %q", s.FirstUserMessage)
			}
			if !utf8.ValidString(s.Preview) || len(s.Preview) > 100+len("…") {
				t.Errorf("preview = %q", s.Preview)
			}
		})
	}
}

// Without --redact the manifest keeps the message as it is; the secret gate
// is what stops such a bundle.
func TestManifestTextUnchangedWithoutRedact(t *testing.T) {
	home := fakeHome(t)
	writeSession(t, home.SessionsDir, "2026/06/13/rollout-2026-06-13T18-22-01-"+claudeID+".jsonl", claudeID, "/work/p")
	out := filepath.Join(t.TempDir(), "plain.codexbundle")
	if _, err := Export(home, ExportOptions{Tool: agent.Codex, OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(readBundle(t, out)[ManifestName], &m); err != nil {
		t.Fatal(err)
	}
	if s := m.Sessions[0]; s.FirstUserMessage != "hello" || s.Preview != "hello" {
		t.Errorf("manifest text changed without --redact: %+v", s)
	}
}

func TestPreviewTextCutsAtCharacterBoundary(t *testing.T) {
	msg := strings.Repeat("a", 99) + "ü rest"
	got := previewText(msg)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Errorf("previewText = %q", got)
	}
	if previewText("  short   text ") != "short text" {
		t.Errorf("whitespace not collapsed: %q", previewText("  short   text "))
	}
}
