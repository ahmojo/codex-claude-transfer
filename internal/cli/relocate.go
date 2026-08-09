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
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

const relocateUsage = "usage: cct relocate OLD NEW [--move-project] [--dry-run] [--include-archived] [--tool codex|claude] [--codex-home <path>] [--claude-home <path>] [--json]"

// relocateFlags contains only the options supported by relocate. Keeping a
// command-specific parser prevents unrelated export/import flags from being
// accepted and then silently ignored.
type relocateFlags struct {
	codexHome       string
	claudeHome      string
	tool            string
	moveProject     bool
	includeArchived bool
	dryRun          bool
	jsonOut         bool
	positional      []string
}

// relocateOps isolates the stateful operations so rollback behavior can be
// exercised with deterministic failures in tests.
type relocateOps struct {
	export       func(codexhome.Home, bundle.ExportOptions) (bundle.ExportResult, error)
	importBundle func(codexhome.Home, bundle.ImportOptions) (bundle.ImportResult, error)
	rename       func(string, string) error
	// removeSource deletes an original Claude transcript after its relocated copy
	// was written and backed up.
	removeSource func(string) error
}

var defaultRelocateOps = relocateOps{
	export:       bundle.Export,
	importBundle: bundle.Import,
	rename:       os.Rename,
	removeSource: os.Remove,
}

// relocateJSON is the stable machine-readable summary emitted by --json. The
// last two fields describe the Claude Code workflow, where relocating also moves
// each transcript into the folder encoding the new project path.
type relocateJSON struct {
	Tool           string   `json:"tool"`
	OldPath        string   `json:"old_path"`
	NewPath        string   `json:"new_path"`
	Sessions       int      `json:"sessions"`
	Remapped       int      `json:"remapped"`
	Replaced       int      `json:"replaced"`
	ProjectAction  string   `json:"project_action"`
	DryRun         bool     `json:"dry_run"`
	Backups        []string `json:"backups,omitempty"`
	MovedFiles     int      `json:"moved_files,omitempty"`
	RemovedSources int      `json:"removed_sources,omitempty"`
	MemoryFiles    int      `json:"memory_files,omitempty"`
}

// relocateOutcome carries what the summary printer reports after either agent's
// workflow.
type relocateOutcome struct {
	kind    agent.Kind
	oldPath string
	newPath string
	result  bundle.ImportResult
	backups []string
	// movedFiles counts Claude transcripts written under the new project folder,
	// and removedSources the originals deleted from the old one.
	movedFiles     int
	removedSources int
	// memoryFiles counts the project's auto-memory files that move with it.
	memoryFiles int
}

// runRelocate safely rewrites a project's recorded working directory after its
// folder changes location. It delegates all session writes to bundle.Import,
// preserving the existing backup, checksum, atomic-write, and undo guarantees.
func runRelocate(args []string, stdout, stderr io.Writer) int {
	return runRelocateWithOps(args, stdout, stderr, defaultRelocateOps)
}

// runRelocateWithOps parses and validates the shared arguments, then hands off to
// the workflow for the selected agent. Injectable stateful operations let the
// failure paths be exercised deterministically in tests.
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

	common := commonFlags{codexHome: f.codexHome, claudeHome: f.claudeHome, tool: f.tool}
	kind, ok := resolveTool(common, stderr)
	if !ok {
		return 2
	}

	// Both agents relocate through the same export/import engine, but Claude Code
	// also addresses a project by folder name, so its transcripts move on disk.
	var home codexhome.Home
	var claudeHome claudehome.Home
	if kind == agent.Claude {
		if f.includeArchived {
			fmt.Fprintln(stderr, "error: --include-archived does not apply to Claude Code: it keeps no separate archive location, so every transcript recorded under OLD is already relocated")
			return 2
		}
		claudeHome, ok = resolveClaudeHome(common, stderr)
		if !ok {
			return 1
		}
		home = codexhome.Home{Root: claudeHome.Root, SessionsDir: claudeHome.ProjectsDir, Source: claudeHome.Source}
	} else {
		if f.claudeHome != "" {
			fmt.Fprintln(stderr, "error: --claude-home applies to --tool claude only")
			return 2
		}
		home, ok = resolveHome(common, stderr)
		if !ok {
			return 1
		}
	}

	oldPath, newPath, err := resolveRelocatePaths(f.positional[0], f.positional[1])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if err := validateRelocateProjectPaths(oldPath, newPath, home.Root, kind, f.moveProject); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	if kind == agent.Claude {
		return runRelocateClaude(f, claudeHome, home, oldPath, newPath, stdout, stderr, ops)
	}
	return runRelocateCodex(f, home, oldPath, newPath, stdout, stderr, ops)
}

