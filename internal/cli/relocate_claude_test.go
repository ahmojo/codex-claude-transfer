package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// claudeTranscriptPath is where a transcript for cwd/id lives inside a Claude home.
func claudeTranscriptPath(home, cwd, id string) string {
	return filepath.Join(home, claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd), id+claudehome.SessionExt)
}

// claudeTranscriptCWD reads the cwd recorded on a transcript's first line.
func claudeTranscriptCWD(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	line, _, _ := bytes.Cut(data, []byte("\n"))
	var record struct {
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	return record.CWD
}

// claudeRelocateFixture builds a Claude home holding one transcript recorded
// under oldPath, plus an existing destination directory.
func claudeRelocateFixture(t *testing.T) (home, oldPath, newPath, id string) {
	t.Helper()
	tmp := t.TempDir()
	home = filepath.Join(tmp, "claude-home")
	oldPath = filepath.Join(tmp, "old-project")
	newPath = filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id = "aaaa1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, oldPath, id, "hello claude")
	return home, oldPath, newPath, id
}

func TestRunRelocateClaudeDryRun(t *testing.T) {
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	source := claudeTranscriptPath(home, oldPath, id)
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read transcript before dry-run: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath,
		"--dry-run", "--json", "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate --dry-run exit=%d stderr=%s", code, errOut.String())
	}

	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read transcript after dry-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run changed the transcript")
	}
	if _, err := os.Stat(claudeTranscriptPath(home, newPath, id)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote into the new project folder: %v", err)
	}
	if backups, _ := filepath.Glob(source + ".cct-bak-*"); len(backups) != 0 {
		t.Fatalf("dry-run created backups: %v", backups)
	}

	var result relocateJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode relocate JSON: %v\n%s", err, out.String())
	}
	if result.Tool != "claude" || result.Sessions != 1 || result.Remapped != 1 || result.MovedFiles != 1 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if !result.DryRun || result.RemovedSources != 0 {
		t.Fatalf("unexpected dry-run state: %+v", result)
	}
}

// TestRunRelocateClaudeMovesTranscriptAndUndoRestoresIt is the end-to-end
// session-only workflow: the transcript is rewritten under the new encoded
// folder, the original is removed (no duplicate session id), and undo reverses
// both halves.
func TestRunRelocateClaudeMovesTranscriptAndUndoRestoresIt(t *testing.T) {
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(t.TempDir(), "cct-config"))
	source := claudeTranscriptPath(home, oldPath, id)
	dest := claudeTranscriptPath(home, newPath, id)
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read original transcript: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	if got := claudeTranscriptCWD(t, dest); got != newPath {
		t.Fatalf("relocated cwd=%q want %q", got, newPath)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("the original transcript was left behind (duplicate session id): %v", err)
	}
	backups, _ := filepath.Glob(source + ".cct-bak-*")
	if len(backups) != 1 {
		t.Fatalf("source backup count=%d want 1 (%v)", len(backups), backups)
	}
	if !strings.Contains(out.String(), "Relocation complete") {
		t.Fatalf("missing completion output:\n%s", out.String())
	}
	// ~/.claude.json is never created or touched.
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatalf("relocate touched the Claude config: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--claude-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	restored, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("undo did not restore the original transcript: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatalf("undo restored different bytes:\n got %q\nwant %q", restored, original)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("undo left the relocated copy in place: %v", err)
	}
}

// TestRunRelocateClaudeMoveProject also renames the project directory itself.
func TestRunRelocateClaudeMoveProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "claude-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "bbbb1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, oldPath, id, "move me")

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--move-project", "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate --move-project exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("project was not moved: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old project directory remains: %v", err)
	}
	if got := claudeTranscriptCWD(t, claudeTranscriptPath(home, newPath, id)); got != newPath {
		t.Fatalf("relocated cwd=%q want %q", got, newPath)
	}
	if !strings.Contains(out.String(), "Project directory: moved") {
		t.Fatalf("missing project move output:\n%s", out.String())
	}
}

