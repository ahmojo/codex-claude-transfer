package undo

import (
	"os"
	"path/filepath"
	"testing"
)

// A Claude relocation records two entries per transcript: the copy written under
// the new project folder (created, paired with the original) and the original
// that was removed from the old folder (restorable from a hashed backup). The
// invariant tested here: undo restores the original first, and never deletes the
// copy while the original is still missing.

// relocationJournal builds the pair of entries a Claude relocation records.
func relocationJournal(t *testing.T, home string) (j Journal, oldPath, newPath, backup string) {
	t.Helper()
	original := []byte(`{"cwd":"/old/project","sessionId":"s1"}` + "\n")
	relocated := []byte(`{"cwd":"/new/project","sessionId":"s1"}` + "\n")

	oldDir := filepath.Join(home, "projects", "-old-project")
	newDir := filepath.Join(home, "projects", "-new-project")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldPath = filepath.Join(oldDir, "s1.jsonl")
	newPath = filepath.Join(newDir, "s1.jsonl")
	backup = oldPath + ".cct-bak-1"

	// After a successful relocation: the copy exists, the original is gone, and
	// its bytes live in the backup.
	if err := os.WriteFile(newPath, relocated, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, original, 0o644); err != nil {
		t.Fatal(err)
	}

	j = Journal{Version: JournalVersion, Tool: "claude", Home: home, Entries: []Entry{
		{Action: "relocate-remove", Dest: oldPath, Backup: backup, BackupSHA: sha(original), Removed: true},
		{Action: "import", Dest: newPath, WroteSHA: sha(relocated), Created: true, Pair: oldPath},
	}}
	return j, oldPath, newPath, backup
}

