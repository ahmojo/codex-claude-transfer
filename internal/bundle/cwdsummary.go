package bundle

import (
	"os"
	"sort"
)

// CWDInfo describes one distinct recorded working directory across a bundle's
// sessions and whether that directory exists on the local machine.
type CWDInfo struct {
	// Path is the first-seen original spelling of the cwd (for display).
	Path string
	// Count is how many sessions recorded this cwd.
	Count int
	// ExistsLocal reports whether the directory exists on this machine. A
	// session whose cwd does not exist locally is hidden in Codex's project
	// sidebar until the folder is created or the cwd is remapped on import.
	ExistsLocal bool
}

// CWDSummary groups a bundle's sessions by their recorded working directory and
// reports which directories are missing on the local machine. It is the data
// behind the "project folders" view that helps users find the #1 portability
// gotcha (imported sessions hidden because their cwd does not exist here).
type CWDSummary struct {
	// Dirs are the distinct non-empty recorded cwds, sorted by path.
	Dirs []CWDInfo
	// UnknownCWD counts sessions with no recorded cwd (e.g. compressed
	// sessions, whose cwd is unknown in v0.1).
	UnknownCWD int
	// MissingCount is how many entries in Dirs do not exist locally.
	MissingCount int
}

// SummarizeCWDs groups sessions by recorded cwd and, for each distinct
// directory, reports the session count and whether it exists locally. Grouping
// is case-insensitive on Windows (via normalizeCWD), matching how cwd is
// compared elsewhere; the first-seen spelling is preserved for display. The
// exists function is injected so the logic is testable without touching the
// filesystem; production callers pass DirExists.
func SummarizeCWDs(sessions []ManifestSession, exists func(string) bool) CWDSummary {
	var summary CWDSummary
	type agg struct {
		path  string // first-seen original spelling
		count int
	}
	byKey := map[string]*agg{}
	var order []string // keys in first-seen order (stable before sort)
	for _, s := range sessions {
		if s.OriginalCWD == "" {
			summary.UnknownCWD++
			continue
		}
		key := normalizeCWD(s.OriginalCWD)
		a, ok := byKey[key]
		if !ok {
			a = &agg{path: s.OriginalCWD}
			byKey[key] = a
			order = append(order, key)
		}
		a.count++
	}

	for _, key := range order {
		a := byKey[key]
		ex := exists(a.path)
		if !ex {
			summary.MissingCount++
		}
		summary.Dirs = append(summary.Dirs, CWDInfo{
			Path:        a.path,
			Count:       a.count,
			ExistsLocal: ex,
		})
	}
	sort.Slice(summary.Dirs, func(i, j int) bool {
		return summary.Dirs[i].Path < summary.Dirs[j].Path
	})
	return summary
}

// DirExists reports whether p exists on the local machine and is a directory.
// It only reads (os.Stat); it never creates anything.
func DirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
