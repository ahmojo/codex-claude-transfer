package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/cctpaths"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/undo"
)

// recordUndoJournal writes an undo journal after a successful, non-dry-run
// import so `cct undo` can reverse it. Journaling is best-effort: a failure to
// record never fails the import (the sessions are already safely written), it
// just means that import cannot be undone. It records every file the import
// created, replaced, or merge-updated, with the SHA-256 of the written bytes so
// reversal can refuse to touch a file edited afterward.
func recordUndoJournal(f commonFlags, kind agent.Kind, home codexhome.Home, bundlePath string, res bundle.ImportResult, stderr io.Writer) {
	var entries []undo.Entry
	for _, it := range res.Items {
		dest := absPath(it.DestPath)
		switch it.Action {
		case bundle.ActionImport, bundle.ActionImportCopy:
			if sum, ok := fileSHA(dest); ok {
				entries = append(entries, undo.Entry{
					Action: string(it.Action), Dest: dest, WroteSHA: sum, Created: true,
					ThreadID: threadIDFor(res, it.BundlePath, it.NewThreadID), Preview: previewFor(res, it.BundlePath),
				})
			}
		case bundle.ActionReplace, bundle.ActionUpdate:
			// Both overwrote an existing file and left a backup (replace always;
			// update because RecordUndo was set). Undo can only restore an
			// overwritten file from a hashed backup, so record both the written
			// hash and the backup's hash; if either is unavailable, skip the entry
			// entirely — better no undo for it than an unverifiable one.
			if it.BackupPath == "" {
				continue
			}
			backup := absPath(it.BackupPath)
			wrote, okW := fileSHA(dest)
			bsum, okB := fileSHA(backup)
			if !okW || !okB {
				continue
			}
			entries = append(entries, undo.Entry{
				Action: string(it.Action), Dest: dest, WroteSHA: wrote, Backup: backup, BackupSHA: bsum,
				ThreadID: threadIDFor(res, it.BundlePath, it.NewThreadID), Preview: previewFor(res, it.BundlePath),
			})
		}
	}
	if len(entries) == 0 {
		return
	}
	// ConfigDir honors $CCT_CONFIG_DIR; no explicit override is threaded here.
	dir, err := cctpaths.ConfigDir("")
	if err != nil {
		fmt.Fprintf(stderr, "note: import succeeded but could not locate cct config dir for undo: %v\n", err)
		return
	}
	j := undo.Journal{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Tool:    string(kind),
		Home:    absPath(home.Root),
		Bundle:  bundlePath,
		Entries: entries,
	}
	if _, err := undo.Record(dir, j); err != nil {
		fmt.Fprintf(stderr, "note: import succeeded but recording undo failed: %v\n", err)
	}
}

// absPath returns the absolute form of p, falling back to p if that fails, so the
// undo journal always records absolute, self-consistent paths (undo validates
// that every dest/backup is absolute and inside the recorded home).
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func threadIDFor(res bundle.ImportResult, bundlePath, newID string) string {
	if newID != "" {
		return newID
	}
	for _, ms := range res.Manifest.Sessions {
		if ms.BundlePath == bundlePath {
			return ms.ThreadID
		}
	}
	return ""
}

func previewFor(res bundle.ImportResult, bundlePath string) string {
	for _, ms := range res.Manifest.Sessions {
		if ms.BundlePath == bundlePath {
			return ms.Preview
		}
	}
	return ""
}

func fileSHA(path string) (string, bool) {
	fdata, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer fdata.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fdata); err != nil {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

// runUndo reverses the most recent import, or lists recent imports with --list.
func runUndo(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	dir, err := cctpaths.ConfigDir("")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot locate cct config dir: %v\n", err)
		return 1
	}

	if f.list {
		journals, err := undo.List(dir)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		printUndoList(stdout, journals)
		return 0
	}

	latest, err := undo.Latest(dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if latest == nil {
		fmt.Fprintln(stdout, "Nothing to undo: no import has been recorded on this machine.")
		return 0
	}

	res := undo.Reverse(*latest, f.dryRun)
	printUndoResult(stdout, *latest, res)
	if !f.dryRun {
		if err := undo.Remove(*latest); err != nil {
			fmt.Fprintf(stderr, "note: reversed the import but could not remove its journal: %v\n", err)
		}
	}
	return 0
}

func printUndoList(w io.Writer, journals []undo.Journal) {
	if len(journals) == 0 {
		fmt.Fprintln(w, "No imports have been recorded on this machine.")
		return
	}
	fmt.Fprintln(w, "Recorded imports (newest first):")
	for i, j := range journals {
		created, replaced, updated := 0, 0, 0
		for _, e := range j.Entries {
			switch e.Action {
			case "import", "import-copy":
				created++
			case "replace":
				replaced++
			case "update":
				updated++
			}
		}
		marker := "  "
		if i == 0 {
			marker = "→ " // the one `cct undo` would reverse
		}
		fmt.Fprintf(w, "%s%s  %s  (%s)\n", marker, when(j.Time), agent.Normalize(agent.Kind(j.Tool)).Label(), safeTerminal(j.Bundle))
		fmt.Fprintf(w, "     %d created, %d replaced, %d updated\n", created, replaced, updated)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "`cct undo` reverses the most recent (→). `cct undo --dry-run` previews it.")
}

func printUndoResult(w io.Writer, j undo.Journal, res undo.Result) {
	fmt.Fprintf(w, "Undoing import: %s\n", safeTerminal(j.Bundle))
	fmt.Fprintf(w, "Recorded: %s (%s)\n\n", when(j.Time), agent.Normalize(agent.Kind(j.Tool)).Label())

	for _, o := range res.Outcomes {
		label := o.Entry.ThreadID
		if p := o.Entry.Preview; p != "" {
			label = fmt.Sprintf("%s  %q", short(o.Entry.ThreadID), truncate(p, 48))
		}
		var mark string
		switch o.Status {
		case "removed":
			mark = "removed  "
		case "restored":
			mark = "restored "
		default:
			mark = "skipped  "
		}
		line := fmt.Sprintf("  %s %s", mark, safeTerminal(label))
		if o.Message != "" && o.Status != "removed" && o.Status != "restored" {
			line += fmt.Sprintf("  (%s)", o.Message)
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w)
	verb := "Undo complete"
	if res.DryRun {
		verb = "Dry run"
	}
	fmt.Fprintf(w, "%s: %d removed, %d restored, %d skipped.\n", verb, res.Removed, res.Restored, res.Skipped)
	if res.Skipped > 0 {
		fmt.Fprintln(w, "Skipped files changed since the import (or their backup was gone) and were left untouched.")
	}
	if res.DryRun {
		fmt.Fprintln(w, "Nothing was changed. Re-run without --dry-run to apply.")
		return
	}
	if agent.Normalize(agent.Kind(j.Tool)) == agent.Claude {
		fmt.Fprintln(w, "Restart Claude Code so it re-scans the session files.")
	} else {
		fmt.Fprintln(w, "Restart the Codex App so it re-scans the session files.")
	}
}

// when formats an RFC3339 timestamp for display, falling back to the raw value.
func when(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Local().Format("2006-01-02 15:04")
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
