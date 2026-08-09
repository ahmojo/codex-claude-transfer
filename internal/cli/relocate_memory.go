package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// Claude Code keeps a project's auto memory next to its transcripts, under
// projects/<encoded-cwd>/memory/, keyed by the same encoded path. Moving only the
// transcripts therefore strands the memory under the old key: the project keeps
// its conversations and silently loses what the agent had learned about it.
//
// So relocation moves the memory directory too, with the same discipline the
// transcripts get: every file is copied and verified first, an original is
// removed only after its copy exists, and both halves go into the undo journal.
// A destination file that already exists with different content stops the whole
// relocation rather than being overwritten.

// memoryFile is one file inside a project's memory directory, planned for
// relocation.
type memoryFile struct {
	// Rel is the path relative to the project's memory directory, so nested
	// directories survive the move.
	Rel string
	// Src and Dest are absolute paths under the old and new project folders.
	Src  string
	Dest string
	// SHA is the source file's content hash, verified again before removal.
	SHA string
	// AlreadyThere marks a destination that already holds these exact bytes.
	// Such a file is not written again, only the original is removed.
	AlreadyThere bool
}

// planClaudeMemoryMove lists the memory files a relocation has to move. It reads
// only: nothing is written, so the same plan backs both --dry-run and the real
// run. An empty plan (no memory directory, or an in-place relocation) is normal.
func planClaudeMemoryMove(claudeHome claudehome.Home, oldPath, newPath string, inPlace bool) ([]memoryFile, error) {
	if inPlace {
		return nil, nil
	}
	srcDir := claudeHome.MemoryDir(oldPath)
	destDir := claudeHome.MemoryDir(newPath)
	info, err := os.Lstat(srcDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot inspect the project's memory directory %s: %w", srcDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory; refusing to relocate the project's memory", srcDir)
	}

	var files []memoryFile
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("refusing to relocate %s: it is not a regular file", path)
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return relErr
		}
		sum, ok := fileSHA(path)
		if !ok {
			return fmt.Errorf("cannot hash the memory file %s", path)
		}
		dest := filepath.Join(destDir, rel)
		f := memoryFile{Rel: filepath.ToSlash(rel), Src: path, Dest: dest, SHA: sum}

		switch destInfo, statErr := os.Lstat(dest); {
		case errors.Is(statErr, os.ErrNotExist):
		case statErr != nil:
			return fmt.Errorf("cannot inspect %s: %w", dest, statErr)
		case !destInfo.Mode().IsRegular():
			return fmt.Errorf("%s already exists and is not a regular file; relocation stopped", dest)
		default:
			destSum, okDest := fileSHA(dest)
			if !okDest {
				return fmt.Errorf("cannot hash the existing memory file %s", dest)
			}
			if destSum != sum {
				return fmt.Errorf("the new project folder already has a different %s (%s); relocation stopped so nothing the agent remembered is overwritten",
					filepath.ToSlash(rel), dest)
			}
			f.AlreadyThere = true
		}
		files = append(files, f)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return files, nil
}

// copyClaudeMemory writes each planned file under the new project folder and
// verifies the bytes that landed. It returns the files it created — including on
// failure, so the caller can remove them again.
func copyClaudeMemory(files []memoryFile) ([]memoryFile, error) {
	var created []memoryFile
	for _, f := range files {
		if f.AlreadyThere {
			continue
		}
		src, err := os.Open(f.Src)
		if err != nil {
			return created, fmt.Errorf("open the memory file %s: %w", f.Src, err)
		}
		copyErr := safety.CopyAtomic(f.Dest, src)
		closeErr := src.Close()
		if copyErr != nil {
			return created, fmt.Errorf("write %s: %w", f.Dest, copyErr)
		}
		if closeErr != nil {
			return created, fmt.Errorf("close %s: %w", f.Src, closeErr)
		}
		sum, ok := fileSHA(f.Dest)
		if !ok || sum != f.SHA {
			created = append(created, f)
			return created, fmt.Errorf("the copy of %s does not match the original; relocation stopped", f.Rel)
		}
		created = append(created, f)
	}
	return created, nil
}

// removeClaudeMemorySources backs up and deletes each original memory file after
// its copy exists, re-verifying every file immediately beforehand: it must still
// be a regular file with the exact bytes that were planned, and must live inside
// the Claude projects directory.
func removeClaudeMemorySources(files []memoryFile, claudeHome claudehome.Home, ops relocateOps) ([]relocatedSource, error) {
	var removed []relocatedSource
	for _, f := range files {
		if !pathContains(claudeHome.ProjectsDir, f.Src) {
			return removed, fmt.Errorf("refusing to remove %s: it is outside %s", f.Src, claudeHome.ProjectsDir)
		}
		info, err := os.Lstat(f.Src)
		if err != nil {
			return removed, fmt.Errorf("cannot inspect the memory file %s: %w", f.Src, err)
		}
		if !info.Mode().IsRegular() {
			return removed, fmt.Errorf("refusing to remove %s: it is no longer a regular file", f.Src)
		}
		sum, ok := fileSHA(f.Src)
		if !ok || sum != f.SHA {
			return removed, fmt.Errorf("the memory file %s changed while the project was being relocated; stop Claude Code and retry", f.Src)
		}
		backup, err := bundle.BackupFile(f.Src)
		if err != nil {
			return removed, fmt.Errorf("back up %s before removing it: %w", f.Src, err)
		}
		bsum, ok := fileSHA(backup)
		if !ok || bsum != sum {
			return removed, fmt.Errorf("the backup of %s does not match the original; nothing was removed", f.Src)
		}
		if err := ops.removeSource(f.Src); err != nil {
			_ = os.Remove(backup)
			return removed, fmt.Errorf("remove the memory file %s: %w", f.Src, err)
		}
		removed = append(removed, relocatedSource{Path: f.Src, Backup: backup, BackupSHA: bsum})
	}
	// The now-empty memory directory is tidied up, best effort: an unexpected
	// leftover file simply keeps the directory.
	if len(removed) > 0 {
		pruneEmptyDirs(filepath.Dir(removed[0].Path), claudeHome.ProjectsDir)
	}
	return removed, nil
}

// rollbackClaudeMemoryCopies deletes the copies this run created, newest first,
// so a failed relocation leaves no half-populated memory under the new project.
func rollbackClaudeMemoryCopies(created []memoryFile) error {
	var errs []error
	for i := len(created) - 1; i >= 0; i-- {
		f := created[i]
		if f.AlreadyThere {
			continue
		}
		if err := os.Remove(f.Dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", f.Dest, err))
		}
	}
	if len(created) > 0 {
		pruneEmptyDirs(filepath.Dir(created[0].Dest), "")
	}
	return errors.Join(errs...)
}

// pruneEmptyDirs removes dir and its now-empty parents, stopping at (and never
// removing) stopAt. It is best effort: a directory that still holds anything is
// simply kept.
func pruneEmptyDirs(dir, stopAt string) {
	stop := filepath.Clean(stopAt)
	for d := filepath.Clean(dir); d != "" && d != stop && d != filepath.Dir(d); d = filepath.Dir(d) {
		if stop != "" && !strings.HasPrefix(d, stop+string(filepath.Separator)) {
			return
		}
		if err := os.Remove(d); err != nil {
			return
		}
	}
}

// memoryMovedCount reports how many memory files a plan actually writes, for the
// command's summary. Files already present at the destination are not counted as
// written but are still moved out of the old folder.
func memoryMovedCount(files []memoryFile) int { return len(files) }
