package lansync

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Protocol version; both peers must agree.
const protocolVersion = 1

// Frame/size caps.
const (
	maxJSONFrame   = 8 << 20   // control messages (manifests can be large but bounded)
	maxBundleBytes = 512 << 20 // a single transferred bundle
)

// authMsg is each side's authenticated hello: its identity plus the HMAC that
// proves knowledge of the pairing code over the captured TLS fingerprints.
type authMsg struct {
	Version  int    `json:"version"`
	Hostname string `json:"hostname"`
	Tool     string `json:"tool"`
	DryRun   bool   `json:"dry_run"`
	MAC      []byte `json:"mac"`
}

// syncEntry is one session in a peer's manifest — the minimum needed to diff.
type syncEntry struct {
	ThreadID string `json:"thread_id"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// manifestMsg is a peer's full in-scope session list.
type manifestMsg struct {
	Tool     string      `json:"tool"`
	Sessions []syncEntry `json:"sessions"`
}

// writeFrame writes a length-prefixed (4-byte BE) byte slice.
func writeFrame(w io.Writer, data []byte) error {
	if len(data) > maxJSONFrame {
		return fmt.Errorf("frame too large (%d bytes)", len(data))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readFrame reads a length-prefixed byte slice, rejecting oversized frames.
func readFrame(r io.Reader, max int) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if int(n) > max {
		return nil, fmt.Errorf("frame of %d bytes exceeds limit %d", n, max)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, data)
}

func readJSON(r io.Reader, v any) error {
	data, err := readFrame(r, maxJSONFrame)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeBlob streams a bundle: an 8-byte BE length, then exactly that many bytes
// copied from src. A length of 0 means "no bundle".
func writeBlob(w io.Writer, src io.Reader, size int64) error {
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(size))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	n, err := io.CopyN(w, src, size)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("short blob write: %d of %d", n, size)
	}
	return nil
}

// readBlob reads a streamed bundle into dst, returning the number of bytes. It
// rejects an over-large declared size before copying.
func readBlob(r io.Reader, dst io.Writer) (int64, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, err
	}
	size := binary.BigEndian.Uint64(hdr[:])
	if size == 0 {
		return 0, nil
	}
	if size > maxBundleBytes {
		return 0, fmt.Errorf("incoming bundle of %d bytes exceeds limit %d", size, maxBundleBytes)
	}
	return io.CopyN(dst, r, int64(size))
}
