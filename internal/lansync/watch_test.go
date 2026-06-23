package lansync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotAndChange(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "2026", "06", "13")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "rollout-x.jsonl")
	if err := os.WriteFile(f, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-session file must be ignored.
	if err := os.WriteFile(filepath.Join(sub, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s1 := snapshot([]string{root})
	if len(s1) != 1 {
		t.Fatalf("snapshot saw %d session files, want 1", len(s1))
	}
	if snapshotChanged(s1, snapshot([]string{root})) {
		t.Fatal("unchanged tree reported as changed")
	}

	// Grow the file (append) -> size changes -> detected.
	if err := os.WriteFile(f, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !snapshotChanged(s1, snapshot([]string{root})) {
		t.Fatal("append not detected")
	}

	// Add a new session file -> count changes -> detected.
	s2 := snapshot([]string{root})
	g := filepath.Join(sub, "rollout-y.jsonl")
	if err := os.WriteFile(g, []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !snapshotChanged(s2, snapshot([]string{root})) {
		t.Fatal("new file not detected")
	}
}

func TestSnapshotMtimeChange(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "rollout-x.jsonl")
	if err := os.WriteFile(f, []byte("same size!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	s1 := snapshot([]string{root})
	// Same size, newer mtime -> detected.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(f, future, future); err != nil {
		t.Fatal(err)
	}
	if !snapshotChanged(s1, snapshot([]string{root})) {
		t.Fatal("mtime change not detected")
	}
}

func TestSnapshotMissingRootIsEmpty(t *testing.T) {
	s := snapshot([]string{filepath.Join(t.TempDir(), "does-not-exist"), ""})
	if len(s) != 0 {
		t.Fatalf("missing root snapshot = %d entries, want 0", len(s))
	}
}
