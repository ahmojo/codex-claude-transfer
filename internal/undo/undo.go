// Package undo records what an import wrote and reverses it on request, backing
// `cct undo`. Every command that changes session files, including relocate,
// delegates those changes to import so they share one verifiable safety net.
//
// A journal is written per import into cct's own config directory (never inside a
// coding-agent home). Each entry carries the destination path, the SHA-256 of the
// bytes cct wrote, and — for a replaced or merge-updated file — a backup of the
// original. Reversal is conservative: it only removes or restores a file whose
// current contents still match what the import wrote, so edits made after the
// import are never destroyed.
package undo

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// JournalVersion is the on-disk journal schema version written by this cct.
//
// Version 2 added the Removed/Pair entry fields used by Claude Code relocation
// (a transcript that cct deleted from its old project folder after writing the
// relocated copy). Version 1 journals are still understood and reversible, so an
// upgrade never strands a recorded import; an older cct reading a version 2
// journal refuses it outright, which is the safe direction.
const JournalVersion = 2

// minJournalVersion is the oldest schema this cct can still reverse.
const minJournalVersion = 1

// maxJournals bounds how many import journals are kept; older ones are pruned as
// new imports are recorded. Undo only ever reverses the most recent import.
const maxJournals = 25

// Entry is the reversal record for one written file.
type Entry struct {
	// Action mirrors the bundle import action ("import", "import-copy",
	// "replace", "update"); it is informational for display.
	Action string `json:"action"`
	// Dest is the absolute path of the file the import wrote.
	Dest string `json:"dest"`
	// WroteSHA is the SHA-256 of the bytes the import wrote to Dest. Reversal only
	// proceeds when Dest still hashes to this, so later edits are never clobbered.
	WroteSHA string `json:"wrote_sha256"`
	// Backup, when set, is the absolute path to a copy of the file that existed at
	// Dest before the import overwrote it (replace and merge-update). Reversal
	// restores it. Empty for a newly created file, which reversal deletes.
	Backup string `json:"backup,omitempty"`
	// BackupSHA is the SHA-256 of the backup's contents, recorded at import time.
	// Reversal refuses to restore a backup whose bytes no longer match, so a
	// swapped or tampered backup can never be written over a session.
	BackupSHA string `json:"backup_sha256,omitempty"`
	// Created is true when the import created a new file (no prior file at Dest),
	// so reversal deletes it rather than restoring a backup.
	Created bool `json:"created"`
	// Removed is true when cct deleted the file at Dest after copying it to
	// Backup. Claude Code relocation uses it: once the relocated transcript is
	// written under the new project folder, the original under the old folder is
	// removed so the session id is not duplicated. Reversal puts the original back
	// from its hashed backup, and only when nothing else occupies Dest. A removed
	// entry has no written bytes, so WroteSHA is empty. (Journal version 2+.)
	Removed bool `json:"removed,omitempty"`
	// Pair, when set, is the Dest of a Removed entry in the same journal that must
	// be restored before this entry may be deleted. Relocation sets it on the new
	// copy so undo can never delete the copy while the original is still missing.
	// (Journal version 2+.)
	Pair string `json:"pair,omitempty"`
	// ThreadID and Preview are carried for a readable undo listing.
	ThreadID string `json:"thread_id,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// Journal is the full record of one import.
type Journal struct {
	Version int     `json:"version"`
	Time    string  `json:"time"` // RFC3339, when the import ran
	Tool    string  `json:"tool"`
	Home    string  `json:"home"`   // agent home root the import wrote into
	Bundle  string  `json:"bundle"` // source bundle path (for display)
	Entries []Entry `json:"entries"`

	// path is the journal file on disk; set by List/Latest, not serialized.
	path string
}

// Path returns the on-disk location of a journal loaded via List/Latest.
func (j Journal) Path() string { return j.path }

// Dir returns the directory holding import journals under a resolved config dir.
func Dir(configDir string) string { return filepath.Join(configDir, "undo") }

// Record writes a journal for a completed import and prunes old journals to the
// retention limit. A journal with no entries is not written (nothing to undo).
func Record(configDir string, j Journal) (string, error) {
	if len(j.Entries) == 0 {
		return "", nil
	}
	j.Version = JournalVersion
	if j.Time == "" {
		j.Time = time.Now().UTC().Format(time.RFC3339)
	}
	dir := Dir(configDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// A zero-padded nanosecond stamp sorts chronologically (fixed width => lexical
	// order equals numeric order); a random suffix avoids a collision when two
	// imports land in the same clock tick (Windows' wall clock is coarse).
	name := fmt.Sprintf("import-%019d-%s.json", time.Now().UnixNano(), randHex())
	p := filepath.Join(dir, name)
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return "", err
	}
	if err := safety.CopyAtomic(p, bytes.NewReader(data)); err != nil {
		return "", err
	}
	pruneOld(dir)
	return p, nil
}

// List returns the recorded journals, newest first.
func List(configDir string) ([]Journal, error) {
	dir := Dir(configDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []Journal
	for _, n := range names {
		p := filepath.Join(dir, n)
		j, err := load(p)
		if err != nil {
			continue // skip an unreadable/corrupt journal rather than fail undo
		}
		out = append(out, j)
	}
	return out, nil
}

// Latest returns the most recent journal for reversal, or nil when none exist.
//
// It is deliberately strict: it loads the single newest journal file and
// validates it, and if that file is missing fields, points outside its recorded
// home, is truncated, or is otherwise malformed, it returns an error rather than
// silently falling back to an older journal. This upholds the core invariant —
// a corrupt, manipulated, or ambiguous journal must lead to "change nothing".
func Latest(configDir string) (*Journal, error) {
	dir := Dir(configDir)
	names, err := journalNames(dir)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	// journalNames is sorted oldest-first; the newest is the last.
	newest := names[len(names)-1]
	j, err := load(filepath.Join(dir, newest))
	if err != nil {
		return nil, fmt.Errorf("the most recent import journal is unreadable, so undo is refusing to touch anything (inspect %s): %w", filepath.Join(dir, newest), err)
	}
	if err := Validate(j); err != nil {
		return nil, fmt.Errorf("the most recent import journal is invalid, so undo is refusing to touch anything: %w", err)
	}
	return &j, nil
}

// journalNames returns the *.json journal file names in dir, sorted oldest-first
// (fixed-width nanosecond prefix makes lexical order chronological).
func journalNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Validate checks a journal is structurally sound and self-consistent before any
// reversal touches the disk. Every failure mode here resolves to "change
// nothing": an unsupported version, a missing/relative path, a path that escapes
// the recorded agent home, a missing hash, or a created/overwritten record that
// disagrees with its backup fields all reject the whole journal.
func Validate(j Journal) error {
	if j.Version < minJournalVersion || j.Version > JournalVersion {
		return fmt.Errorf("unsupported journal version %d (this cct understands %d-%d)", j.Version, minJournalVersion, JournalVersion)
	}
	if j.Home == "" || !filepath.IsAbs(j.Home) {
		return fmt.Errorf("journal home %q is not an absolute path", j.Home)
	}
	if len(j.Entries) == 0 {
		return fmt.Errorf("journal has no entries")
	}
	// Removed entries are the only valid Pair targets, so collect them first.
	removedDests := map[string]bool{}
	for _, e := range j.Entries {
		if e.Removed {
			removedDests[pathKey(e.Dest)] = true
		}
	}
	for i, e := range j.Entries {
		if e.Dest == "" || !filepath.IsAbs(e.Dest) {
			return fmt.Errorf("entry %d: dest %q is not an absolute path", i, e.Dest)
		}
		if !pathWithin(j.Home, e.Dest) {
			return fmt.Errorf("entry %d: dest %q is outside the recorded home %q", i, e.Dest, j.Home)
		}
		if (e.Removed || e.Pair != "") && j.Version < 2 {
			return fmt.Errorf("entry %d: relocation fields require journal version 2, not %d", i, j.Version)
		}
		if e.Pair != "" {
			if e.Removed {
				return fmt.Errorf("entry %d: a removed file must not depend on a pair", i)
			}
			if !filepath.IsAbs(e.Pair) || !pathWithin(j.Home, e.Pair) {
				return fmt.Errorf("entry %d: pair %q is not an absolute path inside the recorded home", i, e.Pair)
			}
			if !removedDests[pathKey(e.Pair)] {
				return fmt.Errorf("entry %d: pair %q is not a removed file in this journal", i, e.Pair)
			}
		}
		if e.Removed {
			if e.Created {
				return fmt.Errorf("entry %d: a file cannot be both created and removed", i)
			}
			if e.WroteSHA != "" {
				return fmt.Errorf("entry %d: a removed file has no written bytes to hash", i)
			}
		} else if !isHex64(e.WroteSHA) {
			return fmt.Errorf("entry %d: wrote_sha256 is not a valid hash", i)
		}
		if e.Created {
			if e.Backup != "" || e.BackupSHA != "" {
				return fmt.Errorf("entry %d: a created file must not carry a backup", i)
			}
			continue
		}
		// Overwritten or removed file: it must carry a backup, inside the home,
		// with a hash, so reversal can verify the bytes before restoring them.
		if e.Backup == "" || !filepath.IsAbs(e.Backup) {
			return fmt.Errorf("entry %d: overwritten file has no absolute backup path", i)
		}
		if !pathWithin(j.Home, e.Backup) {
			return fmt.Errorf("entry %d: backup %q is outside the recorded home %q", i, e.Backup, j.Home)
		}
		if !isHex64(e.BackupSHA) {
			return fmt.Errorf("entry %d: backup_sha256 is not a valid hash", i)
		}
	}
	return nil
}

// pathKey normalizes a path for comparison, matching the case sensitivity of the
// host filesystem (Windows compares case-insensitively).
func pathKey(p string) string {
	c := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(c)
	}
	return c
}

// pathWithin reports whether target is root itself or lies under it, comparing
// cleaned absolute paths (case-insensitively on Windows, matching the OS).
func pathWithin(root, target string) bool {
	r := filepath.Clean(root)
	t := filepath.Clean(target)
	sep := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		rl, tl := strings.ToLower(r), strings.ToLower(t)
		return tl == rl || strings.HasPrefix(tl, rl+sep)
	}
	return t == r || strings.HasPrefix(t, r+sep)
}

// isHex64 reports whether s is a 64-character lowercase-or-uppercase hex string
// (a SHA-256 digest).
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func load(p string) (Journal, error) {
	var j Journal
	data, err := os.ReadFile(p)
	if err != nil {
		return j, err
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return j, err
	}
	j.path = p
	return j, nil
}

// Outcome is what reversal did to one entry.
type Outcome struct {
	Entry  Entry
	Status string // removed | restored | already-gone | changed | not-regular |
	// backup-missing | backup-changed | occupied | pair-missing | error
	Message string
}

// Result summarizes a reversal.
type Result struct {
	DryRun   bool
	Removed  int
	Restored int
	Skipped  int
	Outcomes []Outcome
}

// Reverse undoes an import journal. It deletes files the import created,
// restores backups for files it overwrote, and puts back files it removed — but
// only when the file on disk still matches what the import wrote, so any change
// made afterward is left untouched and reported as skipped. On a real
// (non-dry-run) reversal, restored backups are removed and the journal file is
// deleted afterward by the caller via Remove.
//
// Removed entries are processed first: a relocated copy that names its original
// via Pair is only deleted once that original is back in place, so a failed or
// skipped restore can never leave the session with no copy at all.
func Reverse(j Journal, dryRun bool) Result {
	res := Result{DryRun: dryRun}
	outcomes := make([]Outcome, len(j.Entries))
	back := map[string]bool{} // originals restored in pass 1

	for i, e := range j.Entries {
		if !e.Removed {
			continue
		}
		o := reverseEntry(e, dryRun)
		outcomes[i] = o
		if o.Status == "restored" {
			back[pathKey(e.Dest)] = true
		}
	}
	for i, e := range j.Entries {
		if e.Removed {
			continue
		}
		if e.Pair != "" && !back[pathKey(e.Pair)] {
			outcomes[i] = Outcome{Entry: e, Status: "pair-missing",
				Message: "the original transcript was not restored, so this copy was kept"}
			continue
		}
		outcomes[i] = reverseEntry(e, dryRun)
	}

	for _, o := range outcomes {
		switch o.Status {
		case "removed":
			res.Removed++
		case "restored":
			res.Restored++
		default:
			res.Skipped++
		}
	}
	res.Outcomes = outcomes
	return res
}

func reverseEntry(e Entry, dryRun bool) Outcome {
	if e.Removed {
		return restoreRemoved(e, dryRun)
	}
	// Dest must still be an ordinary file. If it is gone (moved/deleted) there is
	// nothing to undo; if it was replaced by a symlink, directory, or device, it
	// was tampered with and we refuse to follow it — never delete or write through
	// a symlink a session file was swapped for.
	fi, err := os.Lstat(e.Dest)
	if os.IsNotExist(err) {
		return Outcome{Entry: e, Status: "already-gone", Message: "file no longer present; nothing to undo"}
	}
	if err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}
	}
	if !fi.Mode().IsRegular() {
		return Outcome{Entry: e, Status: "not-regular", Message: "no longer a regular file (symlink/dir?); left as-is"}
	}
	sum, err := sha256File(e.Dest)
	if err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}
	}
	// Guard: only touch a file that still holds exactly what the import wrote.
	if sum != e.WroteSHA {
		return Outcome{Entry: e, Status: "changed", Message: "changed since the import; left as-is"}
	}

	if e.Created {
		if dryRun {
			return Outcome{Entry: e, Status: "removed", Message: "would remove"}
		}
		if err := os.Remove(e.Dest); err != nil {
			return Outcome{Entry: e, Status: "error", Message: err.Error()}
		}
		return Outcome{Entry: e, Status: "removed"}
	}

	// Overwritten file: restore its backup — but only a backup that is still an
	// ordinary file whose bytes match the hash recorded at import time, so a
	// swapped or corrupted backup can never be written over the session.
	if bad, ok := verifyBackup(e); !ok {
		return bad
	}
	if dryRun {
		return Outcome{Entry: e, Status: "restored", Message: "would restore from backup"}
	}
	if err := restore(e.Backup, e.Dest); err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}
	}
	_ = os.Remove(e.Backup)
	return Outcome{Entry: e, Status: "restored"}
}

// restoreRemoved puts back a file cct deleted (a relocated Claude transcript's
// original). It refuses to overwrite anything that occupies the path now, and
// restores only from a backup that is still a regular file with the exact bytes
// recorded at relocation time.
func restoreRemoved(e Entry, dryRun bool) Outcome {
	if _, err := os.Lstat(e.Dest); err == nil {
		return Outcome{Entry: e, Status: "occupied", Message: "a file is back at this path; left as-is"}
	} else if !os.IsNotExist(err) {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}
	}
	if bad, ok := verifyBackup(e); !ok {
		return bad
	}
	if dryRun {
		return Outcome{Entry: e, Status: "restored", Message: "would restore from backup"}
	}
	if err := restore(e.Backup, e.Dest); err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}
	}
	_ = os.Remove(e.Backup)
	return Outcome{Entry: e, Status: "restored"}
}

// verifyBackup checks that an entry's backup is still an ordinary file holding
// exactly the bytes hashed at import time. It returns the refusing Outcome and
// false when the backup cannot be trusted.
func verifyBackup(e Entry) (Outcome, bool) {
	bfi, err := os.Lstat(e.Backup)
	if os.IsNotExist(err) {
		return Outcome{Entry: e, Status: "backup-missing", Message: "backup file is gone; left as-is"}, false
	}
	if err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}, false
	}
	if !bfi.Mode().IsRegular() {
		return Outcome{Entry: e, Status: "backup-changed", Message: "backup is not a regular file; left as-is"}, false
	}
	bsum, err := sha256File(e.Backup)
	if err != nil {
		return Outcome{Entry: e, Status: "error", Message: err.Error()}, false
	}
	if bsum != e.BackupSHA {
		return Outcome{Entry: e, Status: "backup-changed", Message: "backup no longer matches its recorded hash; left as-is"}, false
	}
	return Outcome{}, true
}

// Remove deletes a journal file after it has been reversed.
func Remove(j Journal) error {
	if j.path == "" {
		return nil
	}
	return os.Remove(j.path)
}

func restore(backup, dest string) error {
	f, err := os.Open(backup)
	if err != nil {
		return err
	}
	defer f.Close()
	return safety.CopyAtomic(dest, f)
}

func pruneOld(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	if len(names) <= maxJournals {
		return
	}
	sort.Strings(names) // oldest first
	for _, n := range names[:len(names)-maxJournals] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// randHex returns 8 random hex chars to disambiguate same-tick journal names.
func randHex() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b[:])
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
