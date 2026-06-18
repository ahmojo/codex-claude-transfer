package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportStripImages writes a session with a big inline image, exports it with
// StripImages, and verifies the payload is gone, the text survives, and the
// resulting bundle still imports cleanly (so its recorded checksum matches the
// stripped bytes).
func TestExportStripImages(t *testing.T) {
	home := fakeHome(t)
	project := "/Users/example/dev/imgproj"
	name := "rollout-2026-06-13T18-22-01-cccc3333-4444-5555-6666-777788889999.jsonl"
	full := filepath.Join(home.SessionsDir, "2026", "06", "13", name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("Z", 5000)
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"cccc3333-4444-5555-6666-777788889999","cwd":"` + project + `","source":"cli","model_provider":"openai"}}` + "\n" +
		`{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"see this screenshot"}}` + "\n" +
		`{"timestamp":"z","type":"response_item","payload":{"content":[{"type":"input_image","image_url":"data:image/png;base64,` + payload + `"}]}}` + "\n"
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "img.codexbundle")
	res, err := Export(home, ExportOptions{ProjectPath: project, OutputPath: out, StripImages: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if res.ImagesStripped != 1 {
		t.Fatalf("ImagesStripped = %d, want 1", res.ImagesStripped)
	}
	if res.BytesSaved <= 0 {
		t.Errorf("BytesSaved = %d, want > 0", res.BytesSaved)
	}

	var entry []byte
	for n, b := range readBundle(t, out) {
		if strings.HasSuffix(n, name) {
			entry = b
		}
	}
	if entry == nil {
		t.Fatal("session entry not found in bundle")
	}
	if bytes.Contains(entry, []byte(payload)) {
		t.Error("image payload is still present in the bundle")
	}
	if !bytes.Contains(entry, []byte("see this screenshot")) {
		t.Error("conversation text was lost")
	}

	// The stripped bundle must import cleanly: a checksum mismatch (recording the
	// pre-strip hash) would make this fail.
	target := fakeHome(t)
	ir, err := Import(target, ImportOptions{BundlePath: out})
	if err != nil {
		t.Fatalf("import stripped bundle: %v", err)
	}
	if ir.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (checksum must match the stripped bytes)", ir.Imported)
	}
}
