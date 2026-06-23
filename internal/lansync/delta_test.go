package lansync

import (
	"bytes"
	"testing"
)

func TestPlanDeltaAppendRoundTrip(t *testing.T) {
	// Receiver holds the first line; sender's file grew by a second line.
	prefix := []byte(`{"t":"meta"}` + "\n")
	full := append(append([]byte{}, prefix...), []byte(`{"t":"msg"}`+"\n")...)

	plan := PlanDelta(full, int64(len(prefix)), sha256Hex(prefix))
	if !plan.Append {
		t.Fatal("expected an append-only plan for a clean prefix")
	}
	if !bytes.Equal(plan.Suffix, full[len(prefix):]) {
		t.Fatalf("suffix = %q", plan.Suffix)
	}

	got, err := ApplyDelta(prefix, plan)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Fatalf("reconstructed != full:\n got=%q\nwant=%q", got, full)
	}
}

func TestPlanDeltaFullWhenNewOrDiverged(t *testing.T) {
	full := []byte("hello world\n")

	// Receiver doesn't have it.
	if p := PlanDelta(full, -1, ""); p.Append {
		t.Fatal("new session should be a full send")
	}

	// Receiver's prefix hash doesn't match (diverged) -> full send.
	diverged := PlanDelta(full, 5, sha256Hex([]byte("XXXXX")))
	if diverged.Append {
		t.Fatal("diverged prefix should be a full send")
	}
	got, err := ApplyDelta(nil, diverged)
	if err != nil || !bytes.Equal(got, full) {
		t.Fatalf("full apply: got=%q err=%v", got, err)
	}
}

func TestApplyDeltaRejectsCorruption(t *testing.T) {
	full := []byte("abcdef\n")
	prefix := []byte("abc")
	plan := PlanDelta(full, int64(len(prefix)), sha256Hex(prefix))
	// Tamper with the suffix so the reconstructed hash won't match.
	plan.Suffix = []byte("XYZ\n")
	if _, err := ApplyDelta(prefix, plan); err == nil {
		t.Fatal("expected verification failure on a corrupted suffix")
	}
}
