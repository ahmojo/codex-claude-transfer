package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// validBundleBytes builds a minimal, well-formed bundle in memory to seed the
// fuzzer's corpus (so it explores mutations of a real bundle, not just garbage).
func validBundleBytes() []byte {
	data := []byte(`{"type":"session_meta","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":"/p"}}` + "\n")
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Sessions: []ManifestSession{{
			ThreadID:    "aaaa1111-2222-3333-4444-555566667777",
			BundlePath:  sampleRel,
			OriginalCWD: "/p",
			SizeBytes:   int64(len(data)),
			SHA256:      sha256Hex(data),
		}},
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{sampleRel: sha256Hex(data), ManifestName: sha256Hex(mb)}
	cb, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name string
		data []byte
	}{{sampleRel, data}, {ManifestName, mb}, {ChecksumsName, cb}} {
		w, _ := zw.Create(e.name)
		w.Write(e.data)
	}
	zw.Close()
	return buf.Bytes()
}

// FuzzImport feeds arbitrary bytes as a bundle to Import and Inspect. A malicious
// or corrupt bundle (now also a network-delivered input via `cct sync`) must be
// rejected with an error, never a panic, and never a write outside the home.
func FuzzImport(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("not a zip at all"))
	f.Add([]byte("PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")) // empty zip EOCD
	f.Add(validBundleBytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "b.codexbundle")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Skip()
		}
		home := fakeHome(t)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic on bundle input (%d bytes): %v", len(data), r)
			}
		}()
		// Errors are expected and fine; a panic or a write-escape is not.
		_, _ = Inspect(p)
		_, _ = Import(home, ImportOptions{BundlePath: p})
		// Nothing may have been written outside the home root.
		for _, f := range listFilesRel(t, home.Root) {
			if filepath.IsAbs(f) || len(f) >= 2 && f[1] == ':' {
				t.Fatalf("import wrote an unsafe path: %q", f)
			}
		}
	})
}
