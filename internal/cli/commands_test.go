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

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
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

func TestRunExportGitPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tmp := t.TempDir()
	// A project repo with a remote whose HEAD has NOT been pushed.
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGit(t, proj, "init")
	if err := os.WriteFile(filepath.Join(proj, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, proj, "add", ".")
	runGit(t, proj, "commit", "-m", "first")
	bare := filepath.Join(tmp, "remote.git")
	runGit(t, tmp, "init", "--bare", bare)
	runGit(t, proj, "remote", "add", "origin", bare)

	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777", proj)
	bundlePath := filepath.Join(tmp, "p.codexbundle")

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--project", proj, "--git-push", "--codex-home", srcHome, "-o", bundlePath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("export --git-push exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Pushed branch") {
		t.Errorf("missing push confirmation:\n%s", out.String())
	}
	// The bare remote must now contain the commit.
	ls := exec.Command("git", "ls-remote", bare)
	if o, err := ls.Output(); err != nil || strings.TrimSpace(string(o)) == "" {
		t.Errorf("remote has no refs after --git-push (err=%v): %q", err, string(o))
	}
	// The bundle's recorded git state should no longer be unpushed (proving the
	// push ran before git metadata was captured).
	res, err := bundle.Inspect(bundlePath)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if res.Manifest.Git == nil || res.Manifest.Git.Unpushed {
		t.Errorf("bundle git should be recorded and not unpushed: %+v", res.Manifest.Git)
	}
}

