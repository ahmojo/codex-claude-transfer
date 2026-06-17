package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// growthRelation classifies how a bundle's version of a session relates to the
// existing local file, treating both as append-only logs (which is what Codex
// rollout files and Claude transcripts are).
type growthRelation int

const (
	// growthDiverged: neither side is a prefix of the other; both were extended
	// independently, so this is a genuine conflict.
	growthDiverged growthRelation = iota
	// growthBundleExtends: the local file is a (proper) prefix of the bundle's
	// version — the session grew on the other device. Writing the bundle's
	// longer version appends the new lines without losing anything.
	growthBundleExtends
	// growthLocalAhead: the bundle is a prefix of the local file — the local copy
	// already contains everything in the bundle (and possibly more).
	growthLocalAhead
	// growthEqual: byte-identical. (Normally handled as skip-identical earlier;
	// classified here only for completeness.)
	growthEqual
)

// classifyGrowth compares two append-only logs by byte prefix. Because session
// files are only ever appended to, a strict byte-prefix relationship is a sound,
// parse-free test for "one is a forward extension of the other".
func classifyGrowth(bundlePlain, localPlain []byte) growthRelation {
	switch {
	case bytes.Equal(bundlePlain, localPlain):
		return growthEqual
	case bytes.HasPrefix(bundlePlain, localPlain):
		return growthBundleExtends
	case bytes.HasPrefix(localPlain, bundlePlain):
		return growthLocalAhead
	default:
		return growthDiverged
	}
}

// planMerge tries to resolve a conflict as an append-only sync. It compares the
// bundle's content for this session against the existing local file, both as
// plaintext (decompressing .jsonl.zst when the zstd tool is available):
//
//   - local is a prefix of bundle  -> ActionUpdate (the session grew; the longer
//     bundle version is written, losslessly appending the new lines).
//   - bundle is a prefix of local  -> ActionSkipAhead (local already current).
//   - they diverged                -> ActionConflict (unchanged), letting the
//     resolution flags or the default skip handle it.
//
// A compressed session that cannot be compared without zstd stays a conflict with
// an explanatory warning. Only IO/decompression failures return an error (which
// aborts the import before any write).
func planMerge(zr *zip.Reader, item *ImportItem, rel string, result *ImportResult) (Action, error) {
	bundlePlain, localPlain, comparable, err := mergePlaintext(zr, item, rel, item.DestPath)
	if err != nil {
		return "", err
	}
	if !comparable {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%s: compressed session differs and cannot be merged without the 'zstd' tool; skipped (conflict)", rel))
		return ActionConflict, nil
	}
	switch classifyGrowth(bundlePlain, localPlain) {
	case growthBundleExtends:
		item.LinesAdded = countAppendedLines(localPlain, bundlePlain)
		return ActionUpdate, nil
	case growthLocalAhead, growthEqual:
		return ActionSkipAhead, nil
	default:
		return ActionConflict, nil
	}
}

// mergePlaintext returns the plaintext bytes to compare for a merge: the bundle's
// version (the bytes that would actually be written — already cwd-mapped when
// item.content is set) and the current local file. For plain .jsonl files the
// on-disk bytes are the plaintext. For .jsonl.zst files both sides are
// decompressed, which requires the external zstd tool; when it is unavailable,
// comparable is false and the caller leaves the entry a conflict.
func mergePlaintext(zr *zip.Reader, item *ImportItem, rel, dest string) (bundlePlain, localPlain []byte, comparable bool, err error) {
	bundleBytes := item.content
	if bundleBytes == nil {
		bundleBytes, err = readEntryBytes(zr, rel)
		if err != nil {
			return nil, nil, false, err
		}
	}
	localBytes, err := os.ReadFile(dest)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read local %s: %w", rel, err)
	}
	if !strings.HasSuffix(rel, compressedSessionSuffix) {
		return bundleBytes, localBytes, true, nil
	}
	if !zstdcli.Available() {
		return nil, nil, false, nil
	}
	bp, err := zstdcli.Decompress(bundleBytes)
	if err != nil {
		return nil, nil, false, fmt.Errorf("decompress bundle %s: %w", rel, err)
	}
	lp, err := zstdcli.Decompress(localBytes)
	if err != nil {
		return nil, nil, false, fmt.Errorf("decompress local %s: %w", rel, err)
	}
	return bp, lp, true, nil
}

// countAppendedLines returns an approximate count of the lines the bundle adds on
// top of the local file. localPlain is assumed to be a prefix of bundlePlain. The
// count is line-based and for reporting only.
func countAppendedLines(localPlain, bundlePlain []byte) int {
	if len(bundlePlain) <= len(localPlain) {
		return 0
	}
	suffix := bundlePlain[len(localPlain):]
	n := bytes.Count(suffix, []byte{'\n'})
	if suffix[len(suffix)-1] != '\n' {
		n++ // a trailing line with no final newline still counts
	}
	return n
}
