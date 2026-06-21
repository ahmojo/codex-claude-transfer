package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

func write(t *testing.T, dir, name, body string) sessions.Session {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessions.Session{Path: p, ThreadID: strings.TrimSuffix(name, ".jsonl")}
}

const (
	sessA = `{"type":"session_meta","payload":{"id":"a","cwd":"/p"}}
{"type":"event_msg","payload":{"type":"user_message","message":"how do I build a rate limiter in Go"}}
`
	sessB = `{"type":"session_meta","payload":{"id":"b","cwd":"/p"}}
{"type":"event_msg","payload":{"type":"user_message","message":"explain minecraft redstone circuits"}}
`
)

func TestSearchLiteral(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.jsonl", sessA)
	b := write(t, dir, "b.jsonl", sessB)
	m, err := Search([]sessions.Session{a, b}, Query{Text: "rate limiter"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[0].Session.ThreadID != "a" {
		t.Fatalf("got %d matches, want 1 (a): %+v", len(m), m)
	}
	if !strings.Contains(m[0].Snippet, "rate limiter") {
		t.Errorf("snippet missing the hit: %q", m[0].Snippet)
	}
}

func TestSearchCaseInsensitiveByDefault(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.jsonl", sessA)
	m, _ := Search([]sessions.Session{a}, Query{Text: "RATE LIMITER"})
	if len(m) != 1 {
		t.Errorf("case-insensitive search missed it: %d", len(m))
	}
}

func TestSearchRegex(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.jsonl", sessA)
	b := write(t, dir, "b.jsonl", sessB)
	m, err := Search([]sessions.Session{a, b}, Query{Text: "redstone|rate", Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Errorf("regex matched %d, want 2", len(m))
	}
}

// TestSearchIgnoresMetadata: a query that only appears in a non-content field
// (type tag) must NOT match — search only looks at message text.
func TestSearchIgnoresMetadata(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.jsonl", sessA)
	if m, _ := Search([]sessions.Session{a}, Query{Text: "session_meta"}); len(m) != 0 {
		t.Errorf("matched a metadata field value: %+v", m)
	}
	if m, _ := Search([]sessions.Session{a}, Query{Text: "user_message"}); len(m) != 0 {
		t.Errorf("matched a type tag: %+v", m)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	if _, err := Search(nil, Query{Text: "  "}); err == nil {
		t.Error("expected an error for an empty query")
	}
}
