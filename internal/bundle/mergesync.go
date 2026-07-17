package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
		// Byte-diverged. Before declaring a conflict, retry the comparison in a
		// serialization-tolerant (canonical) form: after a cross-platform transfer
		// the SAME records can differ only in JSON key order or escaping, which a
		// byte prefix check misreads as divergence (issue #6).
		return planCanonicalMerge(item, rel, bundlePlain, localPlain)
	}
}

// planCanonicalMerge re-runs the append-only analysis comparing records
// canonically (each JSONL line parsed and re-marshaled with sorted keys) instead
// of byte-for-byte. This recognizes sessions that are semantically identical, or
// semantically grown, across machines whose agents serialize the same records
// differently (key order, string escaping, line endings).
//
// It is deliberately conservative: number literals are preserved exactly (so
// values beyond float64 precision — e.g. nanosecond timestamps — can never
// collapse into false equality), a line that is not valid JSON falls back to its
// raw bytes, and any real value difference remains a conflict.
//
// On a canonical fast-forward the LOCAL file's existing bytes are preserved
// verbatim and only the bundle's raw new lines are appended — the local
// serialization is never rewritten into the bundle's.
func planCanonicalMerge(item *ImportItem, rel string, bundlePlain, localPlain []byte) (Action, error) {
	bundleLines, bundleOffsets := splitJSONLLines(bundlePlain)
	localLines, _ := splitJSONLLines(localPlain)

	common := len(localLines)
	if len(bundleLines) < common {
		common = len(bundleLines)
	}
	for i := 0; i < common; i++ {
		if !bytes.Equal(canonicalJSONLLine(localLines[i]), canonicalJSONLLine(bundleLines[i])) {
			return ActionConflict, nil
		}
	}
	switch {
	case len(bundleLines) <= len(localLines):
		// Canonically identical, or the local file is canonically ahead: the
		// local copy already holds everything the bundle does.
		return ActionSkipAhead, nil
	default:
		// Canonical fast-forward: keep the local bytes, append the bundle's raw
		// suffix (its lines beyond the shared canonical prefix) verbatim.
		suffix := bundlePlain[bundleOffsets[len(localLines)]:]
		merged := make([]byte, 0, len(localPlain)+1+len(suffix))
		merged = append(merged, localPlain...)
		if len(merged) > 0 && merged[len(merged)-1] != '\n' {
			merged = append(merged, '\n')
		}
		merged = append(merged, suffix...)

		out := merged
		if strings.HasSuffix(rel, compressedSessionSuffix) {
			// mergePlaintext only reports .zst content as comparable when the
			// zstd tool is available, so recompression here can only fail on a
			// real tool error — which aborts the import before any write.
			z, err := zstdcli.Compress(merged)
			if err != nil {
				return "", fmt.Errorf("recompress merged %s: %w", rel, err)
			}
			out = z
		}
		item.content = out
		item.LinesAdded = len(bundleLines) - len(localLines)
		return ActionUpdate, nil
	}
}

// splitJSONLLines splits b into lines (without their terminators) and records
// the byte offset at which each line starts, so a caller can slice the original
// bytes from any line boundary. A trailing line without a final newline counts.
func splitJSONLLines(b []byte) (lines [][]byte, offsets []int) {
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			offsets = append(offsets, start)
			lines = append(lines, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		offsets = append(offsets, start)
		lines = append(lines, b[start:])
	}
	return lines, offsets
}

// canonicalJSONLLine returns a canonical byte form of one JSONL line for
// comparison: the line parsed as a single JSON value and re-marshaled, which
// sorts object keys (at every nesting level) and normalizes string escaping and
// insignificant whitespace. Number literals are preserved exactly via
// json.Number, so no precision is lost and distinct literals stay distinct. A
// line that is not exactly one valid JSON value is returned as-is (minus a
// trailing \r), making the comparison fall back to raw bytes for it.
func canonicalJSONLLine(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return line
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return line
	}
	// Reject trailing content after the value ("{...}garbage"): not a clean
	// JSONL record, so compare it raw.
	if _, err := dec.Token(); err != io.EOF {
		return line
	}
	canon, err := json.Marshal(v)
	if err != nil {
		return line
	}
	return canon
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