// TestRunRelocateClaudeUsesCustomClaudeHome confirms the command reads and writes
// only the home it was pointed at.
func TestRunRelocateClaudeUsesCustomClaudeHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	custom := filepath.Join(tmp, "custom-claude")
	other := filepath.Join(tmp, "default-claude")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "cccc1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, custom, oldPath, id, "custom home")
	// A same-named transcript in a different home must not be touched.
	writeClaudeTranscript(t, other, oldPath, id, "custom home")
	t.Setenv("CLAUDE_HOME", other)

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", custom,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	if got := claudeTranscriptCWD(t, claudeTranscriptPath(custom, newPath, id)); got != newPath {
		t.Fatalf("relocated cwd=%q want %q", got, newPath)
	}
	if _, err := os.Stat(claudeTranscriptPath(other, oldPath, id)); err != nil {
		t.Fatalf("the $CLAUDE_HOME transcript was modified: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, claudehome.ProjectsSubdir, claudehome.EncodeCWD(newPath))); !os.IsNotExist(err) {
		t.Fatalf("relocate wrote into the wrong Claude home: %v", err)
	}
}

// TestRunRelocateClaudeEncodedFolderNames checks the destination folder for both
// a POSIX-style and a Windows-style project path, since the encoding is what
// decides where a transcript lands.
func TestRunRelocateClaudeEncodedFolderNames(t *testing.T) {
	cases := []struct {
		name    string
		newPath string
		want    string
	}{
		{"posix", "/srv/apps/web_api", "-srv-apps-web-api"},
		{"windows", `D:\work\web_api`, "D--work-web-api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := claudehome.EncodeCWD(c.newPath); got != c.want {
				t.Fatalf("EncodeCWD(%q) = %q, want %q", c.newPath, got, c.want)
			}
		})
	}

	// End-to-end for the host's own path shape: the transcript must appear under
	// exactly the folder the encoder names.
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(t.TempDir(), "cct-config"))
	var out, errOut bytes.Buffer
	if code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut); code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	wantDir := filepath.Join(home, claudehome.ProjectsSubdir, claudehome.EncodeCWD(newPath))
	entries, err := os.ReadDir(wantDir)
	if err != nil {
		t.Fatalf("read encoded destination folder: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != id+claudehome.SessionExt {
		t.Fatalf("unexpected destination folder contents: %v", entries)
	}
}

// TestRunRelocateClaudeRefusesDuplicateSessionID: a transcript with the same id
// already sits in the destination folder, so relocation stops before writing.
func TestRunRelocateClaudeRefusesDuplicateSessionID(t *testing.T) {
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	writeClaudeTranscript(t, home, newPath, id, "a different conversation")
	source := claudeTranscriptPath(home, oldPath, id)
	dest := claudeTranscriptPath(home, newPath, id)
	sourceBefore, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	destBefore, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "so a session id is never duplicated") {
		t.Fatalf("duplicate id exit=%d stderr=%s", code, errOut.String())
	}
	sourceAfter, _ := os.ReadFile(source)
	destAfter, _ := os.ReadFile(dest)
	if !bytes.Equal(sourceBefore, sourceAfter) || !bytes.Equal(destBefore, destAfter) {
		t.Fatal("a refused relocation changed files")
	}
}

// TestRunRelocateClaudeRejectsIncludeArchived documents the flag's defined
// behavior for Claude Code: there is no separate archive location, so the flag is
// refused instead of silently doing nothing.
func TestRunRelocateClaudeRejectsIncludeArchived(t *testing.T) {
	home, oldPath, newPath, _ := claudeRelocateFixture(t)
	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--include-archived", "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "--include-archived does not apply to Claude Code") {
		t.Fatalf("include-archived exit=%d stderr=%s", code, errOut.String())
	}
}

// TestRunRelocateClaudeRejectsOverlapWithHome keeps the project directory and the
// Claude home from being tangled by a --move-project rename.
func TestRunRelocateClaudeRejectsOverlapWithHome(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "claude-home")
	oldPath := filepath.Join(home, "projects")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--move-project", "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "must not overlap the Claude Code home") {
		t.Fatalf("overlap exit=%d stderr=%s", code, errOut.String())
	}
}

