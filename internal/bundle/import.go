package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// Action describes what import decided to do with one bundle entry.
type Action string

const (
	ActionImport         Action = "import"           // new file; copied (unless dry-run)
	ActionSkipIdentical  Action = "skip-identical"   // target exists with same checksum
	ActionConflict       Action = "conflict"         // target exists with different checksum; not overwritten
	ActionReplace        Action = "replace"          // conflict, overwritten after backing up the local file
	ActionImportCopy     Action = "import-copy"      // conflict, imported as a brand-new session (fresh id + filename)
	ActionSkipArchived   Action = "skip-archived"    // archived_sessions entry; not imported in v0.1
	ActionSkipNonSession Action = "skip-non-session" // unexpected non-session file
	ActionSkipDeselected Action = "skip-deselected"  // not among the --session ids requested for import
)

// ImportItem is the plan/outcome for a single bundle entry.
type ImportItem struct {
	BundlePath  string
	DestPath    string
	Action      Action
	OriginalCWD string
	CWDMismatch bool
	// Mapped reports whether --map-cwd rewrote this session's cwd.
	Mapped bool
	// Copied reports whether this entry was imported as a brand-new session
	// (ActionImportCopy, --import-as-copy) with NewThreadID as its fresh id.
	Copied bool
	// NewThreadID is the freshly assigned session id when Copied is true.
	NewThreadID string
	// BackupPath, when non-empty, is where the pre-existing local file was
	// backed up before being replaced (ActionReplace, --replace-with-backup).
	BackupPath string
	// content, when non-nil, is the (cwd-mapped) bytes to write instead of
	// streaming the entry verbatim from the bundle.
	content []byte
}

// ImportOptions configures an import.
type ImportOptions struct {
	BundlePath string
	DryRun     bool
	// ProjectPath, when non-empty, enables per-session cwd-mismatch checks
	// against this (already absolute) path.
	ProjectPath string
	// MapCWD, when set, rewrites matching sessions' recorded cwd on import.
	// Only plain .jsonl files are rewritten; .jsonl.zst files that match a
	// mapping are copied byte-for-byte and reported as unmappable.
	MapCWD []CWDMapping
	// ReplaceWithBackup turns conflicts (a local file with different content
	// for the same session) into a replace: the local file is backed up next
	// to itself and then overwritten with the bundle's version. Without it,
	// conflicts are reported and skipped (the default, never-overwrite behavior).
	ReplaceWithBackup bool
	// SessionIDs, when non-empty, restricts the import to the bundle sessions
	// whose thread id equals (or uniquely begins with) one of these values; every
	// other session in the bundle is skipped (ActionSkipDeselected). An id that
	// matches no session in the bundle is an error (nothing is written).
	SessionIDs []string
	// ImportAsCopy turns conflicts into a brand-new session: the bundle's
	// version is assigned a fresh session id and written under a new rollout
	// filename, so it coexists with the diverged local session rather than being
	// skipped or replacing it. Only plain .jsonl files can be copied; a
	// compressed (.jsonl.zst) conflict, or one without a session_meta id, stays a
	// skipped conflict. Mutually exclusive with ReplaceWithBackup at the CLI.
	ImportAsCopy bool
}

// ImportResult summarizes an import.
type ImportResult struct {
	Manifest         Manifest
	Items            []ImportItem
	Imported         int
	SkippedIdentical int
	Conflicts        int
	SkippedOther     int
	// SkippedDeselected counts bundle sessions excluded by a --session filter.
	SkippedDeselected int
	CWDMismatchCount  int
	// Replaced counts conflicting sessions that were overwritten after the local
	// file was backed up (--replace-with-backup).
	Replaced int
	// ImportedCopies counts conflicting sessions imported as brand-new sessions
	// (--import-as-copy).
	ImportedCopies int
	// Mapped counts imported sessions whose cwd was rewritten by --map-cwd.
	Mapped int
	// MappedCompressedSkipped counts compressed sessions that matched a
	// mapping but could not be rewritten in v0.1 (copied byte-for-byte).
	MappedCompressedSkipped int
	ProjectProvided         bool
	DryRun                  bool
	Warnings                []string
}

