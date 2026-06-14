package bundle

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// This file implements the content rewriting for `import --import-as-copy`: when
// a bundle session conflicts with a diverged local file, the bundle's version is
// imported as a brand-new session instead of being skipped. A fresh session id
// is assigned (so Codex treats it as a distinct conversation) and the file is
// written under a new rollout filename derived from that id. Like --map-cwd, this
// is a deliberately narrow mutation: only the canonical "id" field of the
// session_meta line changes; every other line is preserved byte-for-byte.

// newSessionID returns a random RFC 4122 version-4 UUID string, matching the
// shape Codex uses for session/thread ids. Generated from crypto/rand so no
// third-party UUID dependency is needed.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// rewriteSessionMetaID returns a copy of plain JSONL bytes with the id field of
// the (first) session_meta line replaced by newID. It returns the previous id so
// the caller can derive a matching rollout filename. Every other line is
// preserved byte-for-byte, and all other session_meta fields (including unknown
// ones) are preserved. changed is false when there is no session_meta line with
// an id to reassign, in which case the original bytes are returned unchanged.
func rewriteSessionMetaID(content []byte, newID string) (out []byte, oldID string, changed bool, err error) {
	lines := splitKeepTerminators(content)
	for i := range lines {
		trimmed := bytes.TrimSpace(lines[i].text)
		if len(trimmed) == 0 {
			continue
		}
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(trimmed, &wrapper) != nil {
			continue // tolerate non-JSON lines
		}
		if lineTypeOf(wrapper) != "session_meta" {
			continue
		}
		newLine, prevID, didChange, rerr := rewriteMetaWrapperID(wrapper, newID)
		if rerr != nil {
			return nil, "", false, rerr
		}
		if !didChange {
			return content, prevID, false, nil
		}
		lines[i].text = newLine
		oldID = prevID
		changed = true
		break
	}
	if !changed {
		return content, "", false, nil
	}
	var buf bytes.Buffer
	for _, ln := range lines {
		buf.Write(ln.text)
		buf.Write(ln.term)
	}
	return buf.Bytes(), oldID, true, nil
}

// rewriteMetaWrapperID rewrites the id inside a parsed session_meta wrapper,
// preserving every other (including unknown) field via generic maps.
func rewriteMetaWrapperID(wrapper map[string]json.RawMessage, newID string) (newLine []byte, oldID string, changed bool, err error) {
	payloadRaw, ok := wrapper["payload"]
	if !ok {
		return nil, "", false, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return nil, "", false, nil
	}
	idRaw, ok := payload["id"]
	if !ok {
		return nil, "", false, nil
	}
	var cur string
	if err := json.Unmarshal(idRaw, &cur); err != nil {
		return nil, "", false, nil
	}
	if cur == newID {
		return nil, cur, false, nil
	}
	nb, err := json.Marshal(newID)
	if err != nil {
		return nil, cur, false, err
	}
	payload["id"] = nb
	pb, err := json.Marshal(payload)
	if err != nil {
		return nil, cur, false, err
	}
	wrapper["payload"] = pb
	wb, err := json.Marshal(wrapper)
	if err != nil {
		return nil, cur, false, err
	}
	return wb, cur, true, nil
}

// validateCopiedJSONL performs a minimal safety re-check of the copied output,
// mirroring validateMappedJSONL:
//   - same number of lines as the original,
//   - every non-empty line is valid JSON,
//   - exactly the session_meta line carries the new id,
//   - all other lines are byte-identical to the original.
func validateCopiedJSONL(original, copied []byte, newID string) error {
	oLines := splitKeepTerminators(original)
	cLines := splitKeepTerminators(copied)
	if len(oLines) != len(cLines) {
		return fmt.Errorf("line count changed (%d -> %d)", len(oLines), len(cLines))
	}
	metaSeen, changed := 0, 0
	for i := range cLines {
		cText := cLines[i].text
		cTrim := bytes.TrimSpace(cText)
		if len(cTrim) == 0 {
			if !bytes.Equal(cText, oLines[i].text) {
				return fmt.Errorf("line %d changed unexpectedly", i+1)
			}
			continue
		}
		if !json.Valid(cTrim) {
			return fmt.Errorf("line %d is not valid JSON after copying", i+1)
		}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(cTrim, &wrapper); err != nil {
			return fmt.Errorf("line %d: %w", i+1, err)
		}
		if lineTypeOf(wrapper) == "session_meta" {
			metaSeen++
			id, err := idOf(wrapper)
			if err != nil {
				return fmt.Errorf("line %d: cannot read id: %w", i+1, err)
			}
			if id == newID {
				changed++
				continue
			}
			// A non-matching session_meta must be unchanged.
			if !bytes.Equal(cText, oLines[i].text) {
				return fmt.Errorf("line %d: session_meta changed but id was not updated", i+1)
			}
			continue
		}
		if !bytes.Equal(cText, oLines[i].text) {
			return fmt.Errorf("line %d changed but is not session_meta", i+1)
		}
	}
	if metaSeen == 0 {
		return fmt.Errorf("session_meta line missing after copying")
	}
	if changed == 0 {
		return fmt.Errorf("no id was updated")
	}
	return nil
}

func idOf(wrapper map[string]json.RawMessage) (string, error) {
	payloadRaw, ok := wrapper["payload"]
	if !ok {
		return "", fmt.Errorf("no payload")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return "", err
	}
	idRaw, ok := payload["id"]
	if !ok {
		return "", fmt.Errorf("no id")
	}
	var id string
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return "", err
	}
	return id, nil
}

// copyDestRel derives a new bundle-relative path for a copied session. Codex
// rollout files are named rollout-<ts>-<uuid>.jsonl, where the uuid is the
// session id, so the new filename embeds newID. When the original filename
// contains oldID it is replaced in place (preserving the timestamp); otherwise
// newID is appended before the .jsonl extension. The result still matches the
// sessions/YYYY/MM/DD/rollout-*.jsonl shape required by safety.IsSessionEntry.
func copyDestRel(rel, oldID, newID string) string {
	dir := path.Dir(rel)
	base := path.Base(rel)
	if oldID != "" && strings.Contains(base, oldID) {
		return path.Join(dir, strings.Replace(base, oldID, newID, 1))
	}
	if strings.HasSuffix(base, ".jsonl") {
		stem := strings.TrimSuffix(base, ".jsonl")
		return path.Join(dir, stem+"-"+newID+".jsonl")
	}
	return path.Join(dir, base+"-"+newID)
}
