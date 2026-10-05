package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// CWDMapping rewrites a session's recorded working directory from Old to New
// during import. It is the only feature that mutates session content, and it
// updates cwd and matching runtime roots in metadata and persisted thread settings.
type CWDMapping struct {
	Old string
	New string
}

// winDriveAbs recognizes a Windows drive-letter absolute path (e.g. C:\x or
// C:/x) on any host OS, so mappings remain portable across platforms.
var winDriveAbs = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// ParseCWDMappings parses and validates a list of "OLD=NEW" mapping specs.
//
// Rules:
//   - syntax must be OLD=NEW (split on the first '='),
//   - OLD must not be empty,
//   - NEW must not be empty and must be absolute/plausible for the target OS,
//   - OLD and NEW must differ after normalization,
//   - duplicate OLD paths (case-insensitive on Windows) are rejected.
func ParseCWDMappings(specs []string) ([]CWDMapping, error) {
	var out []CWDMapping
	seen := map[string]string{} // normalized old -> original old (for error messages)
	for _, s := range specs {
		idx := strings.Index(s, "=")
		if idx < 0 {
			return nil, fmt.Errorf("invalid --map-cwd %q: expected OLD=NEW", s)
		}
		old := s[:idx]
		newPath := s[idx+1:]
		if old == "" {
			return nil, fmt.Errorf("invalid --map-cwd %q: OLD path is empty", s)
		}
		if newPath == "" {
			return nil, fmt.Errorf("invalid --map-cwd %q: NEW path is empty", s)
		}
		if !isPlausibleAbs(newPath) {
			return nil, fmt.Errorf("invalid --map-cwd %q: NEW path %q must be absolute", s, newPath)
		}
		if pathEqual(old, newPath) {
			return nil, fmt.Errorf("invalid --map-cwd %q: OLD and NEW are the same path", s)
		}
		key := normalizeCWD(old)
		if prev, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate --map-cwd for old path %q (already mapped via %q)", old, prev)
		}
		seen[key] = old
		out = append(out, CWDMapping{Old: old, New: newPath})
	}
	return out, nil
}

// isPlausibleAbs reports whether p looks like an absolute path for either the
// host OS, a Unix root, or a Windows drive. We never create this directory; the
// check only guards against obviously relative NEW values.
func isPlausibleAbs(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/") {
		return true
	}
	return winDriveAbs.MatchString(p)
}

// normalizeCWD returns a canonical key for path comparison/dedup, matching the
// case-insensitivity used for cwd handling on Windows.
func normalizeCWD(p string) string {
	c := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(c)
	}
	return c
}

// resolveMapHere builds the cwd mapping for --map-cwd-here: it maps the bundle's
// single recorded project cwd to hereDir (the caller's current directory). It
// requires exactly one distinct source cwd; a bundle spanning several projects is
// ambiguous and rejected with guidance to use explicit --map-cwd. When the sole
// recorded cwd already equals hereDir, it returns no mapping (a plain import
// already lands the sessions correctly) and an explanatory note instead.
func resolveMapHere(manifest Manifest, hereDir string) (mappings []CWDMapping, note string, err error) {
	if hereDir == "" {
		return nil, "", fmt.Errorf("--map-cwd-here: could not determine the current directory")
	}
	if !isPlausibleAbs(hereDir) {
		return nil, "", fmt.Errorf("--map-cwd-here: current directory %q is not absolute", hereDir)
	}
	seen := map[string]string{} // normalized key -> original spelling, first seen
	var order []string
	for _, ms := range ConversationSessions(manifest.Sessions) {
		if ms.OriginalCWD == "" {
			continue
		}
		k := normalizeCWD(ms.OriginalCWD)
		if _, ok := seen[k]; !ok {
			seen[k] = ms.OriginalCWD
			order = append(order, k)
		}
	}
	switch len(order) {
	case 0:
		return nil, "", fmt.Errorf("--map-cwd-here: the bundle has no recorded project cwd to map (nothing to remap)")
	case 1:
		old := seen[order[0]]
		if pathEqual(old, hereDir) {
			return nil, "--map-cwd-here: these sessions were already recorded under this folder; importing as-is", nil
		}
		return []CWDMapping{{Old: old, New: hereDir}}, "", nil
	default:
		paths := make([]string, 0, len(order))
		for _, k := range order {
			paths = append(paths, seen[k])
		}
		sort.Strings(paths)
		return nil, "", fmt.Errorf("--map-cwd-here: the bundle spans %d projects (%s); it is ambiguous which one to map here. Use --map-cwd \"<old>=%s\" for the project you want", len(order), strings.Join(paths, ", "), hereDir)
	}
}

// matchMapping returns the first mapping whose Old equals cwd, or nil. Because
// ParseCWDMappings rejects duplicate OLD paths, at most one mapping can match.
func matchMapping(cwd string, mappings []CWDMapping) *CWDMapping {
	if cwd == "" {
		return nil
	}
	for i := range mappings {
		if pathEqual(mappings[i].Old, cwd) {
			return &mappings[i]
		}
	}
	return nil
}

// jsonLine is one physical line of a JSONL file split into its content and the
// exact terminator that followed it, so unchanged lines can be reproduced
// byte-for-byte (including \n vs \r\n and a missing final newline).
type jsonLine struct {
	text []byte
	term []byte
}

