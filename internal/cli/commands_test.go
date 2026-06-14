package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSession(t *testing.T, home, threadID, cwd string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":"` + cwd + `","source":"cli"}}
{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"Auth refactor"}}
`
	name := "rollout-2026-06-13T18-22-01-" + threadID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRunListWithFakeHome(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "aaaa1111-2222-3333-4444-555566667777", "/Users/example/dev/project")

	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", home}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "Found 1 Codex session") {
		t.Errorf("missing session count: %s", s)
	}
	if !strings.Contains(s, "Auth refactor") {
		t.Errorf("missing preview title: %s", s)
	}
	if !strings.Contains(s, "/Users/example/dev/project") {
		t.Errorf("missing cwd: %s", s)
	}
}

func TestRunDoctorWithFakeHome(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "bbbb1111-2222-3333-4444-555566667777", "/x")

	var out, errOut bytes.Buffer
	code := Run([]string{"doctor", "--codex-home", home}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "SQLite will not be modified") {
		t.Errorf("doctor output missing SQLite safety line: %s", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"frobnicate"}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for unknown command, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("expected unknown command message")
	}
}

func TestRunNoArgsShowsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(nil, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Usage") {
		t.Errorf("expected usage text")
	}
}

func TestRunUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", t.TempDir(), "--bogus"}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for unknown flag, got %d", code)
	}
}

func TestRunEmptyHomeListsNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"list", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "No Codex sessions found") {
		t.Errorf("expected empty message, got: %s", out.String())
	}
}

func TestParseSinceDate(t *testing.T) {
	got, err := parseSince("2026-06-01")
	if err != nil {
		t.Fatalf("parseSince date: %v", err)
	}
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSinceDuration(t *testing.T) {
	before := time.Now()
	got, err := parseSince("7d")
	if err != nil {
		t.Fatalf("parseSince duration: %v", err)
	}
	want := before.Add(-7 * 24 * time.Hour)
	if diff := got.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Errorf("7d cutoff off by %v (got %v want ~%v)", diff, got, want)
	}
}

func TestParseSinceInvalid(t *testing.T) {
	for _, s := range []string{"", "notadate", "7x", "-3d", "2026/06/01"} {
		if _, err := parseSince(s); err == nil {
			t.Errorf("expected error for %q", s)
		}
	}
}

func TestRunExportAllProjectConflict(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--project", ".", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for --all + --project, got %d", code)
	}
	if !strings.Contains(errOut.String(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got: %s", errOut.String())
	}
}

func TestRunExportBadSince(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--since", "garbage", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for invalid --since, got %d", code)
	}
}

func TestRunExportSessionWithAllConflict(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--session", "abcd", "--all", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for --session + --all, got %d", code)
	}
	if !strings.Contains(errOut.String(), "--session cannot be combined") {
		t.Errorf("expected session-conflict error, got: %s", errOut.String())
	}
}

func TestRunExportSessionWithProjectConflict(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--session", "abcd", "--project", ".", "--codex-home", t.TempDir()}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2 for --session + --project, got %d", code)
	}
}

func TestRunExportSessionByID(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "abcd1111-2222-3333-4444-555566667777", "/proj/a")
	out := filepath.Join(t.TempDir(), "s.codexbundle")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"export", "--session", "abcd1111", "--codex-home", home, "-o", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("bundle not written: %v", err)
	}
}

// writeSessionCWD writes a fixture whose cwd is JSON-escaped, so paths with
// backslashes (Windows) remain valid JSON.
func writeSessionCWD(t *testing.T, home, threadID, cwd string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":` + string(cwdJSON) + `,"source":"cli"}}` + "\n" +
		`{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"hello"}}` + "\n"
	name := "rollout-2026-06-13T18-22-01-" + threadID + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRunImportWithGitHandoffAndClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	// A real project repo pushed to a bare remote.
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	runGit(t, proj, "init")
	if err := os.WriteFile(filepath.Join(proj, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(t, proj, "add", ".")
	runGit(t, proj, "commit", "-m", "first")
	bare := filepath.Join(tmp, "remote.git")
	runGit(t, tmp, "init", "--bare", bare)
	runGit(t, proj, "remote", "add", "origin", bare)
	runGit(t, proj, "push", "origin", "HEAD")

	// A Codex session whose recorded cwd is the project path.
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777", proj)

	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--project", proj, "--with-git", "--codex-home", srcHome, "-o", bundle}, &out, &errOut)
	if code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	// Dry-run import should surface the git handoff hint.
	dstHome := filepath.Join(tmp, "home2")
	if err := os.MkdirAll(dstHome, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	out.Reset()
	errOut.Reset()
	code = Run([]string{"import", bundle, "--dry-run", "--codex-home", dstHome}, &out, &errOut)
	if code != 0 {
		t.Fatalf("dry-run import exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "git clone") {
		t.Errorf("dry-run import missing git handoff hint:\n%s", out.String())
	}

	// Real import with --clone should clone the repo into the target dir.
	cloneDir := filepath.Join(tmp, "cloned")
	out.Reset()
	errOut.Reset()
	code = Run([]string{"import", bundle, "--clone", cloneDir, "--codex-home", dstHome}, &out, &errOut)
	if code != 0 {
		t.Fatalf("import --clone exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Clone complete") {
		t.Errorf("missing clone-complete message:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(cloneDir, "f.txt")); err != nil {
		t.Errorf("cloned project file missing: %v", err)
	}
}

func TestRunImportCloneWithoutGitRemoteErrors(t *testing.T) {
	tmp := t.TempDir()
	// A bundle without any git info: a plain session, exported by --all.
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}
	dstHome := filepath.Join(tmp, "home2")
	if err := os.MkdirAll(dstHome, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	out.Reset()
	errOut.Reset()
	code := Run([]string{"import", bundle, "--clone", filepath.Join(tmp, "x"), "--codex-home", dstHome}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 when --clone has no remote, got %d", code)
	}
	if !strings.Contains(errOut.String(), "no git remote") {
		t.Errorf("expected no-remote error, got: %s", errOut.String())
	}
}

func TestSanitizeForFilename(t *testing.T) {
	cases := map[string]string{
		"abcd1111-2222": "abcd1111-2222",
		"a/b\\c":        "a_b_c",
		"":              "codex-session",
	}
	for in, want := range cases {
		if got := sanitizeForFilename(in); got != want {
			t.Errorf("sanitizeForFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
