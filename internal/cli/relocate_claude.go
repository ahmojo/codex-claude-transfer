package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/cctpaths"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/undo"
)

// Claude Code records a project in two places: the per-line "cwd" field inside a
// transcript and the projects/<encoded-cwd>/ folder holding it. Relocating a
// project therefore usually moves each transcript to a different folder, which
// the plain import path alone would not finish: it writes the remapped copy but
// leaves the original behind, duplicating the session id.
//
// The workflow here closes that gap without adding a second session writer:
//
//  1. export the transcripts recorded under OLD into a private temporary bundle;
//  2. preflight the mapped import in dry-run mode and refuse anything that is not
//     a clean, complete set of new files (a taken destination means a duplicate
//     session id, so nothing is written);
//  3. run the real import, which writes every remapped transcript under the new
//     encoded folder;
//  4. only then back up and delete each original, and record both halves in one
//     undo journal so `cct undo` restores the originals and removes the copies.
//
// Any failure after step 3 restores the removed originals, deletes the copies the
// import created, and rolls a --move-project rename back. ~/.claude.json is never
// read or written; Claude Code rediscovers the transcripts on its next run.

// relocatedSource is one original transcript that was backed up and then removed
// after its relocated copy had been written.
type relocatedSource struct {
	// Path is the original transcript's absolute path under the old encoded folder.
	Path string
	// Backup holds its bytes, and BackupSHA the hash undo verifies before restoring.
	Backup    string
	BackupSHA string
}