func splitKeepTerminators(content []byte) []jsonLine {
	var lines []jsonLine
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		text := content[start:i]
		term := content[i : i+1]
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
			term = content[i-1 : i+1]
		}
		lines = append(lines, jsonLine{text: text, term: term})
		start = i + 1
	}
	if start < len(content) {
		lines = append(lines, jsonLine{text: content[start:], term: nil})
	}
	return lines
}

// rewriteSessionMetaCWD maps matching metadata and persisted thread settings.
// Conversation records and unknown fields are preserved.
// changed reports whether a rewrite actually happened.
func rewriteSessionMetaCWD(content []byte, oldCWD, newCWD string) (out []byte, changed bool, err error) {
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
		var newLine []byte
		var didChange bool
		var rerr error
		if lineTypeOf(wrapper) == "session_meta" {
			newLine, didChange, rerr = rewriteMetaWrapper(wrapper, oldCWD, newCWD)
		} else {
			newLine, didChange, rerr = rewriteRuntimeSettings(wrapper, oldCWD, newCWD)
		}
		if rerr != nil {
			return nil, false, rerr
		}
		if !didChange {
			continue
		}
		lines[i].text = newLine
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

// rewriteMetaWrapper rewrites the cwd inside a parsed session_meta wrapper,
// preserving every other (including unknown) field via generic maps.
func rewriteMetaWrapper(wrapper map[string]json.RawMessage, oldCWD, newCWD string) (newLine []byte, changed bool, err error) {
	payloadRaw, ok := wrapper["payload"]
	if !ok {
		return nil, false, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return nil, false, nil
	}
	cwdRaw, ok := payload["cwd"]
	if !ok {
		return nil, false, nil
	}
	var cwd string
	if err := json.Unmarshal(cwdRaw, &cwd); err != nil {
		return nil, false, nil
	}
	if !pathEqual(cwd, oldCWD) {
		return nil, false, nil
	}
	nb, err := json.Marshal(newCWD)
	if err != nil {
		return nil, false, err
	}
	payload["cwd"] = nb
	mapRuntimeRoots(payload, oldCWD, newCWD)
	pb, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	wrapper["payload"] = pb
	wb, err := json.Marshal(wrapper)
	if err != nil {
		return nil, false, err
	}
	return wb, true, nil
}

func mapRuntimeRoots(payload map[string]json.RawMessage, oldCWD, newCWD string) {
	var roots []string
	if json.Unmarshal(payload["runtime_workspace_roots"], &roots) != nil {
		return
	}
	changed := false
	for i, root := range roots {
		if pathEqual(root, oldCWD) {
			roots[i], changed = newCWD, true
		}
	}
	if changed {
		payload["runtime_workspace_roots"], _ = json.Marshal(roots)
	}
}

func rewriteRuntimeSettings(wrapper map[string]json.RawMessage, oldCWD, newCWD string) ([]byte, bool, error) {
	if lineTypeOf(wrapper) != "event_msg" {
		return nil, false, nil
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(wrapper["payload"], &payload) != nil {
		return nil, false, nil
	}
	var typ string
	if json.Unmarshal(payload["type"], &typ) != nil || typ != "thread_settings_applied" {
		return nil, false, nil
	}
	settingsWrapper := map[string]json.RawMessage{"payload": payload["thread_settings"]}
	_, changed, err := rewriteMetaWrapper(settingsWrapper, oldCWD, newCWD)
	if err != nil || !changed {
		return nil, false, err
	}
	payload["thread_settings"] = settingsWrapper["payload"]
	wrapper["payload"], err = json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	line, err := json.Marshal(wrapper)
	return line, true, err
}

// validateMappedJSONL rejects changes outside the expected cwd/settings rewrite.
func validateMappedJSONL(original, mapped []byte, newCWD string) error {
	var oldCWD string
	for _, line := range splitKeepTerminators(original) {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(line.text, &wrapper) == nil && lineTypeOf(wrapper) == "session_meta" {
			oldCWD, _ = cwdOf(wrapper)
			break
		}
	}
	if oldCWD == "" {
		return fmt.Errorf("session_meta cwd missing")
	}
	expected, changed, err := rewriteSessionMetaCWD(original, oldCWD, newCWD)
	if err != nil {
		return err
	}
	if !changed || !bytes.Equal(expected, mapped) {
		return fmt.Errorf("mapped content differs from the expected cwd/settings rewrite")
	}
	for i, line := range splitKeepTerminators(mapped) {
		if len(bytes.TrimSpace(line.text)) > 0 && !json.Valid(line.text) {
			return fmt.Errorf("line %d is not valid JSON after mapping", i+1)
		}
	}
	return nil
}

func lineTypeOf(wrapper map[string]json.RawMessage) string {
	raw, ok := wrapper["type"]
	if !ok {
		return ""
	}
	var t string
	_ = json.Unmarshal(raw, &t)
	return t
}

func cwdOf(wrapper map[string]json.RawMessage) (string, error) {
	payloadRaw, ok := wrapper["payload"]
	if !ok {
		return "", fmt.Errorf("no payload")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return "", err
	}
	cwdRaw, ok := payload["cwd"]
	if !ok {
		return "", fmt.Errorf("no cwd")
	}
	var cwd string
	if err := json.Unmarshal(cwdRaw, &cwd); err != nil {
		return "", err
	}
	return cwd, nil
}
