package webui

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
)

// ageRecipient generates a throwaway age key pair and returns its public key.
// Tests that need it are skipped when age is not installed, like the crypt
// round-trip test.
func ageRecipient(t *testing.T) string {
	t.Helper()
	if !crypt.Available() {
		t.Skip("age not installed")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed")
	}
	keyFile := filepath.Join(t.TempDir(), "key.txt")
	if out, err := exec.Command("age-keygen", "-o", keyFile).CombinedOutput(); err != nil {
		t.Fatalf("age-keygen: %v: %s", err, out)
	}
	f, err := os.Open(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if pub, ok := strings.CutPrefix(sc.Text(), "# public key: "); ok {
			return strings.TrimSpace(pub)
		}
	}
	t.Fatal("no public key in age-keygen output")
	return ""
}

func exportEncryptedBody(t *testing.T, output string, recipient string, overwrite bool) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"mode":       "all",
		"output":     output,
		"encrypt_to": []string{recipient},
		"overwrite":  overwrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertNoExportTemps fails when an intermediate bundle was left in dir.
func assertNoExportTemps(t *testing.T, dir string) {
	t.Helper()
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".cct-export-*"))
	if len(leftovers) > 0 {
		t.Errorf("temporary export files left behind: %v", leftovers)
	}
}

// Encryption writes <output>.age. Replacing an existing one must be confirmed,
// even though the path that was picked or typed is the .codexbundle name.
func TestExportEncryptedAsksBeforeReplacingAgeFile(t *testing.T) {
	recipient := ageRecipient(t)
	s, ts := testServer(t)
	writeSession(t, s.home, sessID, "/work/p")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	target := output + crypt.Extension
	if err := os.WriteFile(target, []byte("previous encrypted bundle"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, data := do(t, ts, "POST", "/api/export", testToken, exportEncryptedBody(t, output, recipient, false))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409: %s", res.StatusCode, data)
	}
	var conflict struct {
		TargetExists bool   `json:"target_exists"`
		Target       string `json:"target"`
	}
	json.Unmarshal(data, &conflict)
	if !conflict.TargetExists || conflict.Target != target {
		t.Errorf("conflict response = %s, want target_exists for %s", data, target)
	}
	if got := readFileString(t, target); got != "previous encrypted bundle" {
		t.Errorf("unconfirmed export changed the existing .age file: %q", got)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf("unconfirmed export wrote %s (err=%v)", output, err)
	}
	assertNoExportTemps(t, dir)

	res, data = do(t, ts, "POST", "/api/export", testToken, exportEncryptedBody(t, output, recipient, true))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("confirmed export: status=%d: %s", res.StatusCode, data)
	}
	if got := readFileString(t, target); !strings.HasPrefix(got, "age-encryption.org/") {
		t.Errorf("confirmed export did not replace the .age file with an age file: %.40q", got)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf("clear bundle left at %s (err=%v)", output, err)
	}
	assertNoExportTemps(t, dir)
}

// A failed encryption must not cost the user an existing file: neither the
// .age target nor a clear bundle that already sat at the typed path.
func TestExportEncryptFailureKeepsExistingFiles(t *testing.T) {
	ageRecipient(t)
	s, ts := testServer(t)
	writeSession(t, s.home, sessID, "/work/p")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	target := output + crypt.Extension
	if err := os.WriteFile(target, []byte("previous encrypted bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("previous clear bundle"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, data := do(t, ts, "POST", "/api/export", testToken, exportEncryptedBody(t, output, "not-an-age-recipient", true))
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422: %s", res.StatusCode, data)
	}
	if got := readFileString(t, target); got != "previous encrypted bundle" {
		t.Errorf("failed encryption changed the existing .age file: %q", got)
	}
	if got := readFileString(t, output); got != "previous clear bundle" {
		t.Errorf("failed encryption changed the file at the typed path: %q", got)
	}
	assertNoExportTemps(t, dir)
}

// The clear bundle is only an intermediate, so a successful encrypted export
// leaves a file that already sat at the typed path alone.
func TestExportEncryptedLeavesFileAtTypedPath(t *testing.T) {
	recipient := ageRecipient(t)
	s, ts := testServer(t)
	writeSession(t, s.home, sessID, "/work/p")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	if err := os.WriteFile(output, []byte("previous clear bundle"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, data := do(t, ts, "POST", "/api/export", testToken, exportEncryptedBody(t, output, recipient, false))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d: %s", res.StatusCode, data)
	}
	var r struct {
		Bundle string `json:"bundle"`
	}
	json.Unmarshal(data, &r)
	if r.Bundle != output+crypt.Extension {
		t.Errorf("bundle = %q, want %q", r.Bundle, output+crypt.Extension)
	}
	if got := readFileString(t, output); got != "previous clear bundle" {
		t.Errorf("encrypted export replaced the file at the typed path: %q", got)
	}
	assertNoExportTemps(t, dir)
}
