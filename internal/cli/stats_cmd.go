package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/stats"
)

// runStats prints a summary of the local sessions: totals, busiest projects, and
// recent activity. Read-only; it scans the same way list/search do.
func runStats(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	scan, code := scanForSearch(f, kind, stderr)
	if code != 0 {
		return code
	}
	list := filterForSearch(f, scan.Sessions, stderr)
	s := stats.Compute(list)
	if f.jsonOut {
		printStatsJSON(stdout, kind, s)
		return 0
	}
	printStats(stdout, kind, s)
	return 0
}

func printStats(w io.Writer, kind agent.Kind, s stats.Stats) {
	if s.Total == 0 {
		fmt.Fprintf(w, "No %s sessions found.\n", kind.Label())
		return
	}
	fmt.Fprintf(w, "%s session summary\n\n", kind.Label())
	fmt.Fprintf(w, "  Sessions:     %d\n", s.Total)
	if s.Compressed > 0 {
		fmt.Fprintf(w, "  Compressed:   %d\n", s.Compressed)
	}
	if s.Archived > 0 {
		fmt.Fprintf(w, "  Archived:     %d\n", s.Archived)
	}
	fmt.Fprintf(w, "  On disk:      %s\n", humanBytes(s.TotalBytes))
	if !s.First.IsZero() {
		fmt.Fprintf(w, "  Activity:     %s → %s\n",
			s.First.Format("2006-01-02"), s.Last.Format("2006-01-02"))
	}

	const topN = 8
	if len(s.Projects) > 0 {
		fmt.Fprintln(w, "\nBusiest projects:")
		for i, p := range s.Projects {
			if i >= topN {
				fmt.Fprintf(w, "  … and %s\n", plural(len(s.Projects)-topN, "more project"))
				break
			}
			fmt.Fprintf(w, "  %4d  %s\n", p.Count, safeTerminal(p.Path))
		}
	}
	if s.NoCWD > 0 {
		fmt.Fprintf(w, "  %4d  (no recorded project)\n", s.NoCWD)
	}

	// A compact 14-day activity sparkline of the most recent days with sessions.
	if spark := recentSparkline(s.Days, 14); spark != "" {
		fmt.Fprintf(w, "\nRecent activity (last %d active days):\n  %s\n", min(14, len(s.Days)), spark)
	}
}

// recentSparkline renders up to the last n active days as a unicode bar sparkline.
func recentSparkline(days []stats.DayStat, n int) string {
	if len(days) == 0 {
		return ""
	}
	if len(days) > n {
		days = days[len(days)-n:]
	}
	maxCount := 0
	for _, d := range days {
		if d.Count > maxCount {
			maxCount = d.Count
		}
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, d := range days {
		idx := 0
		if maxCount > 0 {
			idx = (d.Count - 1) * (len(bars) - 1) / max(1, maxCount-1)
			if idx < 0 {
				idx = 0
			}
		}
		b.WriteRune(bars[idx])
	}
	first := days[0].Day
	last := days[len(days)-1].Day
	return fmt.Sprintf("%s   %s … %s (peak %d/day)", b.String(), first, last, maxCount)
}
