package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

const relocateUsage = "usage: cct relocate OLD NEW [--move-project] [--dry-run] [--include-archived] [--codex-home <path>] [--tool codex] [--json]"

// relocateFlags contains only the options supported by relocate. Keeping a
// command-specific parser prevents unrelated export/import flags from being
// accepted and then silently ignored.
type relocateFlags struct {
	codexHome       string
	tool            string
	moveProject     bool
	includeArchived bool
	dryRun          bool
	jsonOut         bool
	positional      []string
}

// relocateOps isolates the three stateful operations so rollback behavior can
// be exercised with deterministic failures in tests.
type relocateOps struct {
	export       func(codexhome.Home, bundle.ExportOptions) (bundle.ExportResult, error)
	importBundle func(codexhome.Home, bundle.ImportOptions) (bundle.ImportResult, error)
	rename       func(string, string) error
}

var defaultRelocateOps = relocateOps{
	export:       bundle.Export,
	importBundle: bundle.Import,
	rename:       os.Rename,
}

// relocateJSON is the stable machine-readable summary emitted by --json.
type relocateJSON struct {
	Tool          string   `json:"tool"`
	OldPath       string   `json:"old_path"`
	NewPath       string   `json:"new_path"`
	Sessions      int      `json:"sessions"`
	Remapped      int      `json:"remapped"`
	Replaced      int      `json:"replaced"`
	ProjectAction string   `json:"project_action"`
	DryRun        bool     `json:"dry_run"`
	Backups       []string `json:"backups,omitempty"`
}

// runRelocate safely rewrites Codex session cwd metadata after a project folder
// changes location. It delegates all session writes to bundle.Import, preserving
// the existing backup, checksum, atomic-write, and undo guarantees.
func runRelocate(args []string, stdout, stderr io.Writer) int {
	return runRelocateWithOps(args, stdout, stderr, defaultRelocateOps)
}

// runRelocateWithOps contains the command workflow with injectable stateful
// operations for failure-path tests.
func runRelocateWithOps(args []string, stdout, stderr io.Writer, ops relocateOps) int {
	f, err := parseRelocateFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) != 2 {
		fmt.Fprintln(stderr, relocateUsage)
		return 2
	}

	common := commonFlags{codexHome: f.codexHome, tool: f.tool}
	kind, ok := resolveTool(common, stderr)
	if !ok {
		return 2
	}
	if kind != agent.Codex {
		fmt.Fprintln(stderr, "error: relocate currently supports Codex only; Claude Code stores cwd in both transcript contents and its project-directory layout")
		return 2
	}
	home, ok := resolveHome(common, stderr)
	if !ok {
		return 1
	}

	oldPath, newPath, err := resolveRelocatePaths(f.positional[0], f.positional[1])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if err := validateRelocateProjectPaths(oldPath, newPath, home.Root, f.moveProject); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	tmpDir, err := os.MkdirTemp("", "cct-relocate-")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot create private temporary directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)
	bundlePath := filepath.Join(tmpDir, "sessions.codexbundle")

	exported, err := ops.export(home, bundle.ExportOptions{
		Tool:            agent.Codex,
		ProjectPath:     oldPath,
		OutputPath:      bundlePath,
		IncludeArchived: f.includeArchived,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot export sessions for %s: %v\n", oldPath, err)
		return 1
	}

	mapping := []bundle.CWDMapping{{Old: oldPath, New: newPath}}
	importOpts := bundle.ImportOptions{
		BundlePath:        bundlePath,
		DryRun:            true,
		IncludeArchived:   f.includeArchived,
		MapCWD:            mapping,
		ReplaceWithBackup: true,
	}
	preview, err := ops.importBundle(home, importOpts)
	if err != nil {
		fmt.Fprintf(stderr, "error: relocate preflight failed: %v\n", err)
		return 1
	}
	if err := validateRelocatePlan(exported, preview); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := verifyRelocateSources(exported.Manifest); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if f.dryRun {
		printRelocateResult(stdout, f, oldPath, newPath, preview, nil)
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
		importErr = validateRelocateImportResult(exported, result)
	}
	if importErr != nil {
		rollbackErr := rollbackRelocateImport(result)
		var moveBackErr error
		if projectMoved {
			moveBackErr = ops.rename(newPath, oldPath)
		}
		fmt.Fprintf(stderr, "error: relocate import failed: %v\n", importErr)
		if rollbackErr != nil {
			fmt.Fprintf(stderr, "error: session rollback was incomplete: %v\n", rollbackErr)
		}
		if moveBackErr != nil {
			fmt.Fprintf(stderr, "error: project rollback was incomplete: %v\n", moveBackErr)
		}
		return 1
	}

	recordUndoJournal(common, kind, home, fmt.Sprintf("relocate %s -> %s", oldPath, newPath), result, stderr)
	printRelocateResult(stdout, f, oldPath, newPath, result, backupPaths(result))
	return 0
}

