package bundle

import (
	"crypto/sha256"
	"encoding/hex"
)

// Checksums maps each bundle-relative file path (forward slashes) to its
// SHA-256 hex digest. It is written as checksums.json at the root of the ZIP
// and covers every file in the bundle except checksums.json itself.
type Checksums map[string]string

// sha256Hex returns the hex-encoded SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
