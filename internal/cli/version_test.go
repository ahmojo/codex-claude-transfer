package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-V"} {
		var out, errOut bytes.Buffer
		if code := Run([]string{arg}, &out, &errOut); code != 0 {
			t.Fatalf("%s exit = %d, stderr=%s", arg, code, errOut.String())
		}
		s := out.String()
		if !strings.HasPrefix(s, "cct ") {
			t.Errorf("%s output missing banner: %q", arg, s)
		}
		// versionString falls back to "dev" in tests (no linker injection).
		if !strings.Contains(s, "dev") {
			t.Errorf("%s output missing version: %q", arg, s)
		}
	}
}