func TestReverseRelocationRestoresOriginalAndRemovesCopy(t *testing.T) {
	home := t.TempDir()
	j, oldPath, newPath, backup := relocationJournal(t, home)

	res := Reverse(j, false)
	if res.Restored != 1 || res.Removed != 1 || res.Skipped != 0 {
		t.Fatalf("restored=%d removed=%d skipped=%d, want 1/1/0", res.Restored, res.Removed, res.Skipped)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("original transcript was not restored: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("relocated copy still present after undo: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup was not cleaned up after a successful restore: %v", err)
	}
}

func TestReverseRelocationDryRunChangesNothing(t *testing.T) {
	home := t.TempDir()
	j, oldPath, newPath, _ := relocationJournal(t, home)

	res := Reverse(j, true)
	if res.Restored != 1 || res.Removed != 1 {
		t.Fatalf("dry-run restored=%d removed=%d, want 1/1", res.Restored, res.Removed)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run recreated the original: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("dry-run removed the relocated copy: %v", err)
	}
}

// TestReverseRelocationKeepsCopyWhenBackupIsGone is the core safety property: if
// the original cannot be restored, the only remaining copy of the session must
// survive.
func TestReverseRelocationKeepsCopyWhenBackupIsGone(t *testing.T) {
	home := t.TempDir()
	j, oldPath, newPath, backup := relocationJournal(t, home)
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}

	res := Reverse(j, false)
	if res.Restored != 0 || res.Removed != 0 || res.Skipped != 2 {
		t.Fatalf("restored=%d removed=%d skipped=%d, want 0/0/2", res.Restored, res.Removed, res.Skipped)
	}
	if res.Outcomes[0].Status != "backup-missing" {
		t.Errorf("removed-entry status = %q, want backup-missing", res.Outcomes[0].Status)
	}
	if res.Outcomes[1].Status != "pair-missing" {
		t.Errorf("copy status = %q, want pair-missing", res.Outcomes[1].Status)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("undo deleted the only remaining copy of the session: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("original reappeared without a backup: %v", err)
	}
}

// TestReverseRelocationKeepsCopyWhenBackupTampered covers a swapped backup: the
// hash guard refuses the restore, so the copy stays put too.
func TestReverseRelocationKeepsCopyWhenBackupTampered(t *testing.T) {
	home := t.TempDir()
	j, _, newPath, backup := relocationJournal(t, home)
	if err := os.WriteFile(backup, []byte("MALICIOUS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Reverse(j, false)
	if res.Outcomes[0].Status != "backup-changed" {
		t.Errorf("removed-entry status = %q, want backup-changed", res.Outcomes[0].Status)
	}
	if res.Outcomes[1].Status != "pair-missing" {
		t.Errorf("copy status = %q, want pair-missing", res.Outcomes[1].Status)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("undo deleted the copy after refusing a tampered backup: %v", err)
	}
}

// TestReverseRelocationRefusesOccupiedOriginal covers a file reappearing at the
// old path (a new session, or a re-relocation): undo must not overwrite it.
func TestReverseRelocationRefusesOccupiedOriginal(t *testing.T) {
	home := t.TempDir()
	j, oldPath, newPath, _ := relocationJournal(t, home)
	squatter := []byte("a different session wrote here\n")
	if err := os.WriteFile(oldPath, squatter, 0o644); err != nil {
		t.Fatal(err)
	}

	res := Reverse(j, false)
	if res.Outcomes[0].Status != "occupied" {
		t.Errorf("removed-entry status = %q, want occupied", res.Outcomes[0].Status)
	}
	if got, _ := os.ReadFile(oldPath); string(got) != string(squatter) {
		t.Error("undo overwrote a file that reappeared at the original path")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("undo deleted the copy without restoring the original: %v", err)
	}
}

// TestReverseRelocationKeepsEditedCopy: the relocated transcript was appended to
// after the relocation, so the copy is left alone even though the original is
// restored — nothing the user wrote afterward is destroyed.
func TestReverseRelocationKeepsEditedCopy(t *testing.T) {
	home := t.TempDir()
	j, oldPath, newPath, _ := relocationJournal(t, home)
	f, err := os.OpenFile(newPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"cwd":"/new/project","type":"user"}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	res := Reverse(j, false)
	if res.Restored != 1 || res.Removed != 0 || res.Skipped != 1 {
		t.Fatalf("restored=%d removed=%d skipped=%d, want 1/0/1", res.Restored, res.Removed, res.Skipped)
	}
	if res.Outcomes[1].Status != "changed" {
		t.Errorf("copy status = %q, want changed", res.Outcomes[1].Status)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("undo deleted an edited copy: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("original was not restored: %v", err)
	}
}

// TestValidateRejectsBadRelocationEntries keeps the fail-closed rules explicit.
func TestValidateRejectsBadRelocationEntries(t *testing.T) {
	home := t.TempDir()
	removed := Entry{Action: "relocate-remove", Dest: filepath.Join(home, "old.jsonl"),
		Backup: filepath.Join(home, "old.jsonl.cct-bak-1"), BackupSHA: hex64, Removed: true}
	base := func(entries ...Entry) Journal {
		return Journal{Version: JournalVersion, Home: home, Entries: entries}
	}

	cases := []struct {
		name string
		j    Journal
	}{
		{"removed with a written hash", base(func() Entry { e := removed; e.WroteSHA = hex64; return e }())},
		{"removed and created at once", base(func() Entry { e := removed; e.Created = true; return e }())},
		{"removed without a backup", base(func() Entry { e := removed; e.Backup, e.BackupSHA = "", ""; return e }())},
		{"removed with a bad backup hash", base(func() Entry { e := removed; e.BackupSHA = "nothex"; return e }())},
		{"removed depending on a pair", base(func() Entry { e := removed; e.Pair = filepath.Join(home, "x.jsonl"); return e }())},
		{"pair that is not in the journal", base(removed, Entry{
			Action: "import", Dest: filepath.Join(home, "new.jsonl"), WroteSHA: hex64, Created: true,
			Pair: filepath.Join(home, "not-recorded.jsonl"),
		})},
		{"pair outside the home", base(removed, Entry{
			Action: "import", Dest: filepath.Join(home, "new.jsonl"), WroteSHA: hex64, Created: true,
			Pair: outsidePath(),
		})},
		{"relocation fields in a version 1 journal", func() Journal {
			j := base(removed)
			j.Version = 1
			return j
		}()},
	}
	for _, c := range cases {
		if err := Validate(c.j); err == nil {
			t.Errorf("%s: expected Validate to reject, got nil", c.name)
		}
	}

	valid := base(removed, Entry{Action: "import", Dest: filepath.Join(home, "new.jsonl"),
		WroteSHA: hex64, Created: true, Pair: removed.Dest})
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate rejected a valid relocation journal: %v", err)
	}
}

// TestValidateStillAcceptsVersion1 keeps older journals reversible after an
// upgrade: a v1 journal without relocation fields must remain valid.
func TestValidateStillAcceptsVersion1(t *testing.T) {
	home := t.TempDir()
	j := Journal{Version: 1, Home: home, Entries: []Entry{
		{Action: "import", Dest: filepath.Join(home, "a.jsonl"), WroteSHA: hex64, Created: true},
	}}
	if err := Validate(j); err != nil {
		t.Fatalf("Validate rejected a version 1 journal: %v", err)
	}
}
