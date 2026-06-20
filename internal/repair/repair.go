// Package repair fixes the modification time of session files that were imported
// with a "wrong" (import-time) mtime, which makes a coding agent's index see them
// as newer than indexed and re-parse them on every open (Codex read-repair), a
// multi-second delay each time.
//
// It resets each affected file's mtime to the latest timestamp recorded INSIDE the
// session (its real last-activity time). It only ever changes file modification
// times — it never edits session content and never touches any index/SQLite — so
// it is safe to run repeatedly and is a no-op for files that are already correct.
package repair

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Options configures a repair pass.
type Options struct {
	// DryRun reports what would change without touching any file.
	DryRun bool
	// MinGap is the minimum amount by which a file's mtime must exceed its
	// content's last timestamp before it is considered worth fixing. It avoids
	// touching native files whose mtime already matches their content.
	MinGap time.Duration
}

// FileResult is the outcome for one fixed (or, in dry-run, would-be-fixed) file.
type FileResult struct {
	Path        string
	ContentTime time.Time // latest timestamp found inside the file (the new mtime)
	OldMtime    time.Time
}

// Result summarizes a repair pass. Fixed counts the candidate files whose mtime
// was reset (or, in DryRun, would be reset).
type Result struct {
	Scanned  int
	Fixed    int
	Files    []FileResult
	Warnings []string
	DryRun   bool
}

// tsRe extracts a top-level "timestamp":"..." value. Rollout/transcript lines
// carry an RFC3339 timestamp near the start of each line; we read only a prefix of
// each line so a huge (e.g. image) line never has to be held in memory.
var tsRe = regexp.MustCompile(`"timestamp"\s*:\s*"([^"]+)"`)

const linePrefixCap = 8 << 10 // bytes of each line scanned for a timestamp

// RepairTimes walks the given directories for plain *.jsonl session files and, for
// each whose on-disk mtime is more than opts.MinGap later than the newest
// timestamp recorded inside it, resets the mtime to that content timestamp.
// Compressed (.jsonl.zst) files are skipped (their content is not read here).
func RepairTimes(dirs []string, opts Options) (Result, error) {
	if opts.MinGap <= 0 {
		opts.MinGap = time.Minute
	}
	result := Result{DryRun: opts.DryRun}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			continue // a missing sessions/projects dir is normal
		}
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("cannot access %s: %v", path, err))
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			result.Scanned++
			fr, warn := repairFile(path, opts)
			if warn != "" {
				result.Warnings = append(result.Warnings, warn)
			}
			if fr == nil {
				return nil // not a candidate (native/already-correct/no timestamp)
			}
			result.Fixed++
			result.Files = append(result.Files, *fr)
			return nil
		})
		if walkErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("walk %s: %v", dir, walkErr))
		}
	}
	return result, nil
}

// repairFile inspects one file. It returns a FileResult only when the file is a
// candidate (mtime later than content by more than MinGap); otherwise nil.
func repairFile(path string, opts Options) (*FileResult, string) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Sprintf("stat %s: %v", path, err)
	}
	contentTime, ok, err := maxContentTime(path)
	if err != nil {
		return nil, fmt.Sprintf("read %s: %v", path, err)
	}
	if !ok {
		return nil, "" // no usable timestamp inside; leave it alone
	}
	mtime := info.ModTime()
	if mtime.Sub(contentTime) <= opts.MinGap {
		return nil, "" // already correct (native file or already repaired)
	}
	fr := &FileResult{Path: path, ContentTime: contentTime, OldMtime: mtime}
	if opts.DryRun {
		return fr, ""
	}
	if err := os.Chtimes(path, contentTime, contentTime); err != nil {
		return nil, fmt.Sprintf("set mtime on %s: %v", path, err)
	}
	return fr, ""
}

// tailWindow is how many trailing bytes maxContentTime reads first. Rollout and
// transcript files are append-only and chronological, so the newest timestamp is
// at the end; reading only the tail avoids scanning huge (image-heavy) files.
const tailWindow = 256 << 10

// maxContentTime returns the latest top-level timestamp recorded in a JSONL file.
// It reads the tail first (where the newest line is) and falls back to a full scan
// only if the tail contained no timestamp.
func maxContentTime(path string) (time.Time, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, false, err
	}
	if info.Size() > tailWindow {
		if _, err := f.Seek(info.Size()-tailWindow, io.SeekStart); err == nil {
			if t, ok := scanForMaxTimestamp(f); ok {
				return t, true, nil
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return time.Time{}, false, err
			}
		}
	}
	t, ok := scanForMaxTimestamp(f)
	return t, ok, nil
}

// scanForMaxTimestamp returns the newest top-level timestamp across the lines in r.
func scanForMaxTimestamp(r io.Reader) (time.Time, bool) {
	br := bufio.NewReaderSize(r, 64<<10)
	var max time.Time
	found := false
	for {
		prefix, readErr := readLinePrefix(br, linePrefixCap)
		if len(prefix) > 0 {
			if m := tsRe.FindSubmatch(prefix); m != nil {
				if t, ok := parseTime(string(m[1])); ok && (!found || t.After(max)) {
					max, found = t, true
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return max, found
}

// readLinePrefix reads up to maxBytes of the current line, then discards the rest
// of the line up to and including the newline. It returns the prefix and a non-nil
// error (io.EOF) once the file is exhausted.
func readLinePrefix(r *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return buf, err
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf) < maxBytes {
			buf = append(buf, b)
		}
		// bytes beyond maxBytes are consumed but dropped
	}
}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
