// Package claudesessions discovers and defensively parses Claude Code session
// transcripts under ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl.
//
// Unlike Codex rollout files (a type/payload wrapper per line), Claude Code writes
// one flat JSON object per line whose fields live at the top level (type, uuid,
// sessionId, cwd, version, gitBranch, timestamp, message{role,content}, ...). The
// transcript is the durable record; Claude Code rediscovers a dropped-in file with
// no registry to update.
//
// Claude Code is closed-source and its format moves quickly, so all parsing here is
// best-effort: unknown fields are ignored, bad lines become warnings rather than
// fatal errors, and a single unreadable file never aborts a scan. The per-line
// "version" is surfaced so format drift is visible.
package claudesessions

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// Scan limits: we read only enough of each transcript to recover metadata.
const (
	maxLinesScanned = 400
	maxLineBytes    = 4 * 1024 * 1024
)

// transcriptLine is the subset of a Claude Code transcript line we read. Every
// field is optional and tolerated absent.
type transcriptLine struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	SessionID   string          `json:"sessionId"`
	CWD         string          `json:"cwd"`
	Version     string          `json:"version"`
	GitBranch   string          `json:"gitBranch"`
	Timestamp   string          `json:"timestamp"`
	Entrypoint  string          `json:"entrypoint"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

// claudeMeta is the best-effort metadata recovered from a transcript.
type claudeMeta struct {
	SessionID  string
	CWD        string
	Version    string
	GitBranch  string
	CreatedAt  string
	Entrypoint string
	Preview    string
	FirstUser  string
	Sidechain  bool
	FoundAny   bool
}

// parseTranscript reads a transcript head-first and recovers session metadata.
func parseTranscript(path string) (claudeMeta, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return claudeMeta{}, nil, err
	}
	defer f.Close()
	meta, warnings := parseTranscriptReader(f)
	return meta, warnings, nil
}

func parseTranscriptReader(r io.Reader) (meta claudeMeta, warnings []string) {
	reader := bufio.NewReader(r)
	lineNo := 0
	for lineNo < maxLinesScanned {
		raw, readErr := readLine(reader)
		if len(raw) > 0 {
			lineNo++
			trimmed := strings.TrimSpace(string(raw))
			if trimmed != "" {
				consumeLine(trimmed, &meta)
				// Stop once we have the identifying metadata and a preview.
				if meta.SessionID != "" && meta.CWD != "" && meta.FirstUser != "" {
					break
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return meta, warnings
}

func consumeLine(line string, meta *claudeMeta) {
	var tl transcriptLine
	if err := json.Unmarshal([]byte(line), &tl); err != nil {
		return // tolerate non-JSON lines silently; defensive by design
	}
	meta.FoundAny = true
	if meta.SessionID == "" && tl.SessionID != "" {
		meta.SessionID = tl.SessionID
	}
	if meta.CWD == "" && tl.CWD != "" {
		meta.CWD = tl.CWD
	}
	if meta.Version == "" && tl.Version != "" {
		meta.Version = tl.Version
	}
	if meta.GitBranch == "" && tl.GitBranch != "" {
		meta.GitBranch = tl.GitBranch
	}
	if meta.CreatedAt == "" && tl.Timestamp != "" {
		meta.CreatedAt = tl.Timestamp
	}
	if meta.Entrypoint == "" && tl.Entrypoint != "" {
		meta.Entrypoint = tl.Entrypoint
	}
	if tl.IsSidechain {
		meta.Sidechain = true
	}
	if meta.FirstUser == "" && tl.Type == "user" {
		if text := userMessageText(tl.Message); text != "" {
			meta.FirstUser = text
			meta.Preview = preview(text)
		}
	}
}

// userMessageText extracts displayable text from a Claude message field, which
// may be a plain string or an array of content blocks ({type:"text",text:...}).
func userMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}
	// content as a plain string
	var s string
	if json.Unmarshal(msg.Content, &s) == nil && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	// content as an array of blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(msg.Content, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return strings.TrimSpace(b.Text)
			}
		}
	}
	return ""
}

func preview(msg string) string {
	const max = 100
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > max {
		return msg[:max] + "…"
	}
	return msg
}

// applyMeta fills a sessions.Session from recovered Claude metadata. The shared
// Session model is reused so the render/manifest layers are agent-agnostic; the
// Source field records the launching front-end (entrypoint) when present.
func applyMeta(s *sessions.Session, meta claudeMeta, fallbackID string) {
	s.ThreadID = meta.SessionID
	if s.ThreadID == "" {
		s.ThreadID = fallbackID
	}
	s.CWD = meta.CWD
	s.CLIVersion = meta.Version
	s.GitBranch = meta.GitBranch
	s.Source = meta.Entrypoint
	s.FirstUserMessage = meta.FirstUser
	s.Preview = meta.Preview
	s.CreatedAt = meta.CreatedAt
	// A transcript is "parsed" if we recovered at least a session id or cwd.
	s.Parsed = meta.SessionID != "" || meta.CWD != ""
	if meta.Sidechain {
		s.Warnings = append(s.Warnings, "transcript contains sub-agent sidechain lines; resume behavior for sidechains is not yet verified")
	}
	if !s.Parsed {
		s.Warnings = append(s.Warnings, "no sessionId/cwd found; file may be incomplete or a newer Claude Code format")
	}
}

// readLine reads a single line, discarding any portion beyond maxLineBytes so a
// huge record cannot exhaust memory.
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