// parseRelocateFlags accepts the deliberately small relocate interface.
func parseRelocateFlags(args []string) (relocateFlags, error) {
	var f relocateFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--codex-home":
			value, err := takeValue(args, &i, "--codex-home")
			if err != nil {
				return f, err
			}
			f.codexHome = value
		case hasPrefix(arg, "--codex-home="):
			f.codexHome = arg[len("--codex-home="):]
		case arg == "--tool":
			value, err := takeValue(args, &i, "--tool")
			if err != nil {
				return f, err
			}
			f.tool = value
		case hasPrefix(arg, "--tool="):
			f.tool = arg[len("--tool="):]
		case arg == "--move-project":
			f.moveProject = true
		case arg == "--include-archived":
			f.includeArchived = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--json":
			f.jsonOut = true
		case hasPrefix(arg, "-"):
			return f, fmt.Errorf("unknown flag: %q", arg)
		default:
			f.positional = append(f.positional, arg)
		}
	}
	return f, nil
}

// resolveRelocatePaths converts both operands to cleaned absolute paths and
// rejects identical or nested locations before any filesystem change.
func resolveRelocatePaths(oldArg, newArg string) (string, string, error) {
	oldPath, err := filepath.Abs(oldArg)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve OLD path %q: %w", oldArg, err)
	}
	newPath, err := filepath.Abs(newArg)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve NEW path %q: %w", newArg, err)
	}
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if pathEqualCLI(oldPath, newPath) {
		return "", "", errors.New("OLD and NEW resolve to the same path")
	}
	if pathContains(oldPath, newPath) || pathContains(newPath, oldPath) {
		return "", "", errors.New("OLD and NEW must not contain one another")
	}
	return oldPath, newPath, nil
}

// validateRelocateProjectPaths enforces the filesystem preconditions for either
// a session-only remap or an opt-in project-directory rename.
func validateRelocateProjectPaths(oldPath, newPath, homeRoot string, moveProject bool) error {
	if moveProject {
		if pathsOverlap(oldPath, homeRoot) || pathsOverlap(newPath, homeRoot) {
			return errors.New("the project paths must not overlap the Codex home")
		}
		oldInfo, err := os.Lstat(oldPath)
		if err != nil {
			return fmt.Errorf("cannot inspect OLD project: %w", err)
		}
		if oldInfo.Mode()&os.ModeSymlink != 0 || !oldInfo.IsDir() {
			return errors.New("OLD must be a real directory, not a file or symbolic link")
		}
		if _, err := os.Lstat(newPath); err == nil {
			return errors.New("NEW already exists; omit --move-project after copying/moving the project yourself")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect NEW project: %w", err)
		}
		parentInfo, err := os.Stat(filepath.Dir(newPath))
		if err != nil {
			return fmt.Errorf("cannot inspect NEW parent directory: %w", err)
		}
		if !parentInfo.IsDir() {
			return errors.New("NEW parent is not a directory")
		}
		return nil
	}

	newInfo, err := os.Stat(newPath)
	if err != nil {
		return fmt.Errorf("NEW must already exist when --move-project is omitted: %w", err)
	}
	if !newInfo.IsDir() {
		return errors.New("NEW must be a directory")
	}
	return nil
}

// pathsOverlap reports whether two paths are equal or one contains the other.
func pathsOverlap(first, second string) bool {
	return pathEqualCLI(first, second) || pathContains(first, second) || pathContains(second, first)
}