// TestRunRelocateClaudeRollsBackWhenSourceRemovalFails is the partial-failure
// case: the first original is removed, the second removal fails, and the command
// must restore the removed original, delete the copies it wrote, and move the
// project directory back.
func TestRunRelocateClaudeRollsBackWhenSourceRemovalFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "claude-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	firstID := "dddd1111-2222-3333-4444-555566667777"
	secondID := "eeee1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, oldPath, firstID, "first")
	writeClaudeTranscript(t, home, oldPath, secondID, "second")
	sources := map[string][]byte{}
	for _, id := range []string{firstID, secondID} {
		data, err := os.ReadFile(claudeTranscriptPath(home, oldPath, id))
		if err != nil {
			t.Fatalf("read source %s: %v", id, err)
		}
		sources[id] = data
	}

	removals := 0
	ops := defaultRelocateOps
	ops.removeSource = func(path string) error {
		removals++
		if removals > 1 {
			return errors.New("injected removal failure")
		}
		return os.Remove(path)
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project", "--tool", "claude", "--claude-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "injected removal failure") {
		t.Fatalf("removal failure exit=%d stderr=%s", code, errOut.String())
	}

	// Both originals are back, byte-for-byte, with no backups left over.
	for id, want := range sources {
		path := claudeTranscriptPath(home, oldPath, id)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("original %s was not restored: %v", id, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("restored %s differs from the original", id)
		}
		if backups, _ := filepath.Glob(path + ".cct-bak-*"); len(backups) != 0 {
			t.Fatalf("rollback left backups for %s: %v", id, backups)
		}
	}
	// No relocated copies remain.
	for _, id := range []string{firstID, secondID} {
		if _, err := os.Stat(claudeTranscriptPath(home, newPath, id)); !os.IsNotExist(err) {
			t.Fatalf("relocated copy of %s remains after rollback: %v", id, err)
		}
	}
	// And the project directory is back where it was.
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("project was not moved back: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("moved project remains after rollback: %v", err)
	}
}

// TestRunRelocateClaudeRollsBackIncompleteImport covers a destination write that
// silently does less than the preflight promised.
func TestRunRelocateClaudeRollsBackIncompleteImport(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "claude-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "ffff1111-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, oldPath, id, "incomplete")
	source := claudeTranscriptPath(home, oldPath, id)
	dest := claudeTranscriptPath(home, newPath, id)

	ops := defaultRelocateOps
	ops.importBundle = func(target codexhome.Home, opts bundle.ImportOptions) (bundle.ImportResult, error) {
		if opts.DryRun {
			return bundle.Import(target, opts)
		}
		// Write the copy but report nothing as imported.
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return bundle.ImportResult{}, err
		}
		if err := os.WriteFile(dest, []byte("{\"cwd\":\"x\"}\n"), 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		return bundle.ImportResult{Items: []bundle.ImportItem{{
			DestPath: dest, Action: bundle.ActionImport,
		}}}, nil
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "real import was incomplete") {
		t.Fatalf("incomplete import exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("the original transcript was lost: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the half-written copy remains after rollback: %v", err)
	}
}

// TestRunRelocateClaudeSameEncodedFolderRewritesInPlace covers Claude's lossy
// folder encoding: when OLD and NEW encode to the same folder name, the
// transcripts stay put and only their cwd fields are rewritten, so there is no
// original to remove.
func TestRunRelocateClaudeSameEncodedFolderRewritesInPlace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "claude-home")
	// "a_b" and "a-b" both encode to "...a-b".
	oldPath := filepath.Join(tmp, "a_b")
	newPath := filepath.Join(tmp, "a-b")
	if claudehome.EncodeCWD(oldPath) != claudehome.EncodeCWD(newPath) {
		t.Skip("this platform's paths do not produce an encoding collision")
	}
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "9999aaaa-2222-3333-4444-555566667777"
	writeClaudeTranscript(t, home, oldPath, id, "same folder")
	path := claudeTranscriptPath(home, oldPath, id)

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("in-place relocate exit=%d stderr=%s", code, errOut.String())
	}
	if got := claudeTranscriptCWD(t, path); got != newPath {
		t.Fatalf("in-place cwd=%q want %q", got, newPath)
	}
	if backups, _ := filepath.Glob(path + ".cct-bak-*"); len(backups) != 1 {
		t.Fatalf("in-place backup count=%d want 1 (%v)", len(backups), backups)
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--claude-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	if got := claudeTranscriptCWD(t, path); got != oldPath {
		t.Fatalf("undo restored cwd=%q want %q", got, oldPath)
	}
}
