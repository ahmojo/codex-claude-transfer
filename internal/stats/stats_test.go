package stats

import (
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestComputeAggregates(t *testing.T) {
	list := []sessions.Session{
		{ThreadID: "a", CWD: "/home/u/projA", SizeBytes: 100, ModTime: day("2026-06-01")},
		{ThreadID: "b", CWD: "/home/u/projA", SizeBytes: 200, ModTime: day("2026-06-02")},
		{ThreadID: "c", CWD: "/home/u/projB", SizeBytes: 50, ModTime: day("2026-06-02"), Compressed: true},
		{ThreadID: "", CWD: "", SizeBytes: 10, ModTime: day("2026-06-03"), Archived: true},
	}
	s := Compute(list)

	if s.Total != 4 || s.Parsed != 3 || s.Compressed != 1 || s.Archived != 1 || s.NoCWD != 1 {
		t.Fatalf("counts wrong: %+v", s)
	}
	if s.TotalBytes != 360 {
		t.Fatalf("TotalBytes = %d", s.TotalBytes)
	}
	if !s.First.Equal(day("2026-06-01")) || !s.Last.Equal(day("2026-06-03")) {
		t.Fatalf("First/Last = %v / %v", s.First, s.Last)
	}
	if len(s.Projects) != 2 || s.Projects[0].Path != "/home/u/projA" || s.Projects[0].Count != 2 {
		t.Fatalf("projects wrong: %+v", s.Projects)
	}
	// Days sorted ascending with correct counts.
	if len(s.Days) != 3 || s.Days[1].Day != "2026-06-02" || s.Days[1].Count != 2 {
		t.Fatalf("days wrong: %+v", s.Days)
	}
}

func TestComputeEmpty(t *testing.T) {
	s := Compute(nil)
	if s.Total != 0 || len(s.Projects) != 0 || len(s.Days) != 0 || !s.First.IsZero() {
		t.Fatalf("empty stats not zero: %+v", s)
	}
}
