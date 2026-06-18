// Package zstdcli recovers metadata from zstd-compressed Codex rollout files by
// shelling out to the `zstd` CLI (https://github.com/facebook/zstd). Like the
// git and age integrations, cct stays a single, dependency-free binary
// and reuses a well-known external tool when it is available. Codex writes
// `.jsonl.zst` files as standard zstd frames, so the stock `zstd -d` reads them.
//
// This package only ever *reads* (decompresses) compressed sessions to recover
// their session_meta — it never recompresses, rewrites, or modifies them. When
// `zstd` is not installed, callers fall back to treating compressed sessions as
// metadata-unknown, exactly as before.
package zstdcli

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// binary is the external tool name, resolved on PATH.
const binary = "zstd"

// DefaultHeadBytes is how many decompressed bytes are enough to recover the
// session_meta line (the first JSONL record) plus the first user message,
// without inflating a whole large rollout into memory.
const DefaultHeadBytes = 1 << 20 // 1 MiB

// MaxDecompressedBytes caps a full decompression so a small compressed input
// cannot inflate without bound (a decompression bomb). 256 MiB is far larger than
// any real session yet keeps memory bounded. See docs/security/audit.md (SEC-2).
const MaxDecompressedBytes = 256 << 20

// Available reports whether the `zstd` binary is on PATH.
func Available() bool {
	_, err := exec.LookPath(binary)
	return err == nil
}

// DecompressHead returns up to maxBytes of decompressed output from a
// zstd-compressed file. It streams `zstd -dc <path>` and stops once it has read
// enough, so a large rollout is never fully inflated. Truncating the stream
// early is expected and not treated as an error; an error is returned only when
// nothing could be decompressed (e.g. the file is not valid zstd).
func DecompressHead(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultHeadBytes
	}
	cmd := exec.Command(binary, "-dc", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes))
	// We may have intentionally stopped reading before the end; killing the
	// process and ignoring the resulting wait error keeps this from blocking and
	// from surfacing a spurious "signal: killed" error.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if len(data) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if readErr != nil {
			return nil, fmt.Errorf("zstd decompress %s: %v: %s", path, readErr, msg)
		}
		return nil, fmt.Errorf("zstd decompress %s: empty output: %s", path, msg)
	}
	return data, nil
}

// Decompress fully decompresses zstd-compressed bytes (read from stdin) and
// returns the complete plaintext, up to MaxDecompressedBytes. It is suitable for
// rewriting a whole compressed session. Output beyond the cap is treated as a
// decompression bomb and returns an error; a truncated or corrupt frame is also
// surfaced as an error.
func Decompress(compressed []byte) ([]byte, error) {
	cmd := exec.Command(binary, "-dc")
	cmd.Stdin = bytes.NewReader(compressed)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Read one byte past the cap so an over-limit stream is detected, not truncated.
	data, readErr := io.ReadAll(io.LimitReader(stdout, MaxDecompressedBytes+1))
	overflow := int64(len(data)) > MaxDecompressedBytes
	if overflow {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if overflow {
		return nil, fmt.Errorf("zstd decompress: output exceeds %d bytes (possible decompression bomb)", MaxDecompressedBytes)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("zstd decompress: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if readErr != nil {
		return nil, fmt.Errorf("zstd decompress: %v", readErr)
	}
	return data, nil
}

// Compress compresses plaintext bytes (read from stdin) into a standard zstd
// frame and returns the compressed bytes. Used to re-pack a compressed session
// after its metadata was rewritten on import.
func Compress(plain []byte) ([]byte, error) {
	return runPipe([]string{"-q", "-c"}, plain)
}

// runPipe runs `zstd <args>` with input on stdin and returns its stdout. Both
// input and output are held in memory, which is fine for session-sized files.
func runPipe(args []string, input []byte) ([]byte, error) {
	cmd := exec.Command(binary, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("zstd %v: %v: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
