package bundle

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeTestZip writes a minimal bundle-like zip with the given entries.
func writeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "b.codexbundle")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanBundleSecretsFindsKey(t *testing.T) {
	p := writeTestZip(t, map[string]string{
		"manifest.json":                           `{"tool":"codex","aws":"AKIAIOSFODNN7EXAMPLE"}`, // ignored: not a session entry
		"sessions/2026/06/01/rollout-x.jsonl":     `{"text":"my key is AKIAIOSFODNN7EXAMPLE here"}`,
		"sessions/2026/06/01/rollout-clean.jsonl": `{"text":"nothing to see"}`,
	})
	res, err := ScanBundleSecrets(p)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !res.Any() {
		t.Fatal("expected secrets to be found")
	}
	if res.SessionsWithSecrets != 1 {
		t.Fatalf("SessionsWithSecrets = %d, want 1 (manifest must be ignored)", res.SessionsWithSecrets)
	}
	if res.TotalFindings < 1 {
		t.Fatalf("TotalFindings = %d", res.TotalFindings)
	}
}

func TestScanBundleSecretsClean(t *testing.T) {
	p := writeTestZip(t, map[string]string{
		"sessions/2026/06/01/rollout-x.jsonl": `{"text":"all good"}`,
	})
	res, err := ScanBundleSecrets(p)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Any() {
		t.Fatalf("expected no secrets, got %+v", res)
	}
}

func TestIsSessionEntry(t *testing.T) {
	yes := []string{
		"sessions/2026/06/01/rollout-x.jsonl",
		"archived_sessions/2026/06/01/rollout-x.jsonl.zst",
		"projects/-home-u-proj/abc.jsonl",
	}
	no := []string{"manifest.json", "checksums.json", "sessions/", "projects/"}
	for _, n := range yes {
		if !isSessionEntry(n) {
			t.Errorf("isSessionEntry(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if isSessionEntry(n) {
			t.Errorf("isSessionEntry(%q) = true, want false", n)
		}
	}
}
