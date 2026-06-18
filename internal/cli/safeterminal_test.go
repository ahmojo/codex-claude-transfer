package cli

import (
	"strings"
	"testing"
)

func TestSafeTerminal(t *testing.T) {
	if got := safeTerminal("normal text /path/to/x"); got != "normal text /path/to/x" {
		t.Errorf("plain text changed: %q", got)
	}
	// An OSC 52 clipboard-injection payload: ESC ] 52 ; … BEL.
	osc := "before\x1b]52;c;ZXZpbA==\x07after"
	got := safeTerminal(osc)
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Errorf("control bytes survived: %q", got)
	}
	// Newlines and CRs are dropped so a value cannot forge extra terminal lines.
	if strings.ContainsAny(safeTerminal("x\ny\rz"), "\n\r") {
		t.Error("newline/CR not stripped")
	}
	// Tabs become spaces.
	if got := safeTerminal("a\tb"); got != "a b" {
		t.Errorf("tab not normalized: %q", got)
	}
}
