package sessions

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// ScanOptions controls how a Codex home is scanned.
type ScanOptions struct {
	// IncludeArchived also scans the archived_sessions/ directory. By default
	// (false) only active sessions are scanned.
	IncludeArchived bool
	// DecompressCompressed, when true, attempts to recover metadata (cwd, thread
	// id, preview, ...) from .jsonl.zst rollout files by decompressing their head
	// with the external `zstd` tool. It is opt-in and degrades gracefully: when
	// `zstd` is not installed or a file cannot be decompressed, the compressed
	// file is reported as metadata-unknown exactly as when this is false. The
	// compressed files themselves are only read, never modified.
	DecompressCompressed bool
}

// Scan discovers rollout files under the given Codex home. It walks the active
// sessions directory (and, if requested, the archived sessions directory),
// detecting both .jsonl and .jsonl.zst files. Plain files are parsed for
// metadata; compressed files are detected and, when ScanOptions.DecompressCompressed
// is set and the external `zstd` tool is available, decompressed read-only to
// recover their metadata (otherwise reported as metadata-unknown).
//
// Scan is defensive: an unreadable directory or a single unparseable file
// produces a warning and is otherwise skipped, never aborting the whole scan.
// Results are sorted by most-recently-updated first.
func Scan(home codexhome.Home, opts ScanOptions) (ScanResult, error) {
	var result ScanResult

	scanRoot(home.SessionsDir, false, opts, &result)
	if opts.IncludeArchived {
		scanRoot(home.ArchivedSessionsDir, true, opts, &result)
	}

	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].ModTime.After(result.Sessions[j].ModTime)
	})
	return result, nil
}

func scanRoot(root string, archived bool, opts ScanOptions, result *ScanResult) {
	// A missing sessions directory is normal (e.g. fresh Codex install); skip
	// it silently rather than warning.
	if _, err := os.Stat(root); err != nil {
		return
	}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries but keep going.
			result.Warnings = append(result.Warnings, fmt.Sprintf("cannot access %s: %v", path, err))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isRolloutFile(d.Name()) {
			return nil
		}
		s, ok := newSession(root, path, d, archived, opts, result)
		if ok {
			result.Sessions = append(result.Sessions, s)
		}
		return nil
	})
	if walkErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("walk error under %s: %v", root, walkErr))
	}
}

func newSession(root, path string, d fs.DirEntry, archived bool, opts ScanOptions, result *ScanResult) (Session, bool) {
	info, err := d.Info()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot stat %s: %v", path, err))
		return Session{}, false
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = d.Name()
	}

	compressed := strings.HasSuffix(d.Name(), CompressedExt)

	s := Session{
		Path:       path,
		RelPath:    filepath.ToSlash(rel),
		FileName:   d.Name(),
		Compressed: compressed,
		Archived:   archived,
		SizeBytes:  info.Size(),
		ModTime:    info.ModTime(),
	}

	result.Files++

	if compressed {
		result.Compressed++
		// Opt-in: recover metadata by decompressing the head with `zstd`. The
		// compressed file is only read, never modified. Any failure (zstd missing,
		// not valid zstd, no SessionMeta) falls through to the unparsed behavior.
		if opts.DecompressCompressed {
			if meta, warns, err := parseCompressedHead(path); err == nil && meta.FoundSessionMeta {
				applyMeta(&s, meta, d.Name())
				s.Warnings = append(s.Warnings, warns...)
				result.Valid++
				return s, true
			}
		}
		s.Warnings = append(s.Warnings, "compressed (.jsonl.zst) rollout detected; metadata not parsed")
		// Best-effort created_at from the filename timestamp.
		s.CreatedAt = createdAtFromName(d.Name())
		result.Invalid++
		return s, true
	}

	meta, warnings, err := parsePlainRollout(path)
	s.Warnings = append(s.Warnings, warnings...)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot read %s: %v", path, err))
		result.Invalid++
		s.CreatedAt = createdAtFromName(d.Name())
		return s, true
	}

	applyMeta(&s, meta, d.Name())
	if s.Parsed {
		result.Valid++
	} else {
		result.Invalid++
	}
	return s, true
}

func applyMeta(s *Session, meta parsedMeta, fileName string) {
	s.ThreadID = meta.ThreadID
	s.CWD = meta.CWD
	s.Originator = meta.Originator
	s.CLIVersion = meta.CLIVersion
	s.Source = meta.Source
	s.ModelProvider = meta.ModelProvider
	s.GitBranch = meta.GitBranch
	s.GitSHA = meta.GitSHA
	s.GitOriginURL = meta.GitOriginURL
	s.FirstUserMessage = meta.FirstUserMessage
	s.Preview = meta.Preview
	s.Parsed = meta.FoundSessionMeta

	s.CreatedAt = meta.CreatedAt
	if s.CreatedAt == "" {
		s.CreatedAt = createdAtFromName(fileName)
	}
	if !meta.FoundSessionMeta {
		s.Warnings = append(s.Warnings, "no SessionMeta found; file may be incomplete or a newer format")
	}
}

// isRolloutFile reports whether a filename is a Codex rollout file (plain or
// compressed).
func isRolloutFile(name string) bool {
	if !strings.HasPrefix(name, FilePrefix) {
		return false
	}
	return strings.HasSuffix(name, PlainExt) || strings.HasSuffix(name, CompressedExt)
}

// createdAtFromName extracts the timestamp portion from a rollout filename of
// the form rollout-YYYY-MM-DDThh-mm-ss-<uuid>.jsonl[.zst]. Returns "" if it does
// not match.
func createdAtFromName(name string) string {
	trimmed := strings.TrimPrefix(name, FilePrefix)
	trimmed = strings.TrimSuffix(trimmed, CompressedExt)
	trimmed = strings.TrimSuffix(trimmed, PlainExt)
	// Expect at least "YYYY-MM-DDThh-mm-ss-" followed by a uuid.
	const tsLen = len("2006-01-02T15-04-05")
	if len(trimmed) < tsLen {
		return ""
	}
	return trimmed[:tsLen]
}