// pathContains reports whether child is strictly below parent. Rel is used so
// path separators and Windows volume handling follow the host filesystem.
func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	if runtime.GOOS == "windows" {
		rel = strings.ToLower(rel)
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validateRelocatePlan requires every selected rollout to be mapped in place.
// It refuses compressed sessions whose cwd cannot be inspected or rewritten on
// the current machine.
func validateRelocatePlan(exported bundle.ExportResult, preview bundle.ImportResult) error {
	want := exported.IncludedCount
	if exported.CompressedSkipped > 0 {
		return fmt.Errorf("%d compressed session(s) have unknown cwd; install zstd and retry so relocation can verify every matching session", exported.CompressedSkipped)
	}
	if preview.MappedCompressedSkipped > 0 {
		return fmt.Errorf("%d compressed session(s) require the external zstd tool before their cwd can be relocated", preview.MappedCompressedSkipped)
	}
	if preview.Mapped != want {
		return fmt.Errorf("preflight would remap %d of %d selected sessions; no changes were made", preview.Mapped, want)
	}
	if preview.Replaced != want || preview.Imported != 0 || preview.Conflicts != 0 || preview.SkippedOther != 0 {
		return fmt.Errorf("preflight expected %d in-place replacements but found %d; no changes were made", want, preview.Replaced)
	}
	return nil
}

// validateRelocateImportResult applies the preflight completeness invariant to
// the real import. External dependencies such as zstd can disappear between
// the two calls, so a nil import error alone is not proof that every selected
// rollout was rewritten in place.
func validateRelocateImportResult(exported bundle.ExportResult, result bundle.ImportResult) error {
	want := exported.IncludedCount
	if result.MappedCompressedSkipped == 0 &&
		result.Mapped == want &&
		result.Replaced == want &&
		result.Imported == 0 &&
		result.Conflicts == 0 &&
		result.SkippedOther == 0 {
		return nil
	}
	return fmt.Errorf(
		"real import was incomplete: remapped %d and replaced %d of %d selected sessions (compressed-unmapped=%d, imported=%d, conflicts=%d, skipped=%d)",
		result.Mapped,
		result.Replaced,
		want,
		result.MappedCompressedSkipped,
		result.Imported,
		result.Conflicts,
		result.SkippedOther,
	)
}

// verifyRelocateSources catches a session that changed after export, avoiding an
// overwrite based on stale bytes when Codex is still appending to a rollout.
func verifyRelocateSources(manifest bundle.Manifest) error {
	for _, session := range manifest.Sessions {
		info, err := os.Lstat(session.OriginalPath)
		if err != nil {
			return fmt.Errorf("session changed while relocation was prepared (%s): %w", session.OriginalPath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("session changed while relocation was prepared (%s is no longer a regular file)", session.OriginalPath)
		}
		sum, ok := fileSHA(session.OriginalPath)
		if !ok || sum != session.SHA256 {
			return fmt.Errorf("session changed while relocation was prepared (%s); stop Codex and retry", session.OriginalPath)
		}
	}
	return nil
}

// rollbackRelocateImport restores every backup produced before a failed import.
// Backups are processed in reverse order and retained whenever restoration fails.
func rollbackRelocateImport(result bundle.ImportResult) error {
	var errs []error
	for i := len(result.Items) - 1; i >= 0; i-- {
		item := result.Items[i]
		if item.BackupPath == "" {
			continue
		}
		backup, err := os.Open(item.BackupPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("open backup for %s: %w", item.DestPath, err))
			continue
		}
		copyErr := safety.CopyAtomic(item.DestPath, backup)
		closeErr := backup.Close()
		if copyErr != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", item.DestPath, copyErr))
			continue
		}
		if closeErr != nil {
			errs = append(errs, fmt.Errorf("close backup for %s: %w", item.DestPath, closeErr))
			continue
		}
		if err := os.Remove(item.BackupPath); err != nil {
			errs = append(errs, fmt.Errorf("remove restored backup %s: %w", item.BackupPath, err))
		}
	}
	return errors.Join(errs...)
}

// backupPaths returns the persistent backups created by a successful import.
func backupPaths(result bundle.ImportResult) []string {
	var paths []string
	for _, item := range result.Items {
		if item.BackupPath != "" {
			paths = append(paths, item.BackupPath)
		}
	}
	return paths
}

// printRelocateResult emits either a compact JSON object or a human-oriented
// summary without exposing the temporary bundle path.
func printRelocateResult(stdout io.Writer, f relocateFlags, oldPath, newPath string, result bundle.ImportResult, backups []string) {
	action := "not-requested"
	if f.moveProject {
		if f.dryRun {
			action = "would-move"
		} else {
			action = "moved"
		}
	}
	if f.jsonOut {
		writeJSON(stdout, relocateJSON{
			Tool:          string(agent.Codex),
			OldPath:       oldPath,
			NewPath:       newPath,
			Sessions:      len(result.Manifest.Sessions),
			Remapped:      result.Mapped,
			Replaced:      result.Replaced,
			ProjectAction: action,
			DryRun:        f.dryRun,
			Backups:       backups,
		})
		return
	}

	fmt.Fprintln(stdout, "Codex project relocation")
	fmt.Fprintf(stdout, "Old path: %s\n", oldPath)
	fmt.Fprintf(stdout, "New path: %s\n", newPath)
	fmt.Fprintf(stdout, "Sessions: %d\n", len(result.Manifest.Sessions))
	fmt.Fprintf(stdout, "Session cwd rewrites: %d\n", result.Mapped)
	switch action {
	case "would-move":
		fmt.Fprintln(stdout, "Project directory: would move")
	case "moved":
		fmt.Fprintln(stdout, "Project directory: moved")
	default:
		fmt.Fprintln(stdout, "Project directory: already moved/copied by user")
	}
	if f.dryRun {
		fmt.Fprintln(stdout, "No files were changed because --dry-run was used.")
		return
	}
	fmt.Fprintf(stdout, "Session backups kept: %d\n", len(backups))
	fmt.Fprintln(stdout, "Relocation complete. Restart Codex, then run `codex resume` from the new folder.")
	if f.moveProject {
		fmt.Fprintln(stdout, "Note: `cct undo` restores session files only; move the project directory back separately.")
	}
}