// runRelocateCodex rewrites Codex session cwd metadata in place: a Codex rollout
// stays at its dated path, so only the file's contents change.
func runRelocateCodex(f relocateFlags, home codexhome.Home, oldPath, newPath string, stdout, stderr io.Writer, ops relocateOps) int {
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
		printRelocateResult(stdout, f, relocateOutcome{
			kind: agent.Codex, oldPath: oldPath, newPath: newPath, result: preview,
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

	recordUndoJournal(commonFlags{}, agent.Codex, home, relocateLabel(oldPath, newPath), result, stderr)
	printRelocateResult(stdout, f, relocateOutcome{
		kind: agent.Codex, oldPath: oldPath, newPath: newPath, result: result, backups: backupPaths(result),
	})
	return 0
}

// relocateLabel is the journal's "bundle" description for a relocation, which has
// no user-visible bundle file (it uses a private temporary one).
func relocateLabel(oldPath, newPath string) string {
	return fmt.Sprintf("relocate %s -> %s", oldPath, newPath)
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
		case arg == "--claude-home":
			value, err := takeValue(args, &i, "--claude-home")
			if err != nil {
				return f, err
			}
			f.claudeHome = value
		case hasPrefix(arg, "--claude-home="):
			f.claudeHome = arg[len("--claude-home="):]
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
func validateRelocateProjectPaths(oldPath, newPath, homeRoot string, kind agent.Kind, moveProject bool) error {
	if moveProject {
		if pathsOverlap(oldPath, homeRoot) || pathsOverlap(newPath, homeRoot) {
			return fmt.Errorf("the project paths must not overlap the %s home", agent.Normalize(kind).Label())
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

// relocatePathKey normalizes a path for map lookups and comparisons, matching the
// case sensitivity of the host filesystem.
func relocatePathKey(p string) string {
	c := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(c)
	}
	return c
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

// rollbackRelocateImport restores every backup produced before a failed import
// and removes files the import unexpectedly created. Items are processed in
// reverse order, and backups are retained whenever restoration fails.
func rollbackRelocateImport(result bundle.ImportResult) error {
	var errs []error
	for i := len(result.Items) - 1; i >= 0; i-- {
		item := result.Items[i]
		if item.BackupPath == "" {
			if item.Action != bundle.ActionImport && item.Action != bundle.ActionImportCopy {
				continue
			}
			if item.DestPath == "" {
				errs = append(errs, errors.New("remove newly created session: destination path is empty"))
				continue
			}
			if err := os.Remove(item.DestPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove newly created session %s: %w", item.DestPath, err))
			}
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
func printRelocateResult(stdout io.Writer, f relocateFlags, out relocateOutcome) {
	kind := agent.Normalize(out.kind)
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
			Tool:           string(kind),
			OldPath:        out.oldPath,
			NewPath:        out.newPath,
			Sessions:       len(out.result.Manifest.Sessions),
			Remapped:       out.result.Mapped,
			Replaced:       out.result.Replaced,
			ProjectAction:  action,
			DryRun:         f.dryRun,
			Backups:        out.backups,
			MovedFiles:     out.movedFiles,
			RemovedSources: out.removedSources,
			MemoryFiles:    out.memoryFiles,
		})
		return
	}

	fmt.Fprintf(stdout, "%s project relocation\n", kind.Label())
	fmt.Fprintf(stdout, "Old path: %s\n", out.oldPath)
	fmt.Fprintf(stdout, "New path: %s\n", out.newPath)
	fmt.Fprintf(stdout, "Sessions: %d\n", len(out.result.Manifest.Sessions))
	fmt.Fprintf(stdout, "Session cwd rewrites: %d\n", out.result.Mapped)
	if kind == agent.Claude {
		verb := "Transcripts moved to the new project folder"
		if f.dryRun {
			verb = "Transcripts that would move to the new project folder"
		}
		fmt.Fprintf(stdout, "%s: %d\n", verb, out.movedFiles)
		if out.memoryFiles > 0 {
			memVerb := "Auto-memory files moved with the project"
			if f.dryRun {
				memVerb = "Auto-memory files that would move with the project"
			}
			fmt.Fprintf(stdout, "%s: %d\n", memVerb, out.memoryFiles)
		}
	}
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
	if kind == agent.Claude && out.removedSources > 0 {
		fmt.Fprintf(stdout, "Originals removed from the old project folder: %d\n", out.removedSources)
	}
	fmt.Fprintf(stdout, "Session backups kept: %d\n", len(out.backups))
	if kind == agent.Claude {
		fmt.Fprintln(stdout, "Relocation complete. Restart Claude Code, then resume the conversation from the new folder.")
	} else {
		fmt.Fprintln(stdout, "Relocation complete. Restart Codex, then run `codex resume` from the new folder.")
	}
	if f.moveProject {
		fmt.Fprintln(stdout, "Note: `cct undo` restores session files only; move the project directory back separately.")
	}
}