func TestRunExportGitPushRejectsAll(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "abcd1111-2222-3333-4444-555566667777", "/proj/a")
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--git-push", "--codex-home", home,
		"-o", filepath.Join(tmp, "b.codexbundle")}, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "not valid with --all") {
		t.Errorf("missing --git-push/--all conflict message: %s", errOut.String())
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

func TestRunImportReplaceWithBackup(t *testing.T) {
	tmp := t.TempDir()
	id := "abcd1111-2222-3333-4444-555566667777"

	// Export a session from the source home.
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, id, "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	// In the destination home, the same session exists but has diverged.
	dstHome := filepath.Join(tmp, "home2")
	writeSessionCWD(t, dstHome, id, "/proj/a")
	dest := filepath.Join(dstHome, "sessions", "2026", "06", "13",
		"rollout-2026-06-13T18-22-01-"+id+".jsonl")
	if err := os.WriteFile(dest, []byte("locally diverged content\n"), 0o644); err != nil {
		t.Fatalf("diverge: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Run([]string{"import", bundle, "--replace-with-backup", "--codex-home", dstHome}, &out, &errOut)
	if code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Replaced (backup kept): 1") {
		t.Errorf("missing replaced count in output:\n%s", out.String())
	}
	// The diverged content must survive as a backup somewhere in the home.
	var foundBackup bool
	_ = filepath.WalkDir(dstHome, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(p, ".cct-bak-") {
			if b, _ := os.ReadFile(p); string(b) == "locally diverged content\n" {
				foundBackup = true
			}
		}
		return nil
	})
	if !foundBackup {
		t.Errorf("no backup with the diverged content was kept")
	}
}

func TestRunImportAsCopy(t *testing.T) {
	tmp := t.TempDir()
	id := "abcd1111-2222-3333-4444-555566667777"

	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, id, "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	// In the destination home the same session exists but has diverged.
	dstHome := filepath.Join(tmp, "home2")
	writeSessionCWD(t, dstHome, id, "/proj/a")
	dest := filepath.Join(dstHome, "sessions", "2026", "06", "13",
		"rollout-2026-06-13T18-22-01-"+id+".jsonl")
	const diverged = "locally diverged content\n"
	if err := os.WriteFile(dest, []byte(diverged), 0o644); err != nil {
		t.Fatalf("diverge: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code := Run([]string{"import", bundle, "--import-as-copy", "--codex-home", dstHome}, &out, &errOut)
	if code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Imported as new copies: 1") {
		t.Errorf("missing copy count in output:\n%s", out.String())
	}
	// The diverged local file must be untouched.
	if b, _ := os.ReadFile(dest); string(b) != diverged {
		t.Errorf("local diverged file was modified: %q", string(b))
	}
	// A second rollout file (the copy, with a different id) must now exist.
	var rolloutCount int
	_ = filepath.WalkDir(filepath.Join(dstHome, "sessions"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			rolloutCount++
		}
		return nil
	})
	if rolloutCount != 2 {
		t.Errorf("expected 2 rollout files (original + copy), got %d", rolloutCount)
	}
}

func TestRunImportReplaceAndCopyMutuallyExclusive(t *testing.T) {
	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "p.codexbundle")
	// The flags are rejected before the bundle is even read, so an empty path is fine.
	var out, errOut bytes.Buffer
	code := Run([]string{"import", bundle, "--replace-with-backup", "--import-as-copy"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "mutually exclusive") {
		t.Errorf("missing mutual-exclusivity message: %s", errOut.String())
	}
}

func TestRunImportSelectSession(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "home")
	id1 := "aaaa1111-2222-3333-4444-555566667777"
	id2 := "bbbb1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, srcHome, id1, "/proj/a")
	writeSessionCWD(t, srcHome, id2, "/proj/b")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	// Import only the first session (by a unique prefix).
	dstHome := filepath.Join(tmp, "home2")
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"import", bundle, "--session", "aaaa1111", "--codex-home", dstHome}, &out, &errOut); code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "New sessions: 1") {
		t.Errorf("expected 1 new session, output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Skipped (excluded by filter): 1") {
		t.Errorf("expected 1 deselected, output:\n%s", out.String())
	}
	// Only id1's file should exist in the destination.
	gotID1 := filepath.Join(dstHome, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id1+".jsonl")
	gotID2 := filepath.Join(dstHome, "sessions", "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id2+".jsonl")
	if _, err := os.Stat(gotID1); err != nil {
		t.Errorf("selected session was not imported: %v", err)
	}
	if _, err := os.Stat(gotID2); !os.IsNotExist(err) {
		t.Errorf("deselected session should not have been imported")
	}
}

func TestRunImportSelectSessionNoMatchErrors(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "home")
	writeSessionCWD(t, srcHome, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := Run([]string{"import", bundle, "--session", "nope", "--codex-home", filepath.Join(tmp, "home2")}, &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for unmatched --session, got %d", code)
	}
	if !strings.Contains(errOut.String(), "no session in the bundle matches") {
		t.Errorf("missing no-match error: %s", errOut.String())
	}
}

func TestRunExportRejectsMultipleSessions(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--session", "a", "--session", "b", "--codex-home", home,
		"-o", filepath.Join(tmp, "b.codexbundle")}, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "only one --session") {
		t.Errorf("missing multi-session error: %s", errOut.String())
	}
}

func TestRunInspectShowsMissingCWD(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "home")
	// A cwd that does not exist on this machine -> should be flagged missing.
	writeSessionCWD(t, srcHome, "abcd1111-2222-3333-4444-555566667777",
		"/definitely/not/a/real/path/here")
	bundle := filepath.Join(tmp, "p.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", srcHome, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"inspect", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("inspect exit = %d, stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "Project folders (recorded cwd)") {
		t.Errorf("missing cwd summary section:\n%s", s)
	}
	if !strings.Contains(s, "[missing]") {
		t.Errorf("missing folder not flagged:\n%s", s)
	}
	if !strings.Contains(s, "--map-cwd") {
		t.Errorf("missing remap hint:\n%s", s)
	}
}