// Import validates a bundle end-to-end and, unless DryRun is set, copies its
// rollout files into the Codex home, preserving the sessions/YYYY/MM/DD layout.
//
// Safety guarantees:
//   - The whole bundle's checksums are verified BEFORE anything is written.
//   - Unsafe entry paths (absolute, drive-letter, zip-slip) abort the import.
//   - Only sessions/YYYY/MM/DD rollout files are imported.
//   - Existing files are never overwritten: identical files are skipped,
//     differing files are reported as conflicts and skipped.
//   - Writes are atomic (temp file + rename). SQLite is never touched.
//   - .jsonl.zst files are copied byte-for-byte; never parsed or decompressed.
func Import(home codexhome.Home, opts ImportOptions) (ImportResult, error) {
	result := ImportResult{DryRun: opts.DryRun, ProjectProvided: opts.ProjectPath != ""}

	zr, err := zip.OpenReader(opts.BundlePath)
	if err != nil {
		return result, fmt.Errorf("open bundle: %w", err)
	}
	defer zr.Close()

	manifest, checksums, err := readMeta(&zr.Reader)
	if err != nil {
		return result, err
	}
	if err := validateManifest(manifest); err != nil {
		return result, err
	}
	result.Manifest = manifest

	// 1) Verify integrity of the entire bundle before writing anything.
	if err := verifyBundle(&zr.Reader, checksums); err != nil {
		return result, err
	}

	// 2) Build the per-entry plan.
	cwdByBundlePath := map[string]string{}
	for _, ms := range manifest.Sessions {
		cwdByBundlePath[ms.BundlePath] = ms.OriginalCWD
	}

	// Resolve a --session filter (if any) to the exact set of bundle paths it
	// selects, erroring before any write if a requested id matches nothing.
	selectedPaths, err := resolveSelectedPaths(manifest, opts.SessionIDs)
	if err != nil {
		return result, err
	}

	for _, f := range zr.File {
		if f.Name == ManifestName || f.Name == ChecksumsName {
			continue
		}
		// Paths were already validated as safe in verifyBundle.
		rel := f.Name
		if !safety.IsSessionEntry(rel) {
			action := ActionSkipNonSession
			if isArchivedEntry(rel) {
				action = ActionSkipArchived
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: archived sessions are not imported in v0.1; skipped", rel))
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: unexpected non-session file; skipped", rel))
			}
			result.Items = append(result.Items, ImportItem{BundlePath: rel, Action: action})
			result.SkippedOther++
			continue
		}

		// Apply the --session filter: a session not requested is skipped here,
		// before any conflict/checksum/mapping work.
		if selectedPaths != nil && !selectedPaths[rel] {
			result.Items = append(result.Items, ImportItem{BundlePath: rel, Action: ActionSkipDeselected})
			result.SkippedDeselected++
			continue
		}

		dest, err := safety.DestPath(home.Root, rel)
		if err != nil {
			return result, err
		}

		item := ImportItem{
			BundlePath:  rel,
			DestPath:    dest,
			OriginalCWD: cwdByBundlePath[rel],
		}
		if opts.ProjectPath != "" && item.OriginalCWD != "" && !pathEqual(item.OriginalCWD, opts.ProjectPath) {
			item.CWDMismatch = true
			result.CWDMismatchCount++
		}

		// The effective checksum is the bundle's checksum unless --map-cwd
		// rewrites this file, in which case it is the checksum of the rewritten
		// bytes. Never use the bundle checksum as the target checksum after a
		// mutation.
		effectiveSum := checksums[rel]
		if m := matchMapping(item.OriginalCWD, opts.MapCWD); m != nil {
			if strings.HasSuffix(rel, compressedSessionSuffix) {
				mapped, changed, available, err := remapCompressed(&zr.Reader, rel, m)
				if err != nil {
					return result, err
				}
				switch {
				case mapped != nil:
					item.Mapped = true
					item.content = mapped
					effectiveSum = sha256Hex(mapped)
				case !available:
					result.MappedCompressedSkipped++
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s: compressed session cannot be cwd-mapped without the 'zstd' tool; copied byte-for-byte", rel))
				case !changed:
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s: recorded cwd did not match the mapping; not rewritten", rel))
				}
			} else {
				orig, err := readEntryBytes(&zr.Reader, rel)
				if err != nil {
					return result, err
				}
				mapped, changed, err := rewriteSessionMetaCWD(orig, m.Old, m.New)
				if err != nil {
					return result, fmt.Errorf("map cwd for %s: %w", rel, err)
				}
				if changed {
					if err := validateMappedJSONL(orig, mapped, m.New); err != nil {
						return result, fmt.Errorf("mapped %s failed validation: %w", rel, err)
					}
					item.Mapped = true
					item.content = mapped
					effectiveSum = sha256Hex(mapped)
				} else {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("%s: recorded cwd did not match the mapping; not rewritten", rel))
				}
			}
		}

		action, err := decideAction(dest, effectiveSum)
		if err != nil {
			return result, err
		}
		// A conflict can be resolved in one of two opt-in ways. With
		// --replace-with-backup the local file is preserved as a backup and then
		// overwritten (write phase below). With --import-as-copy the bundle's
		// version is imported as a brand-new session, leaving the local file
		// untouched. The CLI keeps the two flags mutually exclusive.
		if action == ActionConflict && opts.ReplaceWithBackup {
			action = ActionReplace
		} else if action == ActionConflict && opts.ImportAsCopy {
			action, err = planImportCopy(&zr.Reader, &item, rel, home.Root, &result)
			if err != nil {
				return result, err
			}
		}
		item.Action = action
		switch action {
		case ActionImport:
			result.Imported++
			if item.Mapped {
				result.Mapped++
			}
		case ActionImportCopy:
			result.ImportedCopies++
			if item.Mapped {
				result.Mapped++
			}
		case ActionReplace:
			result.Replaced++
			if item.Mapped {
				result.Mapped++
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: target exists with different content; the local file will be backed up and replaced", rel))
		case ActionSkipIdentical:
			result.SkippedIdentical++
		case ActionConflict:
			result.Conflicts++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: target exists with different content; skipped (conflict). Use --replace-with-backup to overwrite while keeping a backup of the local file", rel))
		}
		result.Items = append(result.Items, item)
	}

	if !result.ProjectProvided && (result.Imported > 0 || result.ImportedCopies > 0) {
		result.Warnings = append(result.Warnings,
			"no --project given: whether imported sessions show in a project's sidebar depends on Codex's cwd filtering; if the project path differs from the source device they may be hidden from that project view")
	}

	// 3) Perform copies (unless dry-run).
	if opts.DryRun {
		return result, nil
	}
	for i := range result.Items {
		item := &result.Items[i]
		if item.Action != ActionImport && item.Action != ActionReplace && item.Action != ActionImportCopy {
			continue
		}
		// For a replace, back up the existing local file first so nothing is
		// lost. The backup keeps a suffix that does not match Codex's rollout
		// pattern, so Codex ignores it on its next scan.
		if item.Action == ActionReplace {
			backup, err := backupFile(item.DestPath)
			if err != nil {
				return result, fmt.Errorf("back up %s before replacing: %w", item.BundlePath, err)
			}
			item.BackupPath = backup
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: backed up local file to %s", item.BundlePath, backup))
		}
		if item.content != nil {
			if err := safety.CopyAtomic(item.DestPath, bytes.NewReader(item.content)); err != nil {
				return result, fmt.Errorf("import %s: %w", item.BundlePath, err)
			}
			continue
		}
		if err := copyEntry(&zr.Reader, item.BundlePath, item.DestPath); err != nil {
			return result, fmt.Errorf("import %s: %w", item.BundlePath, err)
		}
	}
	return result, nil
}

