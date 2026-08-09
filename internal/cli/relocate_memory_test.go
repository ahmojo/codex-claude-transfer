package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
)

// writeProjectMemory puts a file into a project's auto-memory directory
// (projects/<encoded-cwd>/memory/<rel>), the way Claude Code does.
func writeProjectMemory(t *testing.T, home, cwd, rel, content string) string {
	t.Helper()
	path := filepath.Join(home, claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd), claudehome.MemorySubdir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}
	return path
}

func memoryPath(home, cwd, rel string) string {
	return filepath.Join(home, claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd), claudehome.MemorySubdir, filepath.FromSlash(rel))
}

// A project's memory is keyed by the same encoded path as its transcripts, so a
// relocation that leaves it behind silently strips the project of everything the
// agent had learned about it. This is the regression test for that.
func TestRunRelocateClaudeMovesProjectMemory(t *testing.T) {
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(t.TempDir(), "cct-config"))
	writeProjectMemory(t, home, oldPath, "MEMORY.md", "- [core](core.md) — durable facts\n")
	writeProjectMemory(t, home, oldPath, "notes/core.md", "the build needs Go 1.23\n")

	var out, errOut bytes.Buffer
	if code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut); code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}

	for rel, want := range map[string]string{
		"MEMORY.md":     "- [core](core.md) — durable facts\n",
		"notes/core.md": "the build needs Go 1.23\n",
	} {
		got, err := os.ReadFile(memoryPath(home, newPath, rel))
		if err != nil {
			t.Fatalf("memory file %s did not move with the project: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("memory file %s changed:\n got %q\nwant %q", rel, got, want)
		}
		if _, err := os.Stat(memoryPath(home, oldPath, rel)); !os.IsNotExist(err) {
			t.Errorf("memory file %s was left behind under the old project (err=%v)", rel, err)
		}
	}
	if !strings.Contains(out.String(), "Auto-memory files moved with the project: 2") {
		t.Errorf("the summary does not mention the memory files:\n%s", out.String())
	}

	// Undo puts the memory back where it was and removes the copies.
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--claude-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.ReadFile(memoryPath(home, oldPath, "MEMORY.md")); err != nil {
		t.Fatalf("undo did not restore the memory file: %v", err)
	}
	if _, err := os.Stat(memoryPath(home, newPath, "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("undo left the relocated memory copy in place: %v", err)
	}
	// And the transcript half still works.
	if _, err := os.Stat(claudeTranscriptPath(home, oldPath, id)); err != nil {
		t.Fatalf("undo did not restore the transcript: %v", err)
	}
}

func TestRunRelocateClaudeDryRunLeavesMemoryAlone(t *testing.T) {
	home, oldPath, newPath, _ := claudeRelocateFixture(t)
	writeProjectMemory(t, home, oldPath, "MEMORY.md", "remember this\n")

	var out, errOut bytes.Buffer
	if code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home, "--dry-run", "--json",
	}, &out, &errOut); code != 0 {
		t.Fatalf("dry run exit=%d stderr=%s", code, errOut.String())
	}
	var got struct {
		MemoryFiles int `json:"memory_files"`
		DryRun      bool
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("relocate --json is not JSON: %v\n%s", err, out.String())
	}
	if got.MemoryFiles != 1 {
		t.Errorf("memory_files=%d, want 1", got.MemoryFiles)
	}
	if _, err := os.Stat(memoryPath(home, oldPath, "MEMORY.md")); err != nil {
		t.Errorf("dry run moved the memory file: %v", err)
	}
	if _, err := os.Stat(memoryPath(home, newPath, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote memory under the new project (err=%v)", err)
	}
}

// Overwriting memory would destroy what the agent learned on the other side, so
// a destination file with different content stops the whole relocation — before
// any transcript is touched.
func TestRunRelocateClaudeRefusesConflictingMemory(t *testing.T) {
	home, oldPath, newPath, id := claudeRelocateFixture(t)
	writeProjectMemory(t, home, oldPath, "MEMORY.md", "learned on the old path\n")
	writeProjectMemory(t, home, newPath, "MEMORY.md", "learned on the new path\n")

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("relocate overwrote conflicting memory:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "already has a different") {
		t.Fatalf("stderr does not explain the refusal: %q", errOut.String())
	}
	if got, _ := os.ReadFile(memoryPath(home, newPath, "MEMORY.md")); string(got) != "learned on the new path\n" {
		t.Errorf("the destination memory file was modified: %q", got)
	}
	if _, err := os.Stat(claudeTranscriptPath(home, oldPath, id)); err != nil {
		t.Errorf("the transcript was moved despite the refusal: %v", err)
	}
}

// The same file on both sides is not a conflict: it is not written again, but
// the original still moves out of the old project folder.
func TestRunRelocateClaudeAcceptsIdenticalMemory(t *testing.T) {
	home, oldPath, newPath, _ := claudeRelocateFixture(t)
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(t.TempDir(), "cct-config"))
	writeProjectMemory(t, home, oldPath, "MEMORY.md", "same on both sides\n")
	writeProjectMemory(t, home, newPath, "MEMORY.md", "same on both sides\n")

	var out, errOut bytes.Buffer
	if code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut); code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	if got, _ := os.ReadFile(memoryPath(home, newPath, "MEMORY.md")); string(got) != "same on both sides\n" {
		t.Errorf("the destination memory file changed: %q", got)
	}
	if _, err := os.Stat(memoryPath(home, oldPath, "MEMORY.md")); !os.IsNotExist(err) {
		t.Errorf("the original memory file was left behind (err=%v)", err)
	}
}

// A project with no memory directory is the common case and must stay silent.
func TestRunRelocateClaudeWithoutMemory(t *testing.T) {
	home, oldPath, newPath, _ := claudeRelocateFixture(t)
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(t.TempDir(), "cct-config"))

	var out, errOut bytes.Buffer
	if code := Run([]string{
		"relocate", oldPath, newPath, "--tool", "claude", "--claude-home", home,
	}, &out, &errOut); code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(out.String(), "Auto-memory") {
		t.Errorf("a project without memory should not mention it:\n%s", out.String())
	}
}

// The in-place case (both paths encode to the same folder) has nothing to move.
func TestPlanClaudeMemoryMoveSkipsInPlace(t *testing.T) {
	tmp := t.TempDir()
	home := claudehome.Home{Root: tmp, ProjectsDir: filepath.Join(tmp, claudehome.ProjectsSubdir)}
	old := filepath.Join(tmp, "proj_one")
	newer := filepath.Join(tmp, "proj-one") // '_' and '-' both encode to '-'
	writeProjectMemory(t, tmp, old, "MEMORY.md", "x\n")
	files, err := planClaudeMemoryMove(home, old, newer, true)
	if err != nil || len(files) != 0 {
		t.Fatalf("in-place plan = %v (err=%v), want none", files, err)
	}
}