// writeClaudeTranscript writes a minimal Claude Code transcript under
// <home>/projects/<encoded-cwd>/<sessionID>.jsonl for use in CLI tests.
func writeClaudeTranscript(t *testing.T, home, cwd, sessionID, firstUser string) {
	t.Helper()
	dir := filepath.Join(home, claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line, _ := json.Marshal(map[string]any{
		"type":      "user",
		"uuid":      sessionID,
		"sessionId": sessionID,
		"cwd":       cwd,
		"timestamp": "2026-06-13T10:00:00Z",
		"message":   map[string]any{"role": "user", "content": firstUser},
	})
	path := filepath.Join(dir, sessionID+claudehome.SessionExt)
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

// TestRunImportClaudeShowsProjectGroups: importing a Claude bundle always prints
// the "Project groups" summary (mirroring Claude Code's grouped sidebar), even on
// a clean import where every group folder is the one the session came from.
func TestRunImportClaudeShowsProjectGroups(t *testing.T) {
	tmp := t.TempDir()
	srcHome := filepath.Join(tmp, "src-claude")
	cwd := filepath.Join(tmp, "proj") // a real, existing dir -> shows as [ok]
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	writeClaudeTranscript(t, srcHome, cwd, "abcd1111-2222-3333-4444-555566667777", "hello claude")

	bundle := filepath.Join(tmp, "claude.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--tool", "claude", "--claude-home", srcHome, "--all",
		"-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	dstHome := filepath.Join(tmp, "dst-claude")
	if code := Run([]string{"import", bundle, "--tool", "claude", "--claude-home", dstHome,
		"--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "Project groups (recorded cwd)") {
		t.Errorf("Claude import did not show the project-groups summary:\n%s", s)
	}
	if !strings.Contains(s, cwd) {
		t.Errorf("project-groups summary missing the group path %q:\n%s", cwd, s)
	}
}

// TestRunImportMapCWDHereConflicts: --map-cwd and --map-cwd-here cannot be combined.
func TestRunImportMapCWDHereConflicts(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "aaaa1111-2222-3333-4444-555566667777", "/proj/a")
	bundle := filepath.Join(tmp, "b.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", home, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code := Run([]string{"import", bundle, "--codex-home", filepath.Join(tmp, "dst"),
		"--map-cwd", "/proj/a=/proj/b", "--map-cwd-here", "--dry-run"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "mutually exclusive") {
		t.Errorf("missing mutual-exclusivity error: %s", errOut.String())
	}
}

// TestRunImportMapCWDHere: --map-cwd-here remaps a single-project bundle's cwd to
// the directory the command runs from (no need to look up the old path).
func TestRunImportMapCWDHere(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionCWD(t, home, "aaaa1111-2222-3333-4444-555566667777", "/source/proj")
	bundle := filepath.Join(tmp, "b.codexbundle")
	var out, errOut bytes.Buffer
	if code := Run([]string{"export", "--all", "--codex-home", home, "-o", bundle}, &out, &errOut); code != 0 {
		t.Fatalf("export exit = %d, stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"import", bundle, "--codex-home", filepath.Join(tmp, "dst"),
		"--map-cwd-here"}, &out, &errOut); code != 0 {
		t.Fatalf("import exit = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Remapped cwd: 1") {
		t.Errorf("expected a remap to the current dir:\n%s", out.String())
	}
}

func writeSessionMsg(t *testing.T, home, id, cwd, msg string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdJSON, _ := json.Marshal(cwd)
	body := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":` + string(cwdJSON) + `,"source":"cli"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"` + msg + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+id+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunSearch(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionMsg(t, home, "aaaa1111-2222-3333-4444-555566667777", "/p", "how do I build a rate limiter")
	writeSessionMsg(t, home, "bbbb1111-2222-3333-4444-555566667777", "/p", "explain minecraft redstone")
	var out, errOut bytes.Buffer
	if code := Run([]string{"search", "rate limiter", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "aaaa1111") || strings.Contains(s, "bbbb1111") {
		t.Errorf("search returned wrong sessions:\n%s", s)
	}
}

func TestRunScanFindsSecret(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	writeSessionMsg(t, home, "aaaa1111-2222-3333-4444-555566667777", "/p", "deploy with AKIAIOSFODNN7EXAMPLE")
	var out, errOut bytes.Buffer
	if code := Run([]string{"scan", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "AWS access key") {
		t.Errorf("scan did not report the AWS key:\n%s", out.String())
	}
}

func TestRunExportFormatMarkdown(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionMsg(t, home, id, "/p", "hello markdown")
	out := filepath.Join(tmp, "chat.md")
	var o, e bytes.Buffer
	if code := Run([]string{"export", "--session", id, "--codex-home", home, "--format", "md", "-o", out}, &o, &e); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, e.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	if !strings.Contains(string(data), "# hello markdown") || !strings.Contains(string(data), "## 🧑 User") {
		t.Errorf("markdown missing expected sections:\n%s", data)
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

func TestRunExportPassphraseRecipientConflict(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "eeee1111-2222-3333-4444-555566667777", "/x")

	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--codex-home", home,
		"--passphrase", "--encrypt-to", "age1aaa",
		"-o", filepath.Join(t.TempDir(), "b.codexbundle")}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--passphrase cannot be combined") {
		t.Errorf("missing conflict message: %s", errOut.String())
	}
}

func TestRunImportEncryptedWithoutKeyErrors(t *testing.T) {
	home := t.TempDir()
	enc := filepath.Join(t.TempDir(), "b.codexbundle.age")
	if err := os.WriteFile(enc, []byte("not really age, but .age suffix"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"import", enc, "--codex-home", home}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "encrypted bundle") {
		t.Errorf("missing encrypted-bundle guidance: %s", errOut.String())
	}
}

func TestRunInspectEncryptedWithoutKeyErrors(t *testing.T) {
	enc := filepath.Join(t.TempDir(), "b.CODEXBUNDLE.AGE")
	if err := os.WriteFile(enc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"inspect", enc}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "encrypted bundle") {
		t.Errorf("missing encrypted-bundle guidance: %s", errOut.String())
	}
}

// TestRunExportImportEncryptedRoundTrip exercises the full encrypt-on-export /
// decrypt-on-import path. It is skipped when age/age-keygen are unavailable.
func TestRunExportImportEncryptedRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed; skipping encrypted round-trip")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed; skipping encrypted round-trip")
	}

	src := t.TempDir()
	writeSession(t, src, "ffff1111-2222-3333-4444-555566667777", "/Users/example/dev/project")

	work := t.TempDir()
	keyFile := filepath.Join(work, "key.txt")
	if out, err := exec.Command("age-keygen", "-o", keyFile).CombinedOutput(); err != nil {
		t.Fatalf("age-keygen: %v: %s", err, out)
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "# public key: "
	var recipient string
	for _, line := range strings.Split(string(keyData), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, marker) {
			recipient = strings.TrimPrefix(line, marker)
		}
	}
	if recipient == "" {
		t.Fatal("no recipient in key file")
	}

	bundle := filepath.Join(work, "out.codexbundle")
	var out, errOut bytes.Buffer
	code := Run([]string{"export", "--all", "--codex-home", src,
		"--encrypt-to", recipient, "-o", bundle}, &out, &errOut)
	if code != 0 {
		t.Fatalf("export exit=%d stderr=%s", code, errOut.String())
	}
	enc := bundle + ".age"
	if _, err := os.Stat(enc); err != nil {
		t.Fatalf("encrypted bundle missing: %v", err)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("plaintext bundle was left behind: %v", err)
	}

	dst := t.TempDir()
	out.Reset()
	errOut.Reset()
	code = Run([]string{"import", enc, "--codex-home", dst, "--identity", keyFile}, &out, &errOut)
	if code != 0 {
		t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "New sessions: 1") {
		t.Errorf("expected 1 imported session: %s", out.String())
	}
}
