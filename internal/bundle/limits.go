package bundle

import (
	"archive/zip"
	"fmt"
	"io"
)

// Bundle resource limits, enforced before reading or writing untrusted bundle
// content. They bound the damage a malicious bundle can do via decompression
// bombs or oversized entries (memory, CPU, and disk exhaustion). See the
// "Resource limits" findings in docs/security/audit.md (SEC-2/SEC-3).
const (
	// MaxSessionBytes caps a single session entry's uncompressed size.
	MaxSessionBytes = 100 << 20 // 100 MiB
	// MaxMetadataBytes caps manifest.json / checksums.json uncompressed size.
	MaxMetadataBytes = 16 << 20 // 16 MiB
	// MaxBundleEntries caps the number of files in a bundle.
	MaxBundleEntries = 100_000
	// MaxBundleUncompressed caps the total uncompressed size across all entries.
	MaxBundleUncompressed = 2 << 30 // 2 GiB
)

// readCapped reads up to max bytes from r, returning an error if the stream would
// exceed max. Reading max+1 lets a lying ZIP/zstd header be detected rather than
// silently truncated.
func readCapped(r io.Reader, max int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit (possible decompression bomb)", what, max)
	}
	return data, nil
}

// checkDeclaredSize cheaply rejects a ZIP entry whose declared uncompressed size
// already exceeds max, before any inflation. The actual read is still capped
// separately, in case the header understates the real size.
func checkDeclaredSize(f *zip.File, max int64, what string) error {
	if f.UncompressedSize64 > uint64(max) {
		return fmt.Errorf("%s declares %d bytes, over the %d-byte limit", what, f.UncompressedSize64, max)
	}
	return nil
}