// backupFile copies an existing file to a sibling backup path before it is
// overwritten. The backup name ends in ".codexsync-bak-<unix-nanos>", which does
// not match Codex's rollout-*.jsonl pattern, so Codex will not treat the backup
// as a session. A fresh, non-existing name is chosen to avoid clobbering a prior
// backup.
func backupFile(dest string) (string, error) {
	base := fmt.Sprintf("%s.codexsync-bak-%d", dest, time.Now().UnixNano())
	backup := base
	for n := 1; ; n++ {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		backup = fmt.Sprintf("%s.%d", base, n)
	}
	src, err := os.Open(dest)
	if err != nil {
		return "", err
	}
	defer src.Close()
	if err := safety.CopyAtomic(backup, src); err != nil {
		return "", err
	}
	return backup, nil
}

// planImportCopy turns a conflict into an import-as-copy when possible. It
// assigns a fresh session id to the bundle's (plain .jsonl) version, derives a
// new, non-colliding rollout destination from that id, validates the rewrite,
// and stages the new bytes/destination on item. It returns ActionImportCopy on
// success, or leaves the entry a skipped ActionConflict (with an explanatory
// warning) when the session cannot be safely copied — a compressed file or one
// without a session_meta id. A hard error is only returned for internal/IO
// failures, in which case the whole import aborts before writing anything.
func planImportCopy(zr *zip.Reader, item *ImportItem, rel, root string, result *ImportResult) (Action, error) {
	if strings.HasSuffix(rel, compressedSessionSuffix) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: compressed session cannot be imported as a copy in v0.1; skipped (conflict)", rel))
		return ActionConflict, nil
	}
	base := item.content // may already be cwd-mapped bytes
	if base == nil {
		b, err := readEntryBytes(zr, rel)
		if err != nil {
			return "", err
		}
		base = b
	}
	// Try a few fresh ids in the astronomically unlikely event a destination
	// already exists; we never overwrite when importing as a copy.
	for attempt := 0; attempt < 5; attempt++ {
		newID, err := newSessionID()
		if err != nil {
			return "", err
		}
		copied, oldID, changed, err := rewriteSessionMetaID(base, newID)
		if err != nil {
			return "", fmt.Errorf("assign new session id for %s: %w", rel, err)
		}
		if !changed {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: no session_meta id to reassign; cannot import as a copy; skipped (conflict)", rel))
			return ActionConflict, nil
		}
		if err := validateCopiedJSONL(base, copied, newID); err != nil {
			return "", fmt.Errorf("copied %s failed validation: %w", rel, err)
		}
		newRel := copyDestRel(rel, oldID, newID)
		if !safety.IsSessionEntry(newRel) {
			return "", fmt.Errorf("internal: copy destination %q is not a valid session path", newRel)
		}
		newDest, err := safety.DestPath(root, newRel)
		if err != nil {
			return "", err
		}
		if _, statErr := os.Stat(newDest); statErr == nil {
			continue // destination taken; regenerate
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		item.content = copied
		item.DestPath = newDest
		item.Copied = true
		item.NewThreadID = newID
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: target exists with different content; importing as a new session (id %s)", rel, newID))
		return ActionImportCopy, nil
	}
	return "", fmt.Errorf("could not find a free destination to import a copy of %s", rel)
}

