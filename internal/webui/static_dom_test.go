package webui

import (
	"os/exec"
	"testing"
)

func TestReconcileDOMState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}

	cmd := exec.Command(node, "--test", "testdata/reconcile_dom_test.mjs")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("DOM state test failed: %v\n%s", err, output)
	}
}
