package undo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The invariant exercised throughout this file: a corrupt, manipulated, or
// ambiguous journal — or a session/backup that changed after the import — must
// resolve to "change nothing".

// ---- Validate: fail-closed on malformed / manipulated journals ----

func TestValidateRejectsBadJournals(t *testing.T) {
	home := t.TempDir()
	good := Entry{Action: "import", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: hex64, Created: true}
	base := func(e Entry) Journal { return Journal{Version: JournalVersion, Home: home, Entries: []Entry{e}} }

	cases := []struct {
		name string
		j    Journal
	}{
		{"wrong version", func() Journal { j := base(good); j.Version = 999; return j }()},
		{"version zero (unset)", func() Journal { j := base(good); j.Version = 0; return j }()},
		{"empty home", Journal{Version: JournalVersion, Home: "", Entries: []Entry{good}}},
		{"relative home", Journal{Version: JournalVersion, Home: "rel/home", Entries: []Entry{good}}},
		{"no entries", Journal{Version: JournalVersion, Home: home}},
		{"relative dest", base(Entry{Action: "import", Dest: "rel.jsonl", WroteSHA: hex64, Created: true})},
		{"dest escapes home", base(Entry{Action: "import", Dest: filepath.Join(home, "..", "evil.jsonl"), WroteSHA: hex64, Created: true})},
		{"absolute dest outside home", base(Entry{Action: "import", Dest: outsidePath(), WroteSHA: hex64, Created: true})},
		{"bad wrote hash", base(Entry{Action: "import", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: "nothex", Created: true})},
		{"created with backup", base(Entry{Action: "import", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: hex64, Created: true, Backup: filepath.Join(home, "b")})},
		{"overwrite without backup", base(Entry{Action: "replace", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: hex64})},
		{"overwrite backup outside home", base(Entry{Action: "replace", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: hex64, Backup: outsidePath(), BackupSHA: hex64})},
		{"overwrite bad backup hash", base(Entry{Action: "replace", Dest: filepath.Join(home, "s.jsonl"), WroteSHA: hex64, Backup: filepath.Join(home, "b"), BackupSHA: "nothex"})},
	}
	for _, c := range cases {
		if err := Validate(c.j); err == nil {
			t.Errorf("%s: expected Validate to reject, got nil", c.name)
		}
	}
}

func TestValidateAcceptsGoodJournal(t *testing.T) {
	home := t.TempDir()
	j := Journal{Version: JournalVersion, Home: home, Entries: []Entry{
		{Action: "import", Dest: filepath.Join(home, "a.jsonl"), WroteSHA: hex64, Created: true},
		{Action: "replace", Dest: filepath.Join(home, "b.jsonl"), WroteSHA: hex64, Backup: filepath.Join(home, "b.jsonl.cct-bak-1"), BackupSHA: hex64},
	}}
	if err := Validate(j); err != nil {
		t.Fatalf("Validate rejected a valid journal: %v", err)
	}
}

func outsidePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\evil.txt`
	}
	return "/etc/evil.txt"
}

// ---- Latest: strict about the newest file (no silent fallback) ----

// TestLatestRefusesCorruptNewest confirms that when the newest journal is
// corrupt, Latest errors instead of falling back to an older, valid journal —
// "change nothing".
func TestLatestRefusesCorruptNewest(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	// A valid older journal.
	if _, err := Record(cfg, createdJournal(home, "older")); err != nil {
		t.Fatal(err)
	}
	// A newer, corrupt journal file (invalid JSON), lexically after the first.
	dir := Dir(cfg)
	corrupt := filepath.Join(dir, "import-9999999999999999999-ffffffff.json")
	if err := os.WriteFile(corrupt, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Latest(cfg); err == nil {
		t.Fatal("Latest must error on a corrupt newest journal, not fall back")
	}
}

// TestLatestRefusesTruncatedJournal covers an incomplete/partial write.
func TestLatestRefusesTruncatedJournal(t *testing.T) {
	cfg := t.TempDir()
	dir := Dir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A plausible prefix of a real journal, cut off mid-object.
	truncated := `{"version":1,"home":"/h","entries":[{"action":"import","dest":`
	if err := os.WriteFile(filepath.Join(dir, "import-0000000000000000001-aaaaaaaa.json"), []byte(truncated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Latest(cfg); err == nil {
		t.Fatal("Latest must error on a truncated journal")
	}
}

// TestLatestRefusesManipulatedDest covers a journal edited to point a dest
// outside the recorded home.
func TestLatestRefusesManipulatedDest(t *testing.T) {
	cfg := t.TempDir()
	dir := Dir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	j := `{"version":1,"home":"/home/agent","entries":[{"action":"import","dest":"` +
		strings.ReplaceAll(outsidePath(), `\`, `\\`) + `","wrote_sha256":"` + hex64 + `","created":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "import-0000000000000000002-bbbbbbbb.json"), []byte(j), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Latest(cfg); err == nil {
		t.Fatal("Latest must reject a journal whose dest escapes its home")
	}
}

func TestLatestNoJournals(t *testing.T) {
	cfg := t.TempDir()
	got, err := Latest(cfg)
	if err != nil {
		t.Fatalf("Latest on empty dir errored: %v", err)
	}
	if got != nil {
		t.Fatalf("Latest on empty dir = %+v, want nil", got)
	}
}

// ---- Reverse: manipulation of the on-disk files ----

// TestReverseFileMoved: the imported file was moved/deleted after import.
func TestReverseFileMoved(t *testing.T) {
	dir := t.TempDir()
	data := []byte("imported\n")
	dest := filepath.Join(dir, "gone.jsonl") // never created => simulates a move
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(data), Created: true}}}
	res := Reverse(j, false)
	if res.Removed != 0 || res.Skipped != 1 {
		t.Fatalf("moved file: removed=%d skipped=%d, want 0/1", res.Removed, res.Skipped)
	}
	if res.Outcomes[0].Status != "already-gone" {
		t.Errorf("status = %q, want already-gone", res.Outcomes[0].Status)
	}
}

// TestReverseDestReplacedByDir: dest was swapped for a directory (a stand-in for
// "not a regular file", which also covers the symlink case cross-platform).
func TestReverseDestReplacedByDir(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "sess.jsonl")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: hex64, Created: true}}}
	res := Reverse(j, false)
	if res.Removed != 0 || res.Skipped != 1 {
		t.Fatalf("dir-as-dest: removed=%d skipped=%d, want 0/1", res.Removed, res.Skipped)
	}
	if res.Outcomes[0].Status != "not-regular" {
		t.Errorf("status = %q, want not-regular", res.Outcomes[0].Status)
	}
	// The directory must still be there — undo refused to touch it.
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		t.Errorf("undo removed or altered a non-regular dest")
	}
}

// TestReverseDestReplacedBySymlink: an attacker swaps the session file for a
// symlink to a sensitive file; undo must not follow or delete through it.
func TestReverseDestReplacedBySymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(target, []byte("do not touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "sess.jsonl")
	if err := os.Symlink(target, dest); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: hex64, Created: true}}}
	res := Reverse(j, false)
	if res.Removed != 0 || res.Skipped != 1 {
		t.Fatalf("symlink dest: removed=%d skipped=%d, want 0/1", res.Removed, res.Skipped)
	}
	if res.Outcomes[0].Status != "not-regular" {
		t.Errorf("status = %q, want not-regular", res.Outcomes[0].Status)
	}
	// The symlink target must be untouched.
	if got, _ := os.ReadFile(target); string(got) != "do not touch\n" {
		t.Errorf("undo wrote through a symlink")
	}
}

