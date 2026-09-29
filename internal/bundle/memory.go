package bundle

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// verifyMemoryBinding checks the manifest's memory paths before import can write
// any entries. The relative filename must stay within the named project's
// memory directory and must describe the exact ZIP entry being imported.
func verifyMemoryBinding(zr *zip.Reader, memory []ManifestMemory, checksums Checksums) error {
	entries := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		entries[f.Name] = true
	}
	seen := make(map[string]bool, len(memory))
	for _, mm := range memory {
		if _, err := safety.CleanRelPath(mm.Rel); err != nil {
			return fmt.Errorf("unsafe memory filename %q: %w", mm.Rel, err)
		}
		if !safety.IsClaudeMemoryEntry(mm.BundlePath) ||
			memoryBundlePath(mm.ProjectCWD, mm.Rel) != mm.BundlePath {
			return fmt.Errorf("memory %q is not bound to its project and filename", mm.BundlePath)
		}
		if seen[mm.BundlePath] {
			return fmt.Errorf("manifest lists memory %q more than once", mm.BundlePath)
		}
		seen[mm.BundlePath] = true
		if !entries[mm.BundlePath] || mm.SHA256 != checksums[mm.BundlePath] {
			return fmt.Errorf("memory %q is missing or its checksum does not match the bundle", mm.BundlePath)
		}
	}
	return nil
}

// Claude Code keeps a project's auto memory next to its transcripts, in
// projects/<encoded-cwd>/memory/. It is machine-local by Claude's own design, so
// cct only carries it when asked twice: `export --with-memory` puts it in the
// bundle, and `import --with-memory` writes it out again. A bundle that holds
// memory is otherwise inert — an import without the flag, or an older cct that
// predates the manifest field, simply skips those entries.

// memoryBundlePath is where a project's memory file lives inside a bundle:
// projects/<encoded-cwd>/memory/<rel>, mirroring the on-disk layout.
func memoryBundlePath(cwd, rel string) string {
	return path.Join(claudehome.ProjectsSubdir, claudehome.EncodeCWD(cwd), claudehome.MemorySubdir, rel)
}

// memoryDestRelForCWD maps a bundle memory entry onto the destination project,
// so an import that remaps the cwd puts the file under the right folder.
func memoryDestRelForCWD(rel, newCWD string) string {
	return path.Join(claudehome.ProjectsSubdir, claudehome.EncodeCWD(newCWD), claudehome.MemorySubdir, rel)
}

// importMemoryEntry plans and, unless this is a dry run, performs one memory
// file's import. Memory is never overwritten: a destination that already holds
// different bytes is reported as a conflict and left alone, exactly as a
// diverged session would be. Entries the manifest does not describe are skipped
// rather than guessed at, because without a recorded project cwd there is no
// safe way to know where the file belongs after a remap.
func importMemoryEntry(zr *zip.Reader, home codexhome.Home, rel string, byBundlePath map[string]ManifestMemory,
	mappings []CWDMapping, opts ImportOptions, result *ImportResult) error {
	item := ImportItem{BundlePath: rel, Memory: true}

	mm, described := byBundlePath[rel]
	switch {
	case !opts.WithMemory:
		item.Action = ActionSkipNonSession
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: project memory in the bundle was not imported (pass --with-memory to write it)", rel))
		result.Items = append(result.Items, item)
		result.SkippedOther++
		return nil
	case !described:
		item.Action = ActionSkipNonSession
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: memory file is not described in the manifest; skipped", rel))
		result.Items = append(result.Items, item)
		result.SkippedOther++
		return nil
	}

	item.OriginalCWD = mm.ProjectCWD
	destCWD := mm.ProjectCWD
	if m := matchMapping(mm.ProjectCWD, mappings); m != nil {
		destCWD = m.New
		item.Mapped = true
	}
	destRel := memoryDestRelForCWD(mm.Rel, destCWD)
	dest, err := safety.DestPath(home.Root, destRel)
	if err != nil {
		return fmt.Errorf("memory %s: %w", rel, err)
	}
	item.DestPath = dest

	switch existing, statErr := os.Lstat(dest); {
	case errors.Is(statErr, os.ErrNotExist):
		item.Action = ActionImport
	case statErr != nil:
		return fmt.Errorf("inspect %s: %w", dest, statErr)
	case !existing.Mode().IsRegular():
		item.Action = ActionConflict
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: %s exists and is not a regular file; left untouched", rel, dest))
	default:
		sum, sumErr := sha256File(dest)
		switch {
		case sumErr == nil && sum == mm.SHA256:
			item.Action = ActionSkipIdentical
		default:
			item.Action = ActionConflict
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: this machine has a different %s; memory is never overwritten", rel, mm.Rel))
		}
	}

	switch item.Action {
	case ActionImport:
		if !opts.DryRun {
			if err := copyEntry(zr, rel, dest); err != nil {
				return fmt.Errorf("write memory %s: %w", dest, err)
			}
		}
		result.MemoryImported++
	case ActionSkipIdentical:
		result.MemorySkipped++
	case ActionConflict:
		result.MemoryConflicts++
	}
	result.Items = append(result.Items, item)
	return nil
}

// collectProjectMemory lists the memory files belonging to the selected
// sessions' projects, one entry per file, deduplicated by project. It reads
// only, and a project without a memory directory contributes nothing.
func collectProjectMemory(claudeHome claudehome.Home, selected []sessions.Session) ([]ManifestMemory, []string, error) {
	seen := map[string]bool{}
	var cwds []string
	for _, s := range selected {
		if s.CWD == "" || seen[s.CWD] {
			continue
		}
		seen[s.CWD] = true
		cwds = append(cwds, s.CWD)
	}

	var out []ManifestMemory
	var warnings []string
	for _, cwd := range cwds {
		dir := claudeHome.MemoryDir(cwd)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot read the memory directory for %s: %v", cwd, err))
			continue
		}
		if !info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("%s is not a directory; its memory was not exported", dir))
			continue
		}
		walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("cannot read %s: %w", p, err)
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("%s is not a regular file; skipped", p))
				return nil
			}
			rel, relErr := filepath.Rel(dir, p)
			if relErr != nil {
				return relErr
			}
			slashRel := filepath.ToSlash(rel)
			fi, statErr := d.Info()
			if statErr != nil {
				return statErr
			}
			out = append(out, ManifestMemory{
				ProjectCWD:   cwd,
				Rel:          slashRel,
				OriginalPath: p,
				BundlePath:   memoryBundlePath(cwd, slashRel),
				SizeBytes:    fi.Size(),
			})
			return nil
		})
		if walkErr != nil {
			return nil, warnings, walkErr
		}
	}
	return out, warnings, nil
}
