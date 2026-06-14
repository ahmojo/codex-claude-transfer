package bundle

import (
	"runtime"
	"testing"
)

// existsSet returns an exists-func that reports true only for the given paths.
func existsSet(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func TestSummarizeCWDs_GroupsCountsAndExistence(t *testing.T) {
	sessions := []ManifestSession{
		{OriginalCWD: "/home/a/project"},
		{OriginalCWD: "/home/a/project"},
		{OriginalCWD: "/home/b/other"},
		{OriginalCWD: ""}, // unknown (e.g. compressed)
		{OriginalCWD: ""},
	}
	got := SummarizeCWDs(sessions, existsSet("/home/a/project"))

	if got.UnknownCWD != 2 {
		t.Errorf("UnknownCWD = %d, want 2", got.UnknownCWD)
	}
	if len(got.Dirs) != 2 {
		t.Fatalf("len(Dirs) = %d, want 2: %+v", len(got.Dirs), got.Dirs)
	}
	// Sorted by path: /home/a/project before /home/b/other.
	if got.Dirs[0].Path != "/home/a/project" {
		t.Errorf("Dirs[0].Path = %q, want /home/a/project", got.Dirs[0].Path)
	}
	if got.Dirs[0].Count != 2 {
		t.Errorf("Dirs[0].Count = %d, want 2", got.Dirs[0].Count)
	}
	if !got.Dirs[0].ExistsLocal {
		t.Errorf("Dirs[0].ExistsLocal = false, want true")
	}
	if got.Dirs[1].Path != "/home/b/other" {
		t.Errorf("Dirs[1].Path = %q, want /home/b/other", got.Dirs[1].Path)
	}
	if got.Dirs[1].ExistsLocal {
		t.Errorf("Dirs[1].ExistsLocal = true, want false (not in exists set)")
	}
	if got.MissingCount != 1 {
		t.Errorf("MissingCount = %d, want 1", got.MissingCount)
	}
}

func TestSummarizeCWDs_Empty(t *testing.T) {
	got := SummarizeCWDs(nil, existsSet())
	if len(got.Dirs) != 0 || got.UnknownCWD != 0 || got.MissingCount != 0 {
		t.Errorf("expected zero summary, got %+v", got)
	}
}

func TestSummarizeCWDs_AllMissing(t *testing.T) {
	sessions := []ManifestSession{
		{OriginalCWD: "/x"},
		{OriginalCWD: "/y"},
	}
	got := SummarizeCWDs(sessions, existsSet()) // nothing exists
	if got.MissingCount != 2 {
		t.Errorf("MissingCount = %d, want 2", got.MissingCount)
	}
	for _, d := range got.Dirs {
		if d.ExistsLocal {
			t.Errorf("%q ExistsLocal = true, want false", d.Path)
		}
	}
}

func TestSummarizeCWDs_FirstSeenSpellingPreserved(t *testing.T) {
	// On Windows, grouping is case-insensitive so these collapse to one dir and
	// the first-seen spelling is kept; elsewhere they are distinct.
	sessions := []ManifestSession{
		{OriginalCWD: `C:\Users\Me\Proj`},
		{OriginalCWD: `c:\users\me\proj`},
	}
	got := SummarizeCWDs(sessions, existsSet())
	if runtime.GOOS == "windows" {
		if len(got.Dirs) != 1 {
			t.Fatalf("windows: len(Dirs) = %d, want 1 (case-insensitive grouping)", len(got.Dirs))
		}
		if got.Dirs[0].Count != 2 {
			t.Errorf("windows: Count = %d, want 2", got.Dirs[0].Count)
		}
		if got.Dirs[0].Path != `C:\Users\Me\Proj` {
			t.Errorf("windows: Path = %q, want first-seen spelling C:\\Users\\Me\\Proj", got.Dirs[0].Path)
		}
	} else {
		if len(got.Dirs) != 2 {
			t.Fatalf("non-windows: len(Dirs) = %d, want 2 (case-sensitive)", len(got.Dirs))
		}
	}
}