// remapCompressed applies a cwd mapping to a compressed (.jsonl.zst) bundle
// entry by decompressing it, rewriting the session_meta cwd, and recompressing,
// when the external `zstd` tool is available. It returns:
//   - mapped != nil: the recompressed bytes to write (cwd was rewritten);
//   - available=false: zstd is not installed, so nothing was done (caller copies
//     the entry byte-for-byte and reports it as not remapped);
//   - changed=false (available=true): the recorded cwd did not actually match.
//
// The rewrite uses the same narrow, validated path as plain .jsonl mapping, and
// additionally verifies the recompressed frame round-trips back to the exact
// rewritten plaintext before it is accepted. Any decompress/compress/validation
// failure is returned as an error and aborts the import before any write.
func remapCompressed(zr *zip.Reader, rel string, m *CWDMapping) (mapped []byte, changed bool, available bool, err error) {
	if !zstdcli.Available() {
		return nil, false, false, nil
	}
	raw, err := readEntryBytes(zr, rel)
	if err != nil {
		return nil, false, true, err
	}
	plain, err := zstdcli.Decompress(raw)
	if err != nil {
		return nil, false, true, fmt.Errorf("decompress %s: %w", rel, err)
	}
	rewritten, didChange, err := rewriteSessionMetaCWD(plain, m.Old, m.New)
	if err != nil {
		return nil, false, true, fmt.Errorf("map cwd for %s: %w", rel, err)
	}
	if !didChange {
		return nil, false, true, nil
	}
	if err := validateMappedJSONL(plain, rewritten, m.New); err != nil {
		return nil, false, true, fmt.Errorf("mapped %s failed validation: %w", rel, err)
	}
	recompressed, err := zstdcli.Compress(rewritten)
	if err != nil {
		return nil, false, true, fmt.Errorf("recompress %s: %w", rel, err)
	}
	// Round-trip integrity: the recompressed frame must decompress back to the
	// exact rewritten plaintext, or we refuse to write it.
	check, err := zstdcli.Decompress(recompressed)
	if err != nil {
		return nil, false, true, fmt.Errorf("verify recompressed %s: %w", rel, err)
	}
	if !bytes.Equal(check, rewritten) {
		return nil, false, true, fmt.Errorf("recompressed %s does not round-trip to the rewritten content", rel)
	}
	return recompressed, true, true, nil
}

// compressedSessionSuffix is the extension of compressed rollout files. They are
// copied byte-for-byte unless an opted-in --map-cwd rewrite recompresses one.
const compressedSessionSuffix = ".jsonl.zst"