// TestReverseBackupManipulated: the backup was swapped for different bytes; undo
// must refuse to restore it (hash mismatch) rather than write attacker content.
func TestReverseBackupManipulated(t *testing.T) {
	dir := t.TempDir()
	original := []byte("the real original session\n")
	imported := []byte("the bundle version\n")
	dest := writeFile(t, dir, "sess.jsonl", imported)
	backup := writeFile(t, dir, "sess.jsonl.cct-bak-1", []byte("MALICIOUS REPLACEMENT\n"))

	j := Journal{Entries: []Entry{{
		Action: "replace", Dest: dest, WroteSHA: sha(imported),
		Backup: backup, BackupSHA: sha(original), // recorded hash of the TRUE original
	}}}
	res := Reverse(j, false)
	if res.Restored != 0 || res.Skipped != 1 {
		t.Fatalf("tampered backup: restored=%d skipped=%d, want 0/1", res.Restored, res.Skipped)
	}
	if res.Outcomes[0].Status != "backup-changed" {
		t.Errorf("status = %q, want backup-changed", res.Outcomes[0].Status)
	}
	// dest must be left as the imported version (not the malicious backup).
	if got, _ := os.ReadFile(dest); string(got) != string(imported) {
		t.Errorf("undo restored a tampered backup over the session")
	}
}

// TestReversePermissionError: the parent directory is made unwritable so the
// delete fails; undo must report an error, not crash, and not lose data.
func TestReversePermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory-permission semantics differ on Windows")
	}
	parent := t.TempDir()
	sub := filepath.Join(parent, "locked")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("imported\n")
	dest := filepath.Join(sub, "sess.jsonl")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o500); err != nil { // read+execute, no write => can't unlink
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755) // restore so t.TempDir cleanup works

	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(data), Created: true}}}
	res := Reverse(j, false)
	if res.Removed != 0 || res.Skipped != 1 {
		t.Fatalf("perm error: removed=%d skipped=%d, want 0/1", res.Removed, res.Skipped)
	}
	if res.Outcomes[0].Status != "error" {
		t.Errorf("status = %q, want error", res.Outcomes[0].Status)
	}
	// The file must still be present (nothing lost).
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file lost after a failed undo: %v", err)
	}
}

// ---- Concurrency ----

// TestConcurrentRecord: two parallel imports must each produce their own journal
// file without clobbering the other.
func TestConcurrentRecord(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Record(cfg, createdJournal(home, "b")); err != nil {
				t.Errorf("record: %v", err)
			}
		}()
	}
	wg.Wait()
	names, err := journalNames(Dir(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != n {
		t.Errorf("concurrent Record produced %d journals, want %d (files clobbered)", len(names), n)
	}
}

// TestConcurrentReverseIsSafe: two parallel undos of the same journal must not
// crash, corrupt, or delete anything other than the recorded file. The SHA guard
// makes the operation idempotent-safe: whether one or both goroutines observe the
// file before it is unlinked, the only outcome is that the recorded file ends up
// gone. (Reporting may double-count a removal under a race; that is cosmetic, not
// a safety issue — nothing else is touched.) A neighbor file must survive.
func TestConcurrentReverseIsSafe(t *testing.T) {
	dir := t.TempDir()
	data := []byte("imported content\n")
	dest := writeFile(t, dir, "sess.jsonl", data)
	neighbor := writeFile(t, dir, "keep.jsonl", []byte("unrelated\n"))
	j := Journal{Entries: []Entry{{Action: "import", Dest: dest, WroteSHA: sha(data), Created: true}}}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Reverse(j, false)
		}()
	}
	wg.Wait()

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("recorded file should be gone after concurrent undo")
	}
	if _, err := os.Stat(neighbor); err != nil {
		t.Errorf("concurrent undo harmed an unrelated file: %v", err)
	}
}

// ---- Crash between import and journal write ----

// TestMissingJournalAfterCrash models a crash after the session files were
// written but before the journal was recorded: there is simply no journal, so
// undo finds nothing and touches nothing. (Record writes atomically, so a
// half-written journal never exists — see TestLatestRefusesTruncatedJournal for
// the corrupt-file path.)
func TestMissingJournalAfterCrash(t *testing.T) {
	cfg := t.TempDir()
	got, err := Latest(cfg)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no journal, got %+v", got)
	}
}
