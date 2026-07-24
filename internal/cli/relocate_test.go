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
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

func TestRunRelocateDryRun(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "aaaa1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session before dry-run: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath,
		"--dry-run", "--json", "--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate --dry-run exit=%d stderr=%s", code, errOut.String())
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session after dry-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run changed the session")
	}
	if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 0 {
		t.Fatalf("dry-run created backups: %v", backups)
	}

	var result relocateJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode relocate JSON: %v\n%s", err, out.String())
	}
	if result.Sessions != 1 || result.Remapped != 1 || result.Replaced != 1 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if !result.DryRun || result.ProjectAction != "not-requested" {
		t.Fatalf("unexpected dry-run state: %+v", result)
	}
}

func TestRunRelocateRewritesSessionsAndUndoRestoresThem(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "bbbb1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath,
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate exit=%d stderr=%s", code, errOut.String())
	}
	if got := relocateTestSessionCWD(t, sessionPath); got != newPath {
		t.Fatalf("relocated cwd=%q want %q", got, newPath)
	}
	if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 1 {
		t.Fatalf("backup count=%d want 1 (%v)", len(backups), backups)
	}
	if !strings.Contains(out.String(), "Relocation complete") {
		t.Fatalf("missing completion output:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("undo exit=%d stderr=%s", code, errOut.String())
	}
	if got := relocateTestSessionCWD(t, sessionPath); got != oldPath {
		t.Fatalf("undo restored cwd=%q want %q", got, oldPath)
	}
}

func TestRunRelocateIncludesArchivedSessionsAndUndoRestoresThem(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	activeID := "bcaa1111-2222-3333-4444-555566667777"
	archivedID := "bcbb1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, activeID, oldPath)
	writeRelocateArchivedSessionCWD(t, home, archivedID, oldPath)
	activePath := relocateTestSessionPath(home, activeID)
	archivedPath := relocateTestArchivedSessionPath(home, archivedID)

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--include-archived",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate archived exit=%d stderr=%s", code, errOut.String())
	}
	for _, sessionPath := range []string{activePath, archivedPath} {
		if got := relocateTestSessionCWD(t, sessionPath); got != newPath {
			t.Fatalf("relocated cwd for %s=%q want %q", sessionPath, got, newPath)
		}
		if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 1 {
			t.Fatalf("backup count for %s=%d want 1 (%v)", sessionPath, len(backups), backups)
		}
	}
	if !strings.Contains(out.String(), "Sessions: 2") {
		t.Fatalf("archived relocation count missing:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run([]string{"undo", "--codex-home", home}, &out, &errOut); code != 0 {
		t.Fatalf("undo archived relocation exit=%d stderr=%s", code, errOut.String())
	}
	for _, sessionPath := range []string{activePath, archivedPath} {
		if got := relocateTestSessionCWD(t, sessionPath); got != oldPath {
			t.Fatalf("undo restored cwd for %s=%q want %q", sessionPath, got, oldPath)
		}
	}
}

func TestRunRelocateRejectsUnknownCompressedSessions(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(newPath, 0o755); err != nil {
		t.Fatalf("create new project: %v", err)
	}
	id := "bccc1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session before rejection: %v", err)
	}
	compressedDir := filepath.Dir(sessionPath)
	compressedPath := filepath.Join(compressedDir,
		"rollout-2026-06-13T19-22-01-bcdd1111-2222-3333-4444-555566667777.jsonl.zst")
	if err := os.WriteFile(compressedPath, []byte("not a zstd frame"), 0o644); err != nil {
		t.Fatalf("write unknown compressed session: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath,
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "compressed session(s) have unknown cwd") {
		t.Fatalf("unknown-compressed exit=%d stderr=%s", code, errOut.String())
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session after rejection: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unknown-compressed rejection changed the plain session")
	}
	if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 0 {
		t.Fatalf("unknown-compressed rejection created backups: %v", backups)
	}
}

func TestRunRelocateMovesProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCT_CONFIG_DIR", filepath.Join(tmp, "cct-config"))
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "marker.txt"), []byte("project data"), 0o644); err != nil {
		t.Fatalf("write project marker: %v", err)
	}
	id := "cccc1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate --move-project exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old project still exists or stat failed unexpectedly: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(newPath, "marker.txt")); err != nil || string(data) != "project data" {
		t.Fatalf("moved project marker data=%q err=%v", data, err)
	}
	if got := relocateTestSessionCWD(t, relocateTestSessionPath(home, id)); got != newPath {
		t.Fatalf("relocated cwd=%q want %q", got, newPath)
	}
}

