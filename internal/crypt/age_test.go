package crypt

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildEncryptArgsRecipients(t *testing.T) {
	got := buildEncryptArgs("in.bundle", "out.age", EncryptOptions{
		Recipients: []string{"age1aaa", "ssh-ed25519 BBB"},
	})
	want := []string{"-r", "age1aaa", "-r", "ssh-ed25519 BBB", "-o", "out.age", "in.bundle"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestBuildEncryptArgsRecipientsFile(t *testing.T) {
	got := buildEncryptArgs("in.bundle", "out.age", EncryptOptions{
		RecipientsFile: "rcpts.txt",
	})
	want := []string{"-R", "rcpts.txt", "-o", "out.age", "in.bundle"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestBuildEncryptArgsPassphrase(t *testing.T) {
	got := buildEncryptArgs("in.bundle", "out.age", EncryptOptions{Passphrase: true})
	want := []string{"-p", "-o", "out.age", "in.bundle"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestBuildDecryptArgsIdentity(t *testing.T) {
	got := buildDecryptArgs("in.age", "out.bundle", DecryptOptions{IdentityFile: "key.txt"})
	want := []string{"-d", "-i", "key.txt", "-o", "out.bundle", "in.age"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestBuildDecryptArgsPassphrase(t *testing.T) {
	got := buildDecryptArgs("in.age", "out.bundle", DecryptOptions{Passphrase: true})
	want := []string{"-d", "-o", "out.bundle", "in.age"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestEncryptRequiresRecipient(t *testing.T) {
	err := Encrypt("in", "out", EncryptOptions{})
	if err == nil {
		t.Fatal("expected error when no recipient/passphrase given")
	}
}

// TestEncryptWithoutAge verifies the clear install-guidance error when age is
// not on PATH. This path runs in CI where age is typically absent.
func TestEncryptWithoutAge(t *testing.T) {
	if Available() {
		t.Skip("age is installed; cannot test the missing-age path")
	}
	err := Encrypt("in", "out", EncryptOptions{Recipients: []string{"age1aaa"}})
	if err == nil {
		t.Fatal("expected error when age is not installed")
	}
}

func TestDecryptWithoutAge(t *testing.T) {
	if Available() {
		t.Skip("age is installed; cannot test the missing-age path")
	}
	err := Decrypt("in.age", "out", DecryptOptions{Passphrase: true})
	if err == nil {
		t.Fatal("expected error when age is not installed")
	}
}

// TestRoundTripWithRecipientsFile encrypts and decrypts using an age key pair.
// It is skipped when age is unavailable so the suite stays green without it.
func TestRoundTripWithRecipientsFile(t *testing.T) {
	if !Available() {
		t.Skip("age not installed; skipping round-trip")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed; skipping round-trip")
	}

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.txt")
	out, err := exec.Command("age-keygen", "-o", keyFile).CombinedOutput()
	if err != nil {
		t.Fatalf("age-keygen failed: %v: %s", err, out)
	}
	// Extract the public recipient from the key file's comment line.
	recipient := recipientFromKeyFile(t, keyFile)
	rcptFile := filepath.Join(dir, "rcpts.txt")
	if err := os.WriteFile(rcptFile, []byte(recipient+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plain := filepath.Join(dir, "in.bundle")
	want := []byte("hello cct bundle bytes")
	if err := os.WriteFile(plain, want, 0o600); err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, "out.age")
	if err := Encrypt(plain, enc, EncryptOptions{RecipientsFile: rcptFile}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	dec := filepath.Join(dir, "back.bundle")
	if err := Decrypt(enc, dec, DecryptOptions{IdentityFile: keyFile}); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	got, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, want)
	}
}

func recipientFromKeyFile(t *testing.T, keyFile string) string {
	t.Helper()
	data, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# public key: "
	for _, line := range splitLines(string(data)) {
		if len(line) > len(marker) && line[:len(marker)] == marker {
			return line[len(marker):]
		}
	}
	t.Fatalf("no public key comment in %s", keyFile)
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
