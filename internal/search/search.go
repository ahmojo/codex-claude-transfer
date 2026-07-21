// Package search provides full-text search over local session transcripts. It is
// read-only and agent-agnostic: it extracts the human-readable string values from
// each JSONL line (skipping keys and obvious binary/base64 blobs) and matches a
// literal or regular-expression query against them. It backs both `cct search` and
// `export --match`.
package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// Query describes what to look for.
type Query struct {
	Text          string
	Regex         bool
	CaseSensitive bool
}

// Match is one session that matched, with a hit count and a short snippet.
type Match struct {
	Session sessions.Session
	Hits    int
	Snippet string
}

const (
	maxLineBytes  = 4 << 20 // skip absurdly long lines (e.g. embedded media)
	maxStringScan = 4 << 10 // skip individual string values longer than this with no spaces (base64/data)
	snippetCtx    = 60      // characters of context on each side of a hit
)

type matcher struct {
	re  *regexp.Regexp // non-nil for regex or compiled-literal matching
	raw string
}

func newMatcher(q Query) (matcher, error) {
	if strings.TrimSpace(q.Text) == "" {
		return matcher{}, fmt.Errorf("empty search query")
	}
	pat := q.Text
	if !q.Regex {
		pat = regexp.QuoteMeta(pat)
	}
	if !q.CaseSensitive {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return matcher{}, fmt.Errorf("invalid search pattern: %w", err)
	}
	return matcher{re: re, raw: q.Text}, nil
}

// Search runs the query against every session and returns the matches, sorted by
// hit count (desc) then most-recent. Unreadable sessions are skipped silently.
func Search(list []sessions.Session, q Query) ([]Match, error) {
	m, err := newMatcher(q)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, s := range list {
		if s.Compressed {
			continue // not decompressed here; v1 searches plain .jsonl only
		}
		hits, snippet, err := scanFile(s.Path, m)
		if err != nil || hits == 0 {
			continue
		}
		out = append(out, Match{Session: s, Hits: hits, Snippet: snippet})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		return out[i].Session.UpdatedAt().After(out[j].Session.UpdatedAt())
	})
	return out, nil
}

// SessionMatches reports whether a single session file matches (used by
// export --match, where only a yes/no is needed).
func SessionMatches(path string, q Query) (bool, error) {
	m, err := newMatcher(q)
	if err != nil {
		return false, err
	}
	hits, _, err := scanFile(path, m)
	return hits > 0, err
}

// Matches reports whether an in-memory transcript (raw .jsonl bytes) matches the
// query. It applies the same content extraction as file search, so callers that
// already hold the bytes — e.g. import --match scanning a bundle entry — need not
// write a temp file. Compressed (.jsonl.zst) content must be decompressed first.
func Matches(data []byte, q Query) (bool, error) {
	m, err := newMatcher(q)
	if err != nil {
		return false, err
	}
	hits, _, err := scanReader(bufio.NewReader(bytes.NewReader(data)), m)
	return hits > 0, err
}

// scanFile reads a transcript, extracts readable text per line, and counts hits.
func scanFile(path string, m matcher) (hits int, snippet string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	return scanReader(bufio.NewReader(f), m)
}

// scanReader extracts readable text per line from r and counts hits.
func scanReader(r *bufio.Reader, m matcher) (hits int, snippet string, err error) {
	for {
		line, readErr := readLine(r, maxLineBytes)
		if len(line) > 0 {
			text := extractText(line)
			if text != "" {
				if locs := m.re.FindAllStringIndex(text, -1); len(locs) > 0 {
					hits += len(locs)
					if snippet == "" {
						snippet = makeSnippet(text, locs[0])
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return hits, snippet, nil
}

// contentKeys are the JSON keys whose string values hold conversation text. We
// only search under these so snippets are clean message text, not metadata like
// timestamps, type tags, or ids.
var contentKeys = map[string]bool{"message": true, "content": true, "text": true}

// extractText decodes a JSONL line and joins the human-readable text found under
// content keys, skipping metadata and long space-free blobs (base64/data URIs).
func extractText(line []byte) string {
	var v any
	if json.Unmarshal(line, &v) != nil {
		return "" // tolerate non-JSON lines
	}
	var b strings.Builder
	collect(v, &b, false)
	return b.String()
}

func collect(v any, b *strings.Builder, underContent bool) {
	switch t := v.(type) {
	case string:
		if !underContent || isLikelyBlob(t) {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
	case []any:
		for _, e := range t {
			collect(e, b, underContent)
		}
	case map[string]any:
		for k, e := range t {
			collect(e, b, underContent || contentKeys[k])
		}
	}
}

func isLikelyBlob(s string) bool {
	return len(s) > maxStringScan && !strings.ContainsAny(s, " \n\t")
}

// makeSnippet returns a trimmed, single-line window around a hit.
func makeSnippet(text string, loc []int) string {
	start := loc[0] - snippetCtx
	if start < 0 {
		start = 0
	}
	end := loc[1] + snippetCtx
	if end > len(text) {
		end = len(text)
	}
	s := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s = s + "…"
	}
	return s
}

// readLine reads one line, discarding any bytes beyond max so a huge record cannot
// exhaust memory (the discarded tail just isn't searched).
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if len(buf)+len(chunk) <= max {
			buf = append(buf, chunk...)
		}
		if !isPrefix || err != nil {
			return buf, err
		}
	}
}