func readEntryBytes(zr *zip.Reader, name string) ([]byte, error) {
	f, err := openByName(zr, name)
	if err != nil {
		return nil, err
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// decideAction determines conflict handling for a single target path.
func decideAction(dest, expectedSum string) (Action, error) {
	info, err := os.Stat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return ActionImport, nil
		}
		return "", err
	}
	if info.IsDir() {
		return ActionConflict, nil
	}
	actual, err := sha256File(dest)
	if err != nil {
		return "", err
	}
	if actual == expectedSum {
		return ActionSkipIdentical, nil
	}
	return ActionConflict, nil
}

// verifyBundle validates every entry path and confirms each file's SHA-256
// matches checksums.json. checksums.json itself is not self-referential.
func verifyBundle(zr *zip.Reader, checksums Checksums) error {
	for _, f := range zr.File {
		if f.Name == ChecksumsName {
			continue
		}
		// Reject unsafe paths (absolute, drive-letter, zip-slip) before anything else.
		if _, err := safety.CleanRelPath(f.Name); err != nil {
			return fmt.Errorf("unsafe bundle entry: %w", err)
		}
		expected, ok := checksums[f.Name]
		if !ok {
			return fmt.Errorf("bundle entry %q is missing from checksums.json", f.Name)
		}
		actual, err := sha256ZipEntry(f)
		if err != nil {
			return fmt.Errorf("hash %q: %w", f.Name, err)
		}
		if actual != expected {
			return fmt.Errorf("checksum mismatch for %q: bundle is corrupt or tampered", f.Name)
		}
	}
	return nil
}

func readMeta(zr *zip.Reader) (Manifest, Checksums, error) {
	var manifest Manifest
	var checksums Checksums
	var sawM, sawC bool
	for _, f := range zr.File {
		switch f.Name {
		case ManifestName:
			data, err := readZipFile(f)
			if err != nil {
				return manifest, checksums, fmt.Errorf("read %s: %w", ManifestName, err)
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return manifest, checksums, fmt.Errorf("parse %s: %w", ManifestName, err)
			}
			sawM = true
		case ChecksumsName:
			data, err := readZipFile(f)
			if err != nil {
				return manifest, checksums, fmt.Errorf("read %s: %w", ChecksumsName, err)
			}
			if err := json.Unmarshal(data, &checksums); err != nil {
				return manifest, checksums, fmt.Errorf("parse %s: %w", ChecksumsName, err)
			}
			sawC = true
		}
	}
	if !sawM {
		return manifest, checksums, fmt.Errorf("bundle is missing %s", ManifestName)
	}
	if !sawC {
		return manifest, checksums, fmt.Errorf("bundle is missing %s", ChecksumsName)
	}
	return manifest, checksums, nil
}

// resolveSelectedPaths maps a list of requested --session ids to the set of
// bundle paths they select. For each id, an exact thread-id match wins; failing
// that, any session whose thread id begins with the id is selected. An id that
// matches no session in the bundle is an error so a typo never silently imports
// nothing. It returns nil when no filter was requested (import everything).
func resolveSelectedPaths(manifest Manifest, ids []string) (map[string]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("empty --session id")
		}
		var exact, prefix []string
		for _, ms := range manifest.Sessions {
			if ms.ThreadID == "" {
				continue
			}
			if ms.ThreadID == id {
				exact = append(exact, ms.BundlePath)
			} else if strings.HasPrefix(ms.ThreadID, id) {
				prefix = append(prefix, ms.BundlePath)
			}
		}
		matches := exact
		if len(matches) == 0 {
			matches = prefix
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no session in the bundle matches --session %q", id)
		}
		for _, p := range matches {
			selected[p] = true
		}
	}
	return selected, nil
}

func isArchivedEntry(rel string) bool {
	return len(rel) >= len(codexhome.ArchivedSessionsSubdir)+1 &&
		rel[:len(codexhome.ArchivedSessionsSubdir)+1] == codexhome.ArchivedSessionsSubdir+"/"
}

func copyEntry(zr *zip.Reader, name, dest string) error {
	f, err := openByName(zr, name)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return safety.CopyAtomic(dest, rc)
}

func openByName(zr *zip.Reader, name string) (*zip.File, error) {
	for _, f := range zr.File {
		if f.Name == name {
			return f, nil
		}
	}
	return nil, fmt.Errorf("entry %q not found in bundle", name)
}

func sha256ZipEntry(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