// runRelocateClaude performs the Claude Code workflow described above. Paths are
// already resolved and validated by runRelocateWithOps.
func runRelocateClaude(f relocateFlags, claudeHome claudehome.Home, home codexhome.Home, oldPath, newPath string, stdout, stderr io.Writer, ops relocateOps) int {
	tmpDir, err := os.MkdirTemp("", "cct-relocate-")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot create private temporary directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)
	bundlePath := filepath.Join(tmpDir, "sessions.codexbundle")

	exported, err := ops.export(home, bundle.ExportOptions{
		Tool:        agent.Claude,
		ClaudeHome:  claudeHome,
		ProjectPath: oldPath,
		OutputPath:  bundlePath,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot export transcripts for %s: %v\n", oldPath, err)
		return 1
	}

	// Claude's folder encoding is lossy, so two different project paths can share
	// one folder. When they do, the transcripts stay where they are and only their
	// cwd fields change — the same in-place rewrite Codex uses, with no originals
	// to remove.
	inPlace := claudehome.EncodeCWD(oldPath) == claudehome.EncodeCWD(newPath)

	importOpts := bundle.ImportOptions{
		BundlePath: bundlePath,
		DryRun:     true,
		MapCWD:     []bundle.CWDMapping{{Old: oldPath, New: newPath}},
		// Only the in-place case rewrites an existing file. When transcripts move
		// folders every destination must be free, so conflicts stay conflicts and
		// the preflight below refuses them instead of overwriting anything.
		ReplaceWithBackup: inPlace,
	}
	preview, err := ops.importBundle(home, importOpts)
	if err != nil {
		fmt.Fprintf(stderr, "error: relocate preflight failed: %v\n", err)
		return 1
	}
	if err := validateClaudeRelocatePlan(exported, preview, inPlace); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := verifyRelocateSources(exported.Manifest); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if f.dryRun {
		printRelocateResult(stdout, f, relocateOutcome{
			kind: agent.Claude, oldPath: oldPath, newPath: newPath, result: preview,
			movedFiles: claudeMovedCount(preview, inPlace),
		})
		return 0
	}

	projectMoved := false
	if f.moveProject {
		if err := ops.rename(oldPath, newPath); err != nil {
			fmt.Fprintf(stderr, "error: cannot move project from %s to %s: %v (relocate supports same-filesystem renames only)\n", oldPath, newPath, err)
			return 1
		}
		projectMoved = true
	}
	// Re-check the sources: Claude may have appended to a transcript while the
	// project directory was being renamed.
	if err := verifyRelocateSources(exported.Manifest); err != nil {
		if projectMoved {
			if moveBackErr := ops.rename(newPath, oldPath); moveBackErr != nil {
				fmt.Fprintf(stderr, "error: %v; project rollback was incomplete: %v\n", err, moveBackErr)
				return 1
			}
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	importOpts.DryRun = false
	importOpts.RecordUndo = true
	result, importErr := ops.importBundle(home, importOpts)
	if importErr == nil {
		importErr = validateClaudeRelocateImportResult(exported, result, inPlace)
	}
	if importErr != nil {
		fmt.Fprintf(stderr, "error: relocate import failed: %v\n", importErr)
		reportRelocateRollback(stderr, rollbackRelocateImport(result), nil, rollbackProjectMove(ops, projectMoved, oldPath, newPath))
		return 1
	}

	// Every destination is written and verified, so the originals can go. A
	// failure here restores what was already removed and unwinds the import.
	var removed []relocatedSource
	if !inPlace {
		removed, err = removeRelocatedSources(exported.Manifest, result, claudeHome, ops)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			reportRelocateRollback(stderr, rollbackRelocateImport(result), restoreRelocatedSources(removed), rollbackProjectMove(ops, projectMoved, oldPath, newPath))
			return 1
		}
	}

	recordClaudeRelocateJournal(home, relocateLabel(oldPath, newPath), result, removed, stderr)
	printRelocateResult(stdout, f, relocateOutcome{
		kind: agent.Claude, oldPath: oldPath, newPath: newPath, result: result,
		backups:        append(backupPaths(result), sourceBackupPaths(removed)...),
		movedFiles:     claudeMovedCount(result, inPlace),
		removedSources: len(removed),
	})
	return 0
}

// claudeMovedCount reports how many transcripts land in a different project
// folder. In the in-place case (both paths share one encoded folder) none do.
func claudeMovedCount(result bundle.ImportResult, inPlace bool) int {
	if inPlace {
		return 0
	}
	return result.Imported
}

// validateClaudeRelocatePlan requires the preflight to describe a complete,
// unambiguous relocation. Anything else — a taken destination (a duplicate
// session id), a transcript whose cwd could not be rewritten, two transcripts
// claiming the same destination — stops the command before any write.
func validateClaudeRelocatePlan(exported bundle.ExportResult, preview bundle.ImportResult, inPlace bool) error {
	want := exported.IncludedCount
	if err := checkDistinctDestinations(preview); err != nil {
		return err
	}
	if inPlace {
		// OLD and NEW encode to the same folder, so each transcript is rewritten
		// where it already lives.
		if preview.Replaced != want || preview.Imported != 0 || preview.Conflicts != 0 || preview.SkippedOther != 0 {
			return fmt.Errorf("preflight expected %d in-place rewrites but found %d; no changes were made", want, preview.Replaced)
		}
	} else {
		// A destination that is already taken means the new project folder holds a
		// transcript with the same session id, whether its content differs
		// (conflict) or not (identical).
		if taken := preview.Conflicts + preview.SkippedIdentical; taken > 0 {
			return fmt.Errorf("%d transcript(s) already exist in the new project folder under the same session id; relocation stopped so a session id is never duplicated", taken)
		}
		if preview.Imported != want || preview.Replaced != 0 || preview.SkippedOther != 0 {
			return fmt.Errorf("preflight expected %d transcripts to move into the new project folder but found %d; no changes were made", want, preview.Imported)
		}
	}
	if preview.Mapped != want {
		return fmt.Errorf("preflight would remap %d of %d selected transcripts; no changes were made", preview.Mapped, want)
	}
	return nil
}

// validateClaudeRelocateImportResult applies the preflight invariant to the real
// import, because the on-disk state can change between the two calls.
func validateClaudeRelocateImportResult(exported bundle.ExportResult, result bundle.ImportResult, inPlace bool) error {
	if err := validateClaudeRelocatePlan(exported, result, inPlace); err != nil {
		return fmt.Errorf("real import was incomplete: %w", err)
	}
	return nil
}

// checkDistinctDestinations refuses a plan in which two bundle entries would be
// written to the same file. Claude names a transcript after its session id, so a
// collision means the bundle carries the same id twice (for example the same
// conversation stored under two encoded folders).
func checkDistinctDestinations(result bundle.ImportResult) error {
	seen := map[string]string{}
	for _, item := range result.Items {
		if item.DestPath == "" {
			continue
		}
		key := relocatePathKey(item.DestPath)
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("%s and %s would both be written to %s; relocation stopped so a session id is never duplicated", prev, item.BundlePath, item.DestPath)
		}
		seen[key] = item.BundlePath
	}
	return nil
}

