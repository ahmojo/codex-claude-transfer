package cli

import (
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

// TestSkillFlow runs the exact command sequence the skill documents — export
// into .cct/, then restore into another machine's home from another path — so
// the instructions cannot silently drift away from the CLI.
func TestSkillFlow(t *testing.T) {
	tmp := t.TempDir()
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