func TestRunRelocateMoveProjectDryRunAllowsMissingTarget(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "cddd1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session before dry-run: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--move-project", "--dry-run", "--json",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("relocate move dry-run exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("dry-run moved or removed OLD: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run created NEW or stat failed: %v", err)
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session after dry-run: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("move dry-run changed the session")
	}

	var result relocateJSON
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode relocate JSON: %v\n%s", err, out.String())
	}
	if result.ProjectAction != "would-move" || !result.DryRun {
		t.Fatalf("unexpected move dry-run result: %+v", result)
	}
}

func TestRunRelocateRejectsMissingTargetWithoutMove(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "missing-project")
	id := "ceee1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session before rejection: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath,
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "NEW must already exist") {
		t.Fatalf("missing-target exit=%d stderr=%s", code, errOut.String())
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session after rejection: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("missing-target rejection changed the session")
	}
}

func TestRunRelocateRenameFailurePreservesProjectAndSession(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "cfff1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session before rename failure: %v", err)
	}

	ops := defaultRelocateOps
	ops.rename = func(string, string) error {
		return errors.New("injected rename failure")
	}
	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "injected rename failure") {
		t.Fatalf("rename-failure exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("rename failure removed OLD: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("rename failure created NEW or stat failed: %v", err)
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session after rename failure: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rename failure changed the session")
	}
	if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 0 {
		t.Fatalf("rename failure created backups: %v", backups)
	}
}

