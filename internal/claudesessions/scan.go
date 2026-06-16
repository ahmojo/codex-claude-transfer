package claudesessions

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// ScanOptions controls how a Claude home is scanned. It mirrors the Codex
// sessions.ScanOptions shape for symmetry, though Claude Code has no archived or
// compressed sessions today (the fields are accepted and ignored).
type ScanOptions struct {
	IncludeArchived      bool
	DecompressCompressed bool
}

// Scan discovers Claude Code transcripts under <home>/projects/<encoded-cwd>/.
// Every *.jsonl file in a project folder is a session (a logical conversation may
// also include sidechain lines inside the same file). Results reuse the shared
// sessions.Session / sessions.ScanResult types and are sorted most-recent-first.
//
// Scan is defensive: an unreadable directory or a single unparseable transcript
// produces a warning and is otherwise skipped, never aborting the whole scan.
func Scan(home claudehome.Home, _ ScanOptions) (sessions.ScanResult, error) {
	var result sessions.ScanResult

	root := home.ProjectsDir
	if _, err := os.Stat(root); err != nil {
		// A missing projects directory is normal on a fresh install.
		return result, nil
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("cannot access %s: %v", path, err))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !isTranscriptFile(d.Name()) {
			return nil
		}
		if s, ok := newSession(root, path, d, &result); ok {
			result.Sessions = append(result.Sessions, s)
		}
		return nil
	})
	if walkErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("walk error under %s: %v", root, walkErr))
	}

	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].ModTime.After(result.Sessions[j].ModTime)
	})
	return result, nil
}

func newSession(root, path string, d fs.DirEntry, result *sessions.ScanResult) (sessions.Session, bool) {
	info, err := d.Info()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot stat %s: %v", path, err))
		return sessions.Session{}, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = d.Name()
	}

	s := sessions.Session{
		Path:      path,
		RelPath:   filepath.ToSlash(rel),
		FileName:  d.Name(),
		SizeBytes: info.Size(),
		ModTime:   info.ModTime(),
	}
	result.Files++

	meta, warnings, err := parseTranscript(path)
	s.Warnings = append(s.Warnings, warnings...)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot read %s: %v", path, err))
		result.Invalid++
		return s, true
	}

	applyMeta(&s, meta, fallbackIDFromName(d.Name()))
	if s.Parsed {
		result.Valid++
	} else {
		result.Invalid++
	}
	return s, true
}

// isTranscriptFile reports whether name is a Claude Code transcript (.jsonl).
func isTranscriptFile(name string) bool {
	return strings.HasSuffix(name, claudehome.SessionExt) && name != claudehome.SessionExt
}

// fallbackIDFromName uses the transcript filename (the session uuid) as the
// thread id when the file did not record a sessionId field.
func fallbackIDFromName(name string) string {
	return strings.TrimSuffix(name, claudehome.SessionExt)
}
