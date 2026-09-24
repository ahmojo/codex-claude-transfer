package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
)

// requireAge skips a test that drives an encrypted export through age: export
// refuses to start without it.
func requireAge(t *testing.T) {
	t.Helper()
	if !crypt.Available() {
		t.Skip("age not installed")
	}
}

// ageRecipient generates a throwaway age key pair and returns its public key.
func ageRecipient(t *testing.T) string {
	t.Helper()
	requireAge(t)
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

// writeExistingFiles puts a previous clear bundle at output and a previous
// encrypted one at output.age, the two files an encrypted export must not lose.
func writeExistingFiles(t *testing.T, output string) {
	t.Helper()
	if err := os.WriteFile(output, []byte("previous clear bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output+crypt.Extension, []byte("previous encrypted bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	for _, pattern := range []string{".cct-export-*", ".codexbundle-*.tmp"} {
		leftovers, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(leftovers) > 0 {
			t.Errorf("temporary export files left behind: %v", leftovers)
		}
	}
}

// A failed encryption must not cost the user an existing file: neither the
// .age target, which age never touched, nor a file already at the -o path.
func TestRunExportEncryptFailureKeepsExistingFiles(t *testing.T) {
	requireAge(t)
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/work/p")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	writeExistingFiles(t, output)

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--codex-home", home,
		"--encrypt-to", "not-an-age-recipient", "-o", output}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "encrypt failed") {
		t.Errorf("missing encrypt failure message: %s", errOut.String())
	}
	if got := readFileString(t, output+crypt.Extension); got != "previous encrypted bundle" {
		t.Errorf("failed encryption changed the existing .age file: %q", got)
	}
	if got := readFileString(t, output); got != "previous clear bundle" {
		t.Errorf("failed encryption changed the file at -o: %q", got)
	}
	assertNoExportTemps(t, dir)
}

// Re-exporting to the same path replaces the previous .age, as a plain export
// replaces the previous bundle. The clear bundle is only an intermediate, so a
// file already at -o is left alone.
func TestRunExportEncryptedReplacesAgeFileOnly(t *testing.T) {
	recipient := ageRecipient(t)
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/work/p")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	writeExistingFiles(t, output)

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--codex-home", home,
		"--encrypt-to", recipient, "-o", output, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	var res struct {
		Bundle string `json:"bundle"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("export --json is not JSON: %v\n%s", err, out.String())
	}
	if res.Bundle != output+crypt.Extension {
		t.Errorf("bundle = %q, want %q", res.Bundle, output+crypt.Extension)
	}
	if got := readFileString(t, output+crypt.Extension); !strings.HasPrefix(got, "age-encryption.org/") {
		t.Errorf("the .age file was not replaced with an age file: %.40q", got)
	}
	if got := readFileString(t, output); got != "previous clear bundle" {
		t.Errorf("encrypted export changed the file at -o: %q", got)
	}
	assertNoExportTemps(t, dir)
}

// When the secret gate refuses an encrypted export, it removes only the clear
// intermediate it wrote.
func TestRunExportEncryptedSecretRefusalKeepsExistingFiles(t *testing.T) {
	requireAge(t)
	home := t.TempDir()
	writeSessionMsg(t, home, "aaaa1111-2222-3333-4444-555566667777", "/work/p", "deploy with AKIAIOSFODNN7EXAMPLE")
	dir := t.TempDir()
	output := filepath.Join(dir, "project.codexbundle")
	writeExistingFiles(t, output)

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--codex-home", home,
		"--encrypt-to", "age1notusedbecausethegatestopsfirst", "-o", output}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit=%d, want 1; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "likely secret") {
		t.Errorf("missing secret-gate refusal: %s", errOut.String())
	}
	if got := readFileString(t, output+crypt.Extension); got != "previous encrypted bundle" {
		t.Errorf("refused export changed the existing .age file: %q", got)
	}
	if got := readFileString(t, output); got != "previous clear bundle" {
		t.Errorf("refused export changed the file at -o: %q", got)
	}
	assertNoExportTemps(t, dir)
}
