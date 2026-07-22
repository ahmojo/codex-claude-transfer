package undo

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// writeFile writes data to a fresh file under dir and returns its path.
func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// hex64 is a syntactically valid SHA-256 digest for journals whose files need
// not exist (Validate checks structure, not the file).
var hex64 = strings.Repeat("ab", 32)

// createdJournal builds a valid single-entry "created file" journal rooted at
// home so Validate accepts it.
func createdJournal(home, bundle string) Journal {
	return Journal{Tool: "codex", Home: home, Bundle: bundle, Entries: []Entry{
		{Action: "import", Dest: filepath.Join(home, "sess.jsonl"), WroteSHA: hex64, Created: true},
	}}
}

func TestRecordAndLatest(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	if _, err := Record(cfg, createdJournal(home, "b.codexbundle")); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := Latest(cfg)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.Bundle != "b.codexbundle" || len(got.Entries) != 1 {
		t.Fatalf("latest = %+v", got)
	}
	if got.Version != JournalVersion {
		t.Errorf("version = %d, want %d", got.Version, JournalVersion)
	}
	if got.Path() == "" {
		t.Errorf("loaded journal has no path")
	}
}

func TestRecordEmptyIsNoop(t *testing.T) {
	cfg := t.TempDir()
	p, err := Record(cfg, Journal{Entries: nil})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if p != "" {
		t.Errorf("empty journal recorded a file: %s", p)
	}
	got, _ := Latest(cfg)
	if got != nil {
		t.Errorf("empty journal should not appear in Latest")
	}
}

func TestLatestPicksNewest(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	for _, name := range []string{"first", "second", "third"} {
		if _, err := Record(cfg, createdJournal(home, name)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Latest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bundle != "third" {
		t.Errorf("latest bundle = %q, want third", got.Bundle)
	}
	all, _ := List(cfg)
	if len(all) != 3 {
		t.Errorf("List returned %d, want 3", len(all))
	}
}

// TestReverseCreatedRemoves confirms a created file is deleted on undo when it
// still matches what the import wrote.
func TestReverseCreatedRemoves(t *testing.T) {
	dir := t.TempDir()
	data := []byte("imported content\n")
	dest := writeFile(t, dir, "new.jsonl", data)
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(data), Created: true}}}

	res := Reverse(j, false)
	if res.Removed != 1 || res.Restored != 0 || res.Skipped != 0 {
		t.Fatalf("counts: removed=%d restored=%d skipped=%d", res.Removed, res.Restored, res.Skipped)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("created file was not removed")
	}
}

// TestReverseCreatedChangedIsKept confirms undo refuses to delete a created file
// that was edited after the import — no data loss.
func TestReverseCreatedChangedIsKept(t *testing.T) {
	dir := t.TempDir()
	orig := []byte("imported content\n")
	dest := writeFile(t, dir, "new.jsonl", orig)
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(orig), Created: true}}}

	// User edits the file after the import.
	edited := []byte("imported content\nplus my new work\n")
	if err := os.WriteFile(dest, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	res := Reverse(j, false)
	if res.Skipped != 1 || res.Removed != 0 {
		t.Fatalf("counts: removed=%d skipped=%d (edited file must be kept)", res.Removed, res.Skipped)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(edited) {
		t.Errorf("edited file was modified by undo")
	}
	if res.Outcomes[0].Status != "changed" {
		t.Errorf("status = %q, want changed", res.Outcomes[0].Status)
	}
}

// TestReverseReplaceRestores confirms an overwritten file is restored from its
// backup, and the backup is removed.
func TestReverseReplaceRestores(t *testing.T) {
	dir := t.TempDir()
	original := []byte("the original local session\n")
	imported := []byte("the bundle version\n")
	dest := writeFile(t, dir, "sess.jsonl", imported) // post-import content on disk
	backup := writeFile(t, dir, "sess.jsonl.cct-bak-1", original)

	j := Journal{Entries: []Entry{{Action: "replace", Dest: dest, WroteSHA: sha(imported), Backup: backup, BackupSHA: sha(original)}}}
	res := Reverse(j, false)
	if res.Restored != 1 {
		t.Fatalf("restored = %d, want 1", res.Restored)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(original) {
		t.Errorf("dest = %q, want the original content restored", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("backup should be removed after restore")
	}
}

func TestReverseDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	data := []byte("imported\n")
	dest := writeFile(t, dir, "new.jsonl", data)
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(data), Created: true}}}

	res := Reverse(j, true)
	if res.Removed != 1 || !res.DryRun {
		t.Fatalf("dry-run removed=%d dryRun=%v", res.Removed, res.DryRun)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dry-run must not remove the file")
	}
}

func TestReverseBackupMissing(t *testing.T) {
	dir := t.TempDir()
	imported := []byte("bundle version\n")
	dest := writeFile(t, dir, "sess.jsonl", imported)
	j := Journal{Entries: []Entry{{Action: "replace", Dest: dest, WroteSHA: sha(imported), Backup: filepath.Join(dir, "gone.bak")}}}

	res := Reverse(j, false)
	if res.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (backup missing)", res.Skipped)
	}
	if res.Outcomes[0].Status != "backup-missing" {
		t.Errorf("status = %q, want backup-missing", res.Outcomes[0].Status)
	}
	// Dest must be left as-is (we could not safely restore).
	got, _ := os.ReadFile(dest)
	if string(got) != string(imported) {
		t.Errorf("dest changed despite missing backup")
	}
}

func TestPruneKeepsRecent(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	for i := 0; i < maxJournals+5; i++ {
		if _, err := Record(cfg, createdJournal(home, "b")); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := List(cfg)
	if len(all) > maxJournals {
		t.Errorf("kept %d journals, want <= %d", len(all), maxJournals)
	}
}
