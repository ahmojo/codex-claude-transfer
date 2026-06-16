// Package crypt provides optional bundle encryption by shelling out to the
// `age` CLI (https://github.com/FiloSottile/age). It deliberately does not embed
// a crypto library: like the git integration, cct stays a single,
// dependency-free binary and reuses a well-known external tool when the user
// opts into encryption. If `age` is not installed, encryption simply errors with
// guidance and nothing else in cct is affected.
package crypt

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Extension is the suffix used for encrypted bundles.
const Extension = ".age"

// Available reports whether the `age` binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("age")
	return err == nil
}

// EncryptOptions selects how a bundle is encrypted. Passphrase is mutually
// exclusive with Recipients/RecipientsFile (age cannot mix the two).
type EncryptOptions struct {
	Recipients     []string // age recipient strings (age1..., ssh-ed25519 ...)
	RecipientsFile string   // path to a file of recipients (age -R)
	Passphrase     bool     // encrypt with an interactive passphrase (age -p)
}

// DecryptOptions selects how an encrypted bundle is decrypted.
type DecryptOptions struct {
	IdentityFile string // age identity (private key) file (age -i)
	Passphrase   bool   // decrypt with an interactive passphrase
}

// buildEncryptArgs assembles the age argument list for encryption. It is pure
// so it can be unit-tested without invoking age.
func buildEncryptArgs(in, out string, opts EncryptOptions) []string {
	var args []string
	if opts.Passphrase {
		args = append(args, "-p")
	}
	for _, r := range opts.Recipients {
		args = append(args, "-r", r)
	}
	if opts.RecipientsFile != "" {
		args = append(args, "-R", opts.RecipientsFile)
	}
	args = append(args, "-o", out, in)
	return args
}

// buildDecryptArgs assembles the age argument list for decryption.
func buildDecryptArgs(in, out string, opts DecryptOptions) []string {
	args := []string{"-d"}
	if opts.IdentityFile != "" {
		args = append(args, "-i", opts.IdentityFile)
	}
	args = append(args, "-o", out, in)
	return args
}

// Encrypt encrypts the plaintext file in to the file out using age.
func Encrypt(in, out string, opts EncryptOptions) error {
	if !opts.Passphrase && len(opts.Recipients) == 0 && opts.RecipientsFile == "" {
		return fmt.Errorf("no recipients: pass --encrypt-to, --recipients-file, or --passphrase")
	}
	cmd, err := ageCommand(buildEncryptArgs(in, out, opts), opts.Passphrase)
	if err != nil {
		return err
	}
	return runAge(cmd, opts.Passphrase)
}

// Decrypt decrypts the encrypted file in to the file out using age. age may
// prompt on the terminal for a passphrase or for an encrypted identity, so the
// process's standard streams are connected when decrypting.
func Decrypt(in, out string, opts DecryptOptions) error {
	cmd, err := ageCommand(buildDecryptArgs(in, out, opts), true)
	if err != nil {
		return err
	}
	return runAge(cmd, true)
}

// ageCommand builds an *exec.Cmd for age, wiring the process terminal when an
// interactive prompt may be needed.
func ageCommand(args []string, interactive bool) (*exec.Cmd, error) {
	if !Available() {
		return nil, fmt.Errorf("age is not installed or not on PATH; install age (https://github.com/FiloSottile/age) to use bundle encryption")
	}
	cmd := exec.Command("age", args...)
	if interactive {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd, nil
}

// runAge runs the command. For non-interactive runs it captures stderr so the
// caller gets a useful error; interactive runs let age talk to the terminal.
func runAge(cmd *exec.Cmd, interactive bool) error {
	if interactive {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("age failed: %w", err)
		}
		return nil
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("age failed: %s", msg)
		}
		return fmt.Errorf("age failed: %w", err)
	}
	return nil
}
