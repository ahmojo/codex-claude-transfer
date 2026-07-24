package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	cases := map[string]string{
		"bash": "complete -o default -F _cct cct",
		"zsh":  "compdef _cct cct",
		"fish": "complete -c cct",
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
		for _, want := range []string{"export", "import", "relocate", "diff", "undo", "move-project", "json"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s completion missing %q", shell, want)
			}
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