// removeRelocatedSources backs up and deletes each original transcript after its
// relocated copy exists. Every removal is re-verified immediately beforehand: the
// file must still be an ordinary file with the exact bytes that were exported,
// must live inside the Claude projects directory, and must not be a path the
// import just wrote. It returns what it removed — including on failure, so the
// caller can restore it.
func removeRelocatedSources(manifest bundle.Manifest, result bundle.ImportResult, claudeHome claudehome.Home, ops relocateOps) ([]relocatedSource, error) {
	written := map[string]bool{}
	for _, item := range result.Items {
		if item.DestPath != "" {
			written[relocatePathKey(item.DestPath)] = true
		}
	}

	var removed []relocatedSource
	for _, session := range manifest.Sessions {
		src := absPath(session.OriginalPath)
		if !pathContains(claudeHome.ProjectsDir, src) {
			return removed, fmt.Errorf("refusing to remove %s: it is outside %s", src, claudeHome.ProjectsDir)
		}
		if written[relocatePathKey(src)] {
			return removed, fmt.Errorf("refusing to remove %s: the relocated transcript was written to the same path", src)
		}
		info, err := os.Lstat(src)
		if err != nil {
			return removed, fmt.Errorf("cannot inspect the original transcript %s: %w", src, err)
		}
		if !info.Mode().IsRegular() {
			return removed, fmt.Errorf("refusing to remove %s: it is no longer a regular file", src)
		}
		sum, ok := fileSHA(src)
		if !ok || sum != session.SHA256 {
			return removed, fmt.Errorf("the original transcript %s changed while it was being relocated; stop Claude Code and retry", src)
		}
		backup, err := bundle.BackupFile(src)
		if err != nil {
			return removed, fmt.Errorf("back up %s before removing it: %w", src, err)
		}
		bsum, ok := fileSHA(backup)
		if !ok || bsum != sum {
			return removed, fmt.Errorf("the backup of %s does not match the original; nothing was removed", src)
		}
		if err := ops.removeSource(src); err != nil {
			// The backup is redundant now, so it is not left behind.
			_ = os.Remove(backup)
			return removed, fmt.Errorf("remove the original transcript %s: %w", src, err)
		}
		removed = append(removed, relocatedSource{Path: src, Backup: backup, BackupSHA: bsum})
	}
	return removed, nil
}

// restoreRelocatedSources puts back every original this run had already removed,
// newest first, and drops the backups it consumed.
func restoreRelocatedSources(removed []relocatedSource) error {
	var errs []error
	for i := len(removed) - 1; i >= 0; i-- {
		src := removed[i]
		backup, err := os.Open(src.Backup)
		if err != nil {
			errs = append(errs, fmt.Errorf("open backup for %s: %w", src.Path, err))
			continue
		}
		copyErr := safety.CopyAtomic(src.Path, backup)
		closeErr := backup.Close()
		if copyErr != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", src.Path, copyErr))
			continue
		}
		if closeErr != nil {
			errs = append(errs, fmt.Errorf("close backup for %s: %w", src.Path, closeErr))
			continue
		}
		if err := os.Remove(src.Backup); err != nil {
			errs = append(errs, fmt.Errorf("remove restored backup %s: %w", src.Backup, err))
		}
	}
	return errors.Join(errs...)
}

// rollbackProjectMove renames NEW back to OLD after a failure, when relocate had
// moved the directory itself.
func rollbackProjectMove(ops relocateOps, moved bool, oldPath, newPath string) error {
	if !moved {
		return nil
	}
	return ops.rename(newPath, oldPath)
}

// reportRelocateRollback prints whatever part of the unwinding did not succeed,
// so a half-restored state is never silent.
func reportRelocateRollback(stderr io.Writer, importErr, sourceErr, moveErr error) {
	if sourceErr != nil {
		fmt.Fprintf(stderr, "error: restoring the original transcripts was incomplete: %v\n", sourceErr)
	}
	if importErr != nil {
		fmt.Fprintf(stderr, "error: session rollback was incomplete: %v\n", importErr)
	}
	if moveErr != nil {
		fmt.Fprintf(stderr, "error: project rollback was incomplete: %v\n", moveErr)
	}
}

