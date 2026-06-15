package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	cases := map[string]string{
		"bash": "complete -o default -F _codex_sync codex-sync",
		"zsh":  "compdef _codex_sync codex-sync",
		"fish": "complete -c codex-sync",
	}
	for shell, marker := range cases {
		var out, errOut bytes.Buffer
		if code := Run([]string{"completion", shell}, &out, &errOut); code != 0 {
			t.Fatalf("completion %s exit = %d, stderr=%s", shell, code, errOut.String())
		}
		s := out.String()
		if !strings.Contains(s, marker) {
			t.Errorf("%s completion missing %q:\n%s", shell, marker, s)
		}
		// Every command and the --json flag must appear in each script.
		if !strings.Contains(s, "export") || !strings.Contains(s, "import") || !strings.Contains(s, "json") {
			t.Errorf("%s completion missing expected commands/flags", shell)
		}
	}
}

func TestCompletionUnknownShellErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run([]string{"completion", "powershell"}, &out, &errOut); code != 2 {
		t.Fatalf("expected exit 2 for unsupported shell, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unsupported shell") {
		t.Errorf("missing error message: %s", errOut.String())
	}
}
