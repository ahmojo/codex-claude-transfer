package bundle

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// bigPayload is a base64-ish string long enough to be treated as a real image.
var bigPayload = strings.Repeat("A", 4000)

func TestStripImagesDataURI(t *testing.T) {
	line := []byte(`{"type":"message","content":[{"type":"input_image","image_url":"data:image/png;base64,` + bigPayload + `"}]}` + "\n")
	out, n := StripImagesJSONL(line)
	if n != 1 {
		t.Fatalf("stripped %d images, want 1", n)
	}
	if bytes.Contains(out, []byte(bigPayload)) {
		t.Error("payload still present after stripping")
	}
	if !bytes.Contains(out, []byte("data:image/png;base64,")) {
		t.Error("data-URI prefix was not kept")
	}
	// The result must still be valid JSON.
	var v any
	if err := json.Unmarshal(bytes.TrimSpace(out), &v); err != nil {
		t.Errorf("stripped line is not valid JSON: %v", err)
	}
}

func TestStripImagesBase64Source(t *testing.T) {
	line := []byte(`{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"` + bigPayload + `"}}]}` + "\n")
	out, n := StripImagesJSONL(line)
	if n != 1 {
		t.Fatalf("stripped %d images, want 1", n)
	}
	if bytes.Contains(out, []byte(bigPayload)) {
		t.Error("base64 source data still present")
	}
	if !bytes.Contains(out, []byte(`"media_type":"image/jpeg"`)) && !bytes.Contains(out, []byte(`"media_type": "image/jpeg"`)) {
		t.Error("non-image fields should be preserved")
	}
}

func TestStripImagesLeavesOtherLinesByteIdentical(t *testing.T) {
	meta := `{"timestamp":"2026-06-13T10:00:00Z","type":"session_meta","payload":{"id":"x","cwd":"/p"}}`
	text := `{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}`
	img := `{"type":"message","content":[{"image_url":"data:image/png;base64,` + bigPayload + `"}]}`
	input := []byte(meta + "\n" + text + "\n" + img + "\n")

	out, n := StripImagesJSONL(input)
	if n != 1 {
		t.Fatalf("stripped %d, want 1", n)
	}
	lines := bytes.Split(out, []byte("\n"))
	// The two non-image lines must be untouched, byte-for-byte.
	if string(lines[0]) != meta {
		t.Errorf("meta line changed:\n got %s\nwant %s", lines[0], meta)
	}
	if string(lines[1]) != text {
		t.Errorf("text line changed:\n got %s\nwant %s", lines[1], text)
	}
}

func TestStripImagesNoImagesIsByteIdentical(t *testing.T) {
	input := []byte(`{"a":1,"b":"hello"}` + "\n" + `{"c":[1,2,3]}` + "\n")
	out, n := StripImagesJSONL(input)
	if n != 0 {
		t.Fatalf("stripped %d, want 0", n)
	}
	if !bytes.Equal(out, input) {
		t.Errorf("output changed despite no images:\n got %q\nwant %q", out, input)
	}
}

func TestStripImagesSmallDataURIKept(t *testing.T) {
	// A tiny data URI (below the threshold) is left alone.
	input := []byte(`{"icon":"data:image/png;base64,iVBORw0KGgo="}` + "\n")
	out, n := StripImagesJSONL(input)
	if n != 0 {
		t.Fatalf("stripped %d small data URIs, want 0", n)
	}
	if !bytes.Equal(out, input) {
		t.Errorf("small data URI was modified: %q", out)
	}
}

func TestStripImagesPreservesNoFinalNewline(t *testing.T) {
	input := []byte(`{"a":1}` + "\n" + `{"b":2}`) // no trailing newline
	out, n := StripImagesJSONL(input)
	if n != 0 {
		t.Fatalf("stripped %d, want 0", n)
	}
	if !bytes.Equal(out, input) {
		t.Errorf("output changed:\n got %q\nwant %q", out, input)
	}
}