// sourceBackupPaths lists the backups kept for the removed originals.
func sourceBackupPaths(removed []relocatedSource) []string {
	var out []string
	for _, r := range removed {
		out = append(out, r.Backup)
	}
	return out
}

// recordClaudeRelocateJournal writes one journal covering both halves of the
// relocation: the transcripts written under the new project folder and the
// originals removed from the old one. Each new copy names its original via Pair,
// so `cct undo` restores the original first and never deletes the copy while the
// original is still missing. Journaling is best-effort — the files are already
// safely written, so a failure here only means this relocation cannot be undone.
func recordClaudeRelocateJournal(home codexhome.Home, label string, res bundle.ImportResult, removed []relocatedSource, stderr io.Writer) {
	// The original a given destination came from, so copies can be paired with the
	// removal record for the same session.
	originalOf := map[string]string{}
	for _, r := range removed {
		originalOf[relocatePathKey(r.Path)] = r.Path
	}
	sourceByBundlePath := map[string]string{}
	type sessionLabel struct{ threadID, preview string }
	labelBySource := map[string]sessionLabel{}
	for _, ms := range res.Manifest.Sessions {
		src := absPath(ms.OriginalPath)
		sourceByBundlePath[ms.BundlePath] = src
		labelBySource[relocatePathKey(src)] = sessionLabel{ms.ThreadID, ms.Preview}
	}

	var entries []undo.Entry
	for _, r := range removed {
		label := labelBySource[relocatePathKey(r.Path)]
		entries = append(entries, undo.Entry{
			Action: "relocate-remove", Dest: r.Path, Backup: r.Backup, BackupSHA: r.BackupSHA, Removed: true,
			ThreadID: label.threadID, Preview: label.preview,
		})
	}
	for _, it := range res.Items {
		dest := absPath(it.DestPath)
		switch it.Action {
		case bundle.ActionImport, bundle.ActionImportCopy:
			sum, ok := fileSHA(dest)
			if !ok {
				continue
			}
			pair := ""
			if src, found := sourceByBundlePath[it.BundlePath]; found {
				pair = originalOf[relocatePathKey(src)]
			}
			entries = append(entries, undo.Entry{
				Action: string(it.Action), Dest: dest, WroteSHA: sum, Created: true, Pair: pair,
				ThreadID: threadIDFor(res, it.BundlePath, it.NewThreadID), Preview: previewFor(res, it.BundlePath),
			})
		case bundle.ActionReplace, bundle.ActionUpdate:
			if it.BackupPath == "" {
				continue
			}
			backup := absPath(it.BackupPath)
			wrote, okW := fileSHA(dest)
			bsum, okB := fileSHA(backup)
			if !okW || !okB {
				continue
			}
			entries = append(entries, undo.Entry{
				Action: string(it.Action), Dest: dest, WroteSHA: wrote, Backup: backup, BackupSHA: bsum,
				ThreadID: threadIDFor(res, it.BundlePath, it.NewThreadID), Preview: previewFor(res, it.BundlePath),
			})
		}
	}
	if len(entries) == 0 {
		return
	}

	dir, err := cctpaths.ConfigDir("")
	if err != nil {
		fmt.Fprintf(stderr, "note: relocation succeeded but could not locate cct config dir for undo: %v\n", err)
		return
	}
	j := undo.Journal{
		Version: undo.JournalVersion,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Tool:    string(agent.Claude),
		Home:    absPath(home.Root),
		Bundle:  label,
		Entries: entries,
	}
	if err := undo.Validate(j); err != nil {
		// A journal undo would refuse to replay is worse than none: it would make
		// every later `cct undo` fail on this file. Say so instead of writing it.
		fmt.Fprintf(stderr, "note: relocation succeeded but its undo journal was rejected by its own validation (%v); this relocation cannot be undone\n", err)
		return
	}
	if _, err := undo.Record(dir, j); err != nil {
		fmt.Fprintf(stderr, "note: relocation succeeded but recording undo failed: %v\n", err)
	}
}
