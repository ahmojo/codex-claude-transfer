package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Scan limits, mirroring the spirit of Codex's own head/event scan limits. We
// only read enough of each file to recover metadata, never the whole file.
const (
	maxLinesScanned = 400
	// maxLineBytes caps an individual line we will buffer. Rollout lines can be
	// large (full response items); we skip lines longer than this when looking
	// for metadata rather than loading them fully.
	maxLineBytes = 4 * 1024 * 1024
)

// rolloutLine is the on-disk wrapper for each JSONL record. Codex serializes
// RolloutItem with an internal "type" tag and a "payload" object, plus a
// recorder timestamp. Unknown fields are ignored.
type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// sessionMetaPayload mirrors the SessionMeta(+git) fields we care about. All
// fields are optional and tolerant of absence.
type sessionMetaPayload struct {
	ID            string          `json:"id"`
	Timestamp     string          `json:"timestamp"`
	CWD           string          `json:"cwd"`
	Originator    string          `json:"originator"`
	CLIVersion    string          `json:"cli_version"`
	Source        json.RawMessage `json:"source"`
	ModelProvider string          `json:"model_provider"`
	Git           json.RawMessage `json:"git"`
}

type eventMsgPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// parsedMeta is the best-effort metadata recovered from a rollout file.
type parsedMeta struct {
	ThreadID         string
	CWD              string
	Originator       string
	CLIVersion       string
	Source           string
	ModelProvider    string
	GitBranch        string
	GitSHA           string
	GitOriginURL     string
	CreatedAt        string
	FirstUserMessage string
	Preview          string
	FoundSessionMeta bool
}

// parsePlainRollout reads a plain .jsonl rollout file line-by-line, extracting
// the SessionMeta and the first user message. It tolerates invalid lines,
// returning them as warnings instead of failing. It never loads the whole file
// into memory and stops early once it has what it needs.
func parsePlainRollout(path string) (meta parsedMeta, warnings []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return parsedMeta{}, nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	lineNo := 0
	for lineNo < maxLinesScanned {
		raw, readErr := readLine(reader)
		if len(raw) > 0 {
			lineNo++
			trimmed := strings.TrimSpace(string(raw))
			if trimmed != "" {
				if w := consumeLine(trimmed, lineNo, &meta); w != "" {
					warnings = append(warnings, w)
				}
				// Stop early once we have both the meta and a first user message.
				if meta.FoundSessionMeta && meta.FirstUserMessage != "" {
					break
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				warnings = append(warnings, fmt.Sprintf("read error: %v", readErr))
			}
			break
		}
	}
	return meta, warnings, nil
}

// consumeLine parses a single JSONL line into meta. It returns a warning string
// if the line could not be parsed (empty string means no warning).
func consumeLine(line string, lineNo int, meta *parsedMeta) string {
	var rl rolloutLine
	if err := json.Unmarshal([]byte(line), &rl); err != nil {
		return fmt.Sprintf("line %d: invalid JSON, skipped", lineNo)
	}
	switch rl.Type {
	case "session_meta":
		applySessionMeta(rl.Payload, meta)
	case "event_msg":
		applyEventMsg(rl.Payload, meta)
	}
	return ""
}

func applySessionMeta(payload json.RawMessage, meta *parsedMeta) {
	var p sessionMetaPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	meta.FoundSessionMeta = true
	meta.ThreadID = p.ID
	meta.CWD = p.CWD
	meta.Originator = p.Originator
	meta.CLIVersion = p.CLIVersion
	meta.ModelProvider = p.ModelProvider
	if p.Timestamp != "" {
		meta.CreatedAt = p.Timestamp
	}
	meta.Source = decodeSource(p.Source)
	meta.GitBranch, meta.GitSHA, meta.GitOriginURL = decodeGit(p.Git)
}

func applyEventMsg(payload json.RawMessage, meta *parsedMeta) {
	if meta.FirstUserMessage != "" {
		return
	}
	var e eventMsgPayload
	if err := json.Unmarshal(payload, &e); err != nil {
		return
	}
	if e.Type == "user_message" {
		msg := stripUserMessageMarker(strings.TrimSpace(e.Message))
		if msg != "" {
			meta.FirstUserMessage = msg
			if meta.Preview == "" {
				meta.Preview = preview(msg)
			}
		}
	}
}

// decodeSource handles SessionSource being either a plain string (unit variants
// like "cli"/"appServer") or an object (e.g. {"custom":"atlas"}).
func decodeSource(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		for k, v := range obj {
			var inner string
			if json.Unmarshal(v, &inner) == nil && inner != "" {
				return inner
			}
			return k
		}
	}
	return ""
}

// decodeGit pulls branch/sha/origin from the git object, trying several
// plausible key names since the exact Codex GitInfo field names may vary.
func decodeGit(raw json.RawMessage) (branch, sha, origin string) {
	if len(raw) == 0 {
		return "", "", ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", "", ""
	}
	branch = firstString(m, "branch", "git_branch")
	sha = firstString(m, "sha", "commit_hash", "commit", "git_sha")
	origin = firstString(m, "origin_url", "repository_url", "remote_url", "git_origin_url")
	return branch, sha, origin
}

func firstString(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

// stripUserMessageMarker removes Codex's internal user-message begin marker if
// present. The exact marker can change; we strip a known prefix shape
// defensively without depending on its precise value.
func stripUserMessageMarker(msg string) string {
	const marker = "USER_MESSAGE_BEGIN"
	if idx := strings.Index(msg, marker); idx != -1 {
		msg = msg[idx+len(marker):]
	}
	return strings.TrimSpace(msg)
}

func preview(msg string) string {
	const max = 100
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > max {
		return msg[:max] + "…"
	}
	return msg
}

// readLine reads a single line, discarding lines longer than maxLineBytes so a
// huge record cannot exhaust memory. It returns the line bytes (without the
// trailing newline) and any read error.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if len(buf)+len(chunk) <= maxLineBytes {
			buf = append(buf, chunk...)
		}
		if !isPrefix || err != nil {
			return buf, err
		}
	}
}
