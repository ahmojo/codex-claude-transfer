package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/meta"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// run is a tiny helper that runs a command and returns stdout, stderr, exit code.
func run(args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestConfigSetGetList(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	if _, e, c := run("config", "set", "tool", "claude"); c != 0 {
		t.Fatalf("set tool exit=%d %s", c, e)
	}
	if o, _, c := run("config", "get", "tool"); c != 0 || strings.TrimSpace(o) != "claude" {
		t.Fatalf("get tool = %q exit=%d", o, c)
	}
	if o, _, _ := run("config", "list"); !strings.Contains(o, "tool") || !strings.Contains(o, "claude") {
		t.Fatalf("list missing tool:\n%s", o)
	}
	// Invalid key/value are rejected.
	if _, _, c := run("config", "set", "tool", "vim"); c == 0 {
		t.Fatal("invalid tool should fail")
	}
}

func TestConfigDefaultHomeApplied(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	t.Setenv("CODEX_HOME", "") // ensure env doesn't supply the home
	home := filepath.Join(t.TempDir(), "cx")
	writeSessionMsg(t, home, "aaaa1111-2222-3333-4444-555566667777", "/p", "hi")
	if _, e, c := run("config", "set", "codex-home", home); c != 0 {
		t.Fatalf("set codex-home: %s", e)
	}
	// doctor with no --codex-home should pick up the configured home.
	o, e, c := run("doctor", "--json")
	if c != 0 {
		t.Fatalf("doctor exit=%d %s", c, e)
	}
	// The JSON output escapes backslashes on Windows, so escape the expected path
	// the same way before comparing.
	want := strings.ReplaceAll(home, `\`, `\\`)
	if !strings.Contains(o, want) {
		t.Fatalf("doctor did not use the configured home:\n%s", o)
	}
}

func TestTagAndNameRoundTrip(t *testing.T) {
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	home := filepath.Join(t.TempDir(), "home")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, id, "/p", "tag me")

	if o, e, c := run("tag", "add", "aaaa1111", "WIP", "--codex-home", home); c != 0 {
		t.Fatalf("tag add exit=%d %s", c, e)
	} else if !strings.Contains(o, "wip") {
		t.Fatalf("tag add output:\n%s", o)
	}
	if o, _, _ := run("tag", "ls", "--codex-home", home); !strings.Contains(o, "wip") {
		t.Fatalf("tag ls missing wip:\n%s", o)
	}
	if o, _, _ := run("tag", "ls", "wip", "--codex-home", home); !strings.Contains(o, id) {
		t.Fatalf("tag ls wip missing id:\n%s", o)
	}
	if o, e, c := run("name", "aaaa1111", "my", "session", "--codex-home", home); c != 0 {
		t.Fatalf("name exit=%d %s", c, e)
	} else if !strings.Contains(o, "my session") {
		t.Fatalf("name output:\n%s", o)
	}
	if o, _, _ := run("name", "aaaa1111", "--codex-home", home); !strings.Contains(o, "my session") {
		t.Fatalf("name show missing:\n%s", o)
	}
	// Removing the tag.
	if o, _, _ := run("tag", "rm", "aaaa1111", "wip", "--codex-home", home); !strings.Contains(o, "Untagged") {
		t.Fatalf("tag rm output:\n%s", o)
	}
}

func TestStatsCommand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	writeSessionMsg(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a", "one")
	writeSessionMsg(t, home, "bbbb1111-2222-3333-4444-555566667777", "/proj/a", "two")
	o, e, c := run("stats", "--codex-home", home)
	if c != 0 {
		t.Fatalf("stats exit=%d %s", c, e)
	}
	if !strings.Contains(o, "Sessions:") || !strings.Contains(o, "/proj/a") {
		t.Fatalf("stats output unexpected:\n%s", o)
	}
	// JSON form is valid and reports the total.
	oj, _, cj := run("stats", "--codex-home", home, "--json")
	if cj != 0 || !strings.Contains(oj, "\"total\": 2") {
		t.Fatalf("stats --json:\n%s", oj)
	}
}

func TestResumePrintsCommand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, id, "/p", "how to build a rate limiter")
	writeSessionMsg(t, home, "bbbb1111-2222-3333-4444-555566667777", "/p", "unrelated")
	o, e, c := run("resume", "rate limiter", "--codex-home", home)
	if c != 0 {
		t.Fatalf("resume exit=%d %s", c, e)
	}
	if !strings.Contains(o, "codex resume "+id) {
		t.Fatalf("resume did not print the right command:\n%s", o)
	}
}

func TestResumeCommandBuilder(t *testing.T) {
	if n, a := resumeCommand(agent.Codex, "ID"); n != "codex" || a[0] != "resume" || a[1] != "ID" {
		t.Fatalf("codex resume = %s %v", n, a)
	}
	if n, a := resumeCommand(agent.Claude, "ID"); n != "claude" || a[0] != "--resume" || a[1] != "ID" {
		t.Fatalf("claude resume = %s %v", n, a)
	}
}

func TestExportFormatHTML(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, id, "/p", "hello <b>html</b>")
	out := filepath.Join(tmp, "chat.html")
	if _, e, c := run("export", "--session", id, "--codex-home", home, "--format", "html", "-o", out); c != 0 {
		t.Fatalf("export html exit=%d %s", c, e)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read html: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "<!doctype html>") {
		t.Fatal("not an HTML document")
	}
	if strings.Contains(s, "<b>html</b>") || !strings.Contains(s, "&lt;b&gt;html&lt;/b&gt;") {
		t.Fatalf("html content not escaped:\n%s", s)
	}
}

func TestExportSecretGate(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, id, "/p", "deploy with AKIAIOSFODNN7EXAMPLE")

	bundle := filepath.Join(tmp, "b.codexbundle")
	// Default: blocked, bundle not left behind.
	_, e, c := run("export", "--session", id, "--codex-home", home, "-o", bundle)
	if c == 0 {
		t.Fatal("export should be blocked by the secret gate")
	}
	if !strings.Contains(e, "secret") {
		t.Fatalf("unexpected error: %s", e)
	}
	if _, err := os.Stat(bundle); err == nil {
		t.Fatal("blocked export left a bundle behind")
	}

	// --allow-secrets proceeds.
	if _, e, c := run("export", "--session", id, "--codex-home", home, "-o", bundle, "--allow-secrets"); c != 0 {
		t.Fatalf("--allow-secrets export failed: %s", e)
	}
	if _, err := os.Stat(bundle); err != nil {
		t.Fatalf("allowed export produced no bundle: %v", err)
	}

	// --redact proceeds and reports a redaction.
	out2 := filepath.Join(tmp, "b2.codexbundle")
	o, e, c := run("export", "--session", id, "--codex-home", home, "-o", out2, "--redact")
	if c != 0 {
		t.Fatalf("--redact export failed: %s", e)
	}
	if !strings.Contains(o, "Secrets redacted") {
		t.Fatalf("redact export did not report redaction:\n%s", o)
	}
}

func TestFilterBrowseSortsRecent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	older := "aaaa1111-2222-3333-4444-555566667777"
	newer := "bbbb1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, older, "/p", "old")
	writeSessionMsg(t, home, newer, "/p", "new")
	// Bump newer's mtime so order is deterministic.
	p := filepath.Join(home, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+newer+".jsonl")
	future := mustFuture()
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}

	scan, code := scanForSearch(commonFlags{codexHome: home}, agent.Codex, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("scan failed: %d", code)
	}
	got := filterBrowse(scan.Sessions, "", commonFlags{})
	if len(got) < 2 || got[0].ThreadID != newer {
		t.Fatalf("filterBrowse did not sort newest-first: %v", threadIDs(got))
	}
}

func threadIDs(list []sessions.Session) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.ThreadID
	}
	return out
}

func TestBrowseLabel(t *testing.T) {
	s := sessions.Session{ThreadID: "id", Preview: "fix the parser", CWD: "/home/u/proj"}
	got := browseLabel(s, meta.Entry{Name: "renamed", Tags: []string{"wip", "review"}})
	// A cct name overrides the preview; tags are shown in brackets.
	if !strings.Contains(got, "renamed") || !strings.Contains(got, "[wip,review]") {
		t.Fatalf("browseLabel = %q", got)
	}
	if strings.Contains(got, "fix the parser") {
		t.Fatalf("name should override preview: %q", got)
	}
}

func mustFuture() time.Time { return time.Now().Add(2 * time.Hour) }