func TestRunRelocateRollsBackSessionsAndProject(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "dddd1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	original, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read original session: %v", err)
	}

	ops := defaultRelocateOps
	ops.importBundle = func(target codexhome.Home, opts bundle.ImportOptions) (bundle.ImportResult, error) {
		if opts.DryRun {
			return bundle.Import(target, opts)
		}
		backup := sessionPath + ".cct-bak-test"
		if err := os.WriteFile(backup, original, 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		mapped := bytes.ReplaceAll(original, []byte(oldPath), []byte(newPath))
		if err := os.WriteFile(sessionPath, mapped, 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		return bundle.ImportResult{Items: []bundle.ImportItem{{
			DestPath:   sessionPath,
			BackupPath: backup,
			Action:     bundle.ActionReplace,
		}}}, errors.New("injected import failure")
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut, ops)
	if code != 1 {
		t.Fatalf("relocate failure exit=%d want 1", code)
	}
	if !strings.Contains(errOut.String(), "injected import failure") {
		t.Fatalf("missing injected error:\n%s", errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old project was not restored: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new project remains after rollback or stat failed: %v", err)
	}
	restored, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read rolled-back session: %v", err)
	}
	if !bytes.Equal(restored, original) {
		t.Fatal("session content was not rolled back")
	}
	if _, err := os.Stat(sessionPath + ".cct-bak-test"); !os.IsNotExist(err) {
		t.Fatalf("rollback backup remains or stat failed: %v", err)
	}
}

func TestRunRelocateRollsBackIncompleteRealImport(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	firstID := "ddaa1111-2222-3333-4444-555566667777"
	secondID := "ddbb1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, firstID, oldPath)
	writeSessionCWD(t, home, secondID, oldPath)
	firstPath := relocateTestSessionPath(home, firstID)
	secondPath := relocateTestSessionPath(home, secondID)
	firstOriginal, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read first original session: %v", err)
	}

	ops := defaultRelocateOps
	ops.importBundle = func(target codexhome.Home, opts bundle.ImportOptions) (bundle.ImportResult, error) {
		if opts.DryRun {
			return bundle.Import(target, opts)
		}
		backup := firstPath + ".cct-bak-test"
		if err := os.WriteFile(backup, firstOriginal, 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		mapped := bytes.ReplaceAll(firstOriginal, []byte(oldPath), []byte(newPath))
		if err := os.WriteFile(firstPath, mapped, 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		return bundle.ImportResult{
			Mapped:                  1,
			Replaced:                1,
			MappedCompressedSkipped: 1,
			Items: []bundle.ImportItem{{
				DestPath:   firstPath,
				BackupPath: backup,
				Action:     bundle.ActionReplace,
			}},
		}, nil
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "real import was incomplete") {
		t.Fatalf("incomplete-import exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old project was not restored: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new project remains after rollback or stat failed: %v", err)
	}
	for _, sessionPath := range []string{firstPath, secondPath} {
		if got := relocateTestSessionCWD(t, sessionPath); got != oldPath {
			t.Fatalf("rolled-back cwd for %s=%q want %q", sessionPath, got, oldPath)
		}
		if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 0 {
			t.Fatalf("rollback backup remains for %s: %v", sessionPath, backups)
		}
	}
}

func TestRunRelocateRemovesUnexpectedCreatedSessionOnRollback(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "ddcc1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)
	original, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read original session: %v", err)
	}

	ops := defaultRelocateOps
	ops.importBundle = func(target codexhome.Home, opts bundle.ImportOptions) (bundle.ImportResult, error) {
		if opts.DryRun {
			return bundle.Import(target, opts)
		}
		if err := os.Remove(sessionPath); err != nil {
			return bundle.ImportResult{}, err
		}
		mapped := bytes.ReplaceAll(original, []byte(oldPath), []byte(newPath))
		if err := os.WriteFile(sessionPath, mapped, 0o600); err != nil {
			return bundle.ImportResult{}, err
		}
		return bundle.ImportResult{
			Mapped:   1,
			Imported: 1,
			Items: []bundle.ImportItem{{
				DestPath: sessionPath,
				Action:   bundle.ActionImport,
			}},
		}, nil
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "real import was incomplete") {
		t.Fatalf("unexpected-created exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old project was not restored: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new project remains after rollback or stat failed: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("unexpected created session remains after rollback or stat failed: %v", err)
	}
}

func TestRunRelocateStopsIfSessionChangesDuringProjectMove(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "codex-home")
	oldPath := filepath.Join(tmp, "old-project")
	newPath := filepath.Join(tmp, "new-project")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	id := "eeee1111-2222-3333-4444-555566667777"
	writeSessionCWD(t, home, id, oldPath)
	sessionPath := relocateTestSessionPath(home, id)

	ops := defaultRelocateOps
	ops.rename = func(oldName, newName string) error {
		if err := os.Rename(oldName, newName); err != nil {
			return err
		}
		if oldName == oldPath {
			file, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			if _, err := file.WriteString("{\"type\":\"event_msg\"}\n"); err != nil {
				file.Close()
				return err
			}
			return file.Close()
		}
		return nil
	}

	var out, errOut bytes.Buffer
	code := runRelocateWithOps([]string{
		oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut, ops)
	if code != 1 || !strings.Contains(errOut.String(), "session changed") {
		t.Fatalf("changed-session exit=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old project was not restored: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("new project remains after rollback or stat failed: %v", err)
	}
	if backups, _ := filepath.Glob(sessionPath + ".cct-bak-*"); len(backups) != 0 {
		t.Fatalf("change detection created backups: %v", backups)
	}
}

func TestRunRelocateRejectsNestedPathsAndClaude(t *testing.T) {
	tmp := t.TempDir()
	oldPath := filepath.Join(tmp, "project")
	newPath := filepath.Join(oldPath, "nested")
	home := filepath.Join(tmp, "codex-home")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("create old project: %v", err)
	}

	var out, errOut bytes.Buffer
	code := Run([]string{
		"relocate", oldPath, newPath, "--move-project",
		"--tool", "codex", "--codex-home", home,
	}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "must not contain") {
		t.Fatalf("nested paths exit=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Run([]string{"relocate", oldPath, tmp, "--tool", "claude"}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "supports Codex only") {
		t.Fatalf("Claude relocate exit=%d stderr=%s", code, errOut.String())
	}
}

func relocateTestSessionPath(home, id string) string {
	return filepath.Join(home, "sessions", "2026", "06", "13",
		"rollout-2026-06-13T18-22-01-"+id+".jsonl")
}

func relocateTestArchivedSessionPath(home, id string) string {
	return filepath.Join(home, "archived_sessions",
		"rollout-2026-06-13T18-22-01-"+id+".jsonl")
}

func writeRelocateArchivedSessionCWD(t *testing.T, home, threadID, cwd string) {
	t.Helper()
	path := relocateTestArchivedSessionPath(home, threadID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create archived session directory: %v", err)
	}
	cwdJSON, err := json.Marshal(cwd)
	if err != nil {
		t.Fatalf("encode archived cwd: %v", err)
	}
	body := `{"timestamp":"x","type":"session_meta","payload":{"id":"` + threadID + `","cwd":` + string(cwdJSON) + `,"source":"cli"}}` + "\n" +
		`{"timestamp":"y","type":"event_msg","payload":{"type":"user_message","message":"archived"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write archived session: %v", err)
	}
}

func relocateTestSessionCWD(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	line, _, _ := bytes.Cut(data, []byte("\n"))
	var record struct {
		Payload struct {
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("decode session metadata: %v", err)
	}
	return record.Payload.CWD
}
