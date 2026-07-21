// Package undo records what an import wrote and reverses it on request, backing
// `cct undo`. Import is cct's only operation that changes files, so a clean,
// verifiable reversal of the last import is the natural safety net.
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
	"sort"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// JournalVersion is the on-disk journal schema version.
const JournalVersion = 1

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
	// Created is true when the import created a new file (no prior file at Dest),
	// so reversal deletes it rather than restoring a backup.
	Created bool `json:"created"`
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

// Latest returns the most recent journal, or nil when none exist.
func Latest(configDir string) (*Journal, error) {
	js, err := List(configDir)
	if err != nil {
		return nil, err
	}
	if len(js) == 0 {
		return nil, nil
	}
	return &js[0], nil
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
	Entry   Entry
	Status  string // "removed", "restored", "already-gone", "changed", "backup-missing", "error"
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

// Reverse undoes an import journal. It deletes files the import created and
// restores backups for files it overwrote — but only when the file on disk still
// matches what the import wrote, so any change made afterward is left untouched
// and reported as skipped. On a real (non-dry-run) reversal, restored backups are
// removed and the journal file is deleted afterward by the caller via Remove.
func Reverse(j Journal, dryRun bool) Result {
	res := Result{DryRun: dryRun}
	for _, e := range j.Entries {
		o := reverseEntry(e, dryRun)
		switch o.Status {
		case "removed":
			res.Removed++
		case "restored":
			res.Restored++
		default:
			res.Skipped++
		}
		res.Outcomes = append(res.Outcomes, o)
	}
	return res
}

func reverseEntry(e Entry, dryRun bool) Outcome {
	sum, err := sha256File(e.Dest)
	if os.IsNotExist(err) {
		return Outcome{Entry: e, Status: "already-gone", Message: "file no longer present; nothing to undo"}
	}
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

	// Overwritten file: restore its backup.
	if e.Backup == "" {
		return Outcome{Entry: e, Status: "error", Message: "no backup recorded; cannot restore"}
	}
	if _, err := os.Stat(e.Backup); err != nil {
		return Outcome{Entry: e, Status: "backup-missing", Message: "backup file is gone; left as-is"}
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
