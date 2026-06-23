// Package stats aggregates a set of discovered sessions into a small summary —
// how many, where (by project), and when (by day) — for `cct stats`. It is a
// pure transform over already-scanned sessions: it reads nothing from disk and
// never touches an agent's files or index.
package stats

import (
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// ProjectStat is one project's session count, keyed by recorded cwd.
type ProjectStat struct {
	Path  string
	Count int
}

// DayStat is one calendar day's session count (by last-activity / mtime).
type DayStat struct {
	Day   string // YYYY-MM-DD
	Count int
}

// Stats is the computed summary.
type Stats struct {
	Total      int
	Parsed     int // sessions whose metadata parsed (have a thread id)
	Compressed int
	Archived   int
	NoCWD      int
	TotalBytes int64
	First      time.Time // earliest last-activity
	Last       time.Time // latest last-activity
	Projects   []ProjectStat
	Days       []DayStat
}

// Compute summarizes the given sessions. Project grouping is case-insensitive on
// Windows to match the OS's path semantics; the first-seen spelling is kept.
func Compute(list []sessions.Session) Stats {
	var s Stats
	s.Total = len(list)

	type proj struct {
		spelling string
		count    int
	}
	projByKey := map[string]*proj{}
	dayCount := map[string]int{}

	for _, sess := range list {
		if sess.ThreadID != "" {
			s.Parsed++
		}
		if sess.Compressed {
			s.Compressed++
		}
		if sess.Archived {
			s.Archived++
		}
		s.TotalBytes += sess.SizeBytes

		if !sess.ModTime.IsZero() {
			if s.First.IsZero() || sess.ModTime.Before(s.First) {
				s.First = sess.ModTime
			}
			if sess.ModTime.After(s.Last) {
				s.Last = sess.ModTime
			}
			dayCount[sess.ModTime.Format("2006-01-02")]++
		}

		if sess.CWD == "" {
			s.NoCWD++
			continue
		}
		key := projectKey(sess.CWD)
		if p := projByKey[key]; p != nil {
			p.count++
		} else {
			projByKey[key] = &proj{spelling: sess.CWD, count: 1}
		}
	}

	for _, p := range projByKey {
		s.Projects = append(s.Projects, ProjectStat{Path: p.spelling, Count: p.count})
	}
	// Most active project first; ties broken by path for stable output.
	sort.SliceStable(s.Projects, func(i, j int) bool {
		if s.Projects[i].Count != s.Projects[j].Count {
			return s.Projects[i].Count > s.Projects[j].Count
		}
		return s.Projects[i].Path < s.Projects[j].Path
	})

	for d, c := range dayCount {
		s.Days = append(s.Days, DayStat{Day: d, Count: c})
	}
	sort.SliceStable(s.Days, func(i, j int) bool { return s.Days[i].Day < s.Days[j].Day })

	return s
}

func projectKey(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
