package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// minStrippedImageChars is the smallest base64 payload worth replacing. Real
// screenshots/photos are many kilobytes; this avoids touching tiny inline data
// (e.g. a small icon) where the placeholder would not save anything meaningful.
const minStrippedImageChars = 256

// StripImagesJSONL removes inline base64 image payloads from a rollout/transcript
// JSONL buffer, returning the rewritten bytes and the number of images stripped.
//
// It is lossy and strictly opt-in (export --strip-images): the conversation text
// is kept, but embedded picture bytes are replaced with a short placeholder so an
// image-heavy bundle becomes much smaller. It recognizes the two shapes that
// appear in Codex and Claude Code transcripts:
//
//   - data URIs: "data:image/png;base64,<payload>" (the OpenAI/Codex shape) — the
//     "data:...;base64," prefix is kept and the payload replaced.
//   - base64 source objects: {"type":"base64","data":"<payload>", ...} (the
//     Anthropic shape) — the "data" field is replaced.
//
// Lines and any content without an image are preserved byte-for-byte; only the
// objects on the path to a stripped image are re-encoded, so the file otherwise
// stays exactly as it was.
func StripImagesJSONL(plain []byte) (out []byte, count int) {
	var buf bytes.Buffer
	total := 0
	i := 0
	for i < len(plain) {
		// Carve out one line, remembering its exact terminator (\n, \r\n, or none).
		end := bytes.IndexByte(plain[i:], '\n')
		var line []byte
		hasNL := false
		if end < 0 {
			line = plain[i:]
			i = len(plain)
		} else {
			line = plain[i : i+end]
			i = i + end + 1
			hasNL = true
		}
		body := line
		cr := false
		if len(body) > 0 && body[len(body)-1] == '\r' {
			cr = true
			body = body[:len(body)-1]
		}

		if len(bytes.TrimSpace(body)) == 0 {
			buf.Write(body)
		} else if stripped, n := stripImagesInValue(json.RawMessage(body)); n > 0 {
			buf.Write(stripped)
			total += n
		} else {
			buf.Write(body)
		}
		if cr {
			buf.WriteByte('\r')
		}
		if hasNL {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), total
}

// stripImagesInValue recursively walks a JSON value and replaces image payloads.
// It returns the (possibly rewritten) value and how many images were replaced. A
// value that contains no image is returned unchanged (its original bytes), so
// untouched structure keeps its exact original encoding.
func stripImagesInValue(raw json.RawMessage) (json.RawMessage, int) {
	// String: a data URI is the only string we rewrite.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if repl, ok := strippedDataURI(s); ok {
			b, _ := json.Marshal(repl)
			return b, 1
		}
		return raw, 0
	}

	// Object.
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		count := 0
		// Anthropic-style image source: {"type":"base64","data":"<payload>"}.
		if isBase64Source(obj) {
			if d, ok := obj["data"]; ok {
				var ds string
				if json.Unmarshal(d, &ds) == nil && len(ds) >= minStrippedImageChars {
					b, _ := json.Marshal(imagePlaceholder(len(ds)))
					obj["data"] = b
					count++
				}
			}
		}
		for k, v := range obj {
			nv, c := stripImagesInValue(v)
			if c > 0 {
				obj[k] = nv
				count += c
			}
		}
		if count > 0 {
			b, _ := json.Marshal(obj)
			return b, count
		}
		return raw, 0
	}

	// Array.
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		count := 0
		for idx := range arr {
			nv, c := stripImagesInValue(arr[idx])
			if c > 0 {
				arr[idx] = nv
				count += c
			}
		}
		if count > 0 {
			b, _ := json.Marshal(arr)
			return b, count
		}
		return raw, 0
	}

	return raw, 0
}

// strippedDataURI replaces the payload of a base64 data URI, keeping the
// "data:<mime>;base64," prefix so the value still reads as an image reference.
func strippedDataURI(s string) (string, bool) {
	if !strings.HasPrefix(s, "data:") {
		return "", false
	}
	marker := ";base64,"
	i := strings.Index(s, marker)
	if i < 0 {
		return "", false
	}
	payload := s[i+len(marker):]
	if len(payload) < minStrippedImageChars {
		return "", false
	}
	return s[:i+len(marker)] + imagePlaceholder(len(payload)), true
}

// isBase64Source reports whether an object is an Anthropic-style base64 image
// source ({"type":"base64", ...}).
func isBase64Source(obj map[string]json.RawMessage) bool {
	t, ok := obj["type"]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(t, &s) == nil && s == "base64"
}

// imagePlaceholder is the short text left in place of stripped image bytes.
func imagePlaceholder(n int) string {
	return fmt.Sprintf("[image stripped by cct: %d base64 chars]", n)
}
