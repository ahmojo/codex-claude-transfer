package bundle

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path"
	"reflect"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
)

// Claude Code records the project working directory in two places: the encoded
// folder name (projects/<encoded-cwd>/) and a top-level "cwd" field on (nearly)
// every transcript line. A faithful cwd remap therefore (1) rewrites the cwd field
// on every line that carries the old path and (2) moves the file into the folder
// for the new path's encoding. Both are validated before anything is written.
//
// This is the Claude analog of the Codex session_meta cwd rewrite in mapcwd.go;
// the difference is that Claude repeats cwd per line and addresses the project by
// folder name, so more lines change and the destination path changes too.

// rewriteClaudeCWD returns a copy of a Claude transcript with every top-level
// "cwd" field equal to oldCWD replaced by newCWD. Lines without a matching cwd are
// preserved byte-for-byte; matching lines are re-serialized as JSON (key order may
// change, but the content is semantically identical apart from cwd). changed
// reports whether at least one cwd was rewritten.
func rewriteClaudeCWD(content []byte, oldCWD, newCWD string) (out []byte, changed bool, err error) {
	lines := splitKeepTerminators(content)
	newCWDJSON, err := json.Marshal(newCWD)
	if err != nil {
		return nil, false, err
	}
	for i := range lines {
		trimmed := bytes.TrimSpace(lines[i].text)
		if len(trimmed) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(trimmed, &obj) != nil {
			continue // tolerate non-object lines
		}
		cwdRaw, ok := obj["cwd"]
		if !ok {
			continue
		}
		var cwd string
		if json.Unmarshal(cwdRaw, &cwd) != nil {
			continue
		}
		if !pathEqual(cwd, oldCWD) {
			continue
		}
		obj["cwd"] = newCWDJSON
		rewritten, merr := json.Marshal(obj)
		if merr != nil {
			return nil, false, merr
		}
		lines[i].text = rewritten
		changed = true
	}
	if !changed {
		return content, false, nil
	}
	var buf bytes.Buffer
	for _, ln := range lines {
		buf.Write(ln.text)
		buf.Write(ln.term)
	}
	return buf.Bytes(), true, nil
}

// validateClaudeMappedCWD re-checks a Claude cwd remap: the line count is
// unchanged, every non-empty line is valid JSON, at least one cwd was changed from
// oldCWD to newCWD, and every line is otherwise identical to the original ignoring
// only its top-level "cwd" field. It guards against the rewrite altering anything
// it should not.
func validateClaudeMappedCWD(original, mapped []byte, oldCWD, newCWD string) error {
	oLines := splitKeepTerminators(original)
	mLines := splitKeepTerminators(mapped)
	if len(oLines) != len(mLines) {
		return fmt.Errorf("line count changed (%d -> %d)", len(oLines), len(mLines))
	}
	changed := 0
	for i := range mLines {
		oText := bytes.TrimSpace(oLines[i].text)
		mText := bytes.TrimSpace(mLines[i].text)
		if len(mText) == 0 {
			if !bytes.Equal(oLines[i].text, mLines[i].text) {
				return fmt.Errorf("line %d changed unexpectedly", i+1)
			}
			continue
		}
		if !json.Valid(mText) {
			return fmt.Errorf("line %d is not valid JSON after mapping", i+1)
		}
		oCWD, oRest, oObj := splitCWD(oText)
		mCWD, mRest, mObj := splitCWD(mText)
		if !oObj || !mObj {
			// Non-object line: must be byte-identical.
			if !bytes.Equal(oLines[i].text, mLines[i].text) {
				return fmt.Errorf("line %d changed but is not a JSON object", i+1)
			}
			continue
		}
		if !reflect.DeepEqual(oRest, mRest) {
			return fmt.Errorf("line %d changed a field other than cwd", i+1)
		}
		switch {
		case oCWD != "" && pathEqual(oCWD, oldCWD):
			if mCWD != newCWD {
				return fmt.Errorf("line %d: cwd was not updated to the new path", i+1)
			}
			changed++
		default:
			if mCWD != oCWD {
				return fmt.Errorf("line %d: cwd changed unexpectedly", i+1)
			}
		}
	}
	if changed == 0 {
		return fmt.Errorf("no cwd was updated")
	}
	return nil
}

// splitCWD parses one transcript line into its cwd string and the rest of the
// object (cwd removed), so two lines can be compared ignoring cwd. ok is false
// when the line is not a JSON object.
func splitCWD(line []byte) (cwd string, rest map[string]interface{}, ok bool) {
	var obj map[string]interface{}
	if json.Unmarshal(line, &obj) != nil {
		return "", nil, false
	}
	if c, present := obj["cwd"]; present {
		if s, isStr := c.(string); isStr {
			cwd = s
		}
		delete(obj, "cwd")
	}
	return cwd, obj, true
}

// claudeDestRelForCWD returns the bundle-relative destination for a Claude
// transcript after its cwd is remapped to newCWD: the same uuid filename, but
// under the folder encoding newCWD.
func claudeDestRelForCWD(rel, newCWD string) string {
	file := path.Base(rel)
	return path.Join(claudehome.ProjectsSubdir, claudehome.EncodeCWD(newCWD), file)
}

// rewriteClaudeSessionID returns a copy of a Claude transcript with every
// top-level "sessionId" field replaced by newID (used by --import-as-copy so a
// diverged session is imported as a brand-new conversation). Every other field is
// preserved; matching lines are re-serialized. oldID is the first sessionId seen.
// changed reports whether at least one sessionId was rewritten.
func rewriteClaudeSessionID(content []byte, newID string) (out []byte, oldID string, changed bool, err error) {
	lines := splitKeepTerminators(content)
	newIDJSON, err := json.Marshal(newID)
	if err != nil {
		return nil, "", false, err
	}
	for i := range lines {
		trimmed := bytes.TrimSpace(lines[i].text)
		if len(trimmed) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(trimmed, &obj) != nil {
			continue
		}
		idRaw, ok := obj["sessionId"]
		if !ok {
			continue
		}
		var id string
		if json.Unmarshal(idRaw, &id) != nil {
			continue
		}
		if oldID == "" {
			oldID = id
		}
		obj["sessionId"] = newIDJSON
		rewritten, merr := json.Marshal(obj)
		if merr != nil {
			return nil, "", false, merr
		}
		lines[i].text = rewritten
		changed = true
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

// claudeCopyDestRel returns the destination for an imported-as-copy Claude
// transcript: the same project folder, but a new uuid filename.
func claudeCopyDestRel(rel, newID string) string {
	dir := path.Dir(rel)
	return path.Join(dir, newID+claudehome.SessionExt)
}

// newClaudeSessionID generates a fresh lowercase UUIDv4 for a copied session.
func newClaudeSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
