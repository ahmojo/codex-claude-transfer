package bundle

// Fuzz tests for the serialization-tolerant (canonical) comparison behind
// import --merge (issue #6). The comparator processes session files that were
// produced by another machine, so it is fuzzed against hostile shapes: deeply
// nested JSON, huge number literals, unusual unicode escapes, mixed CRLF/LF,
// corrupted trailing lines, and very large single lines.
//
// The load-bearing invariant, checked from every angle:
//
//	the canonical comparison must NEVER classify two semantically different
//	values as equal.
//
// "Semantically equal" is defined by Go's own JSON decoding with UseNumber():
// two lines are equal only if both decode to deeply equal values (number
// literals compared exactly, so no float64 collapse), or neither decodes and
// their raw bytes (minus a trailing \r) are identical.

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// fuzzDecodeLine mirrors canonicalJSONLLine's parsing rules: one strict JSON
// value per line, exact number literals, no trailing content.
func fuzzDecodeLine(line []byte) (any, bool) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return v, true
}

// linesSemanticallyEqual is the ground truth the canonical comparison is
// checked against.
func linesSemanticallyEqual(a, b []byte) bool {
	va, oka := fuzzDecodeLine(a)
	vb, okb := fuzzDecodeLine(b)
	if oka != okb {
		return false
	}
	if oka {
		return reflect.DeepEqual(va, vb)
	}
	return bytes.Equal(bytes.TrimSuffix(a, []byte{'\r'}), bytes.TrimSuffix(b, []byte{'\r'}))
}

func addLinePairSeeds(f *testing.F) {
	deepA := strings.Repeat(`{"a":`, 200) + `1` + strings.Repeat(`}`, 200)
	deepB := strings.Repeat(`{"a":`, 200) + `2` + strings.Repeat(`}`, 200)
	bigNum := `{"n":` + strings.Repeat("9", 400) + `}`
	bigLine := `{"blob":"` + strings.Repeat("x", 64*1024) + `"}`
	seeds := [][2]string{
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`}, // key order only
		{`{"a":1,"b":2}`, `{"a":1,"b":3}`}, // real value difference
		{deepA, deepA},                     // deep nesting, equal
		{deepA, deepB},                     // deep nesting, innermost differs
		{`{"ts":1755043200000000001}`, `{"ts":1755043200000000002}`}, // beyond float64 precision
		{`{"n":1e3}`, `{"n":1000}`},                                  // distinct literals, same float
		{bigNum, bigNum},                                             // 400-digit literal
		{`{"s":"\u00e9"}`, `{"s":"é"}`},                              // escape vs raw UTF-8
		{`{"s":"\ud800"}`, `{"s":"\udc00"}`},                         // lone surrogates
		{`{"s":"a\/b"}`, `{"s":"a/b"}`},                              // escaped slash
		{`{"a":1}` + "\r", `{"a":1}`},                                // CRLF vs LF
		{`{"a":1`, `{"a":1`},                                         // corrupted (truncated) line
		{`{"a":1}garbage`, `{"a":1}`},                                // trailing garbage vs clean
		{bigLine, bigLine},                                           // very large single line
		{``, ` `},                                                    // empty vs whitespace
		{`null`, `null`},                                             // bare scalar
		{`[1,2,3]`, `[1,2,3]`},                                       // array record
		{string([]byte{0xff, 0xfe, '{', '}'}), `{}`},                 // invalid UTF-8 prefix
	}
	for _, s := range seeds {
		f.Add([]byte(s[0]), []byte(s[1]))
	}
}

// FuzzCanonicalJSONLLine checks that canonical equality of two arbitrary lines
// coincides exactly with semantic equality — in particular that different
// values never collapse into a false canonical match.
func FuzzCanonicalJSONLLine(f *testing.F) {
	addLinePairSeeds(f)
	f.Fuzz(func(t *testing.T, a, b []byte) {
		canonEqual := bytes.Equal(canonicalJSONLLine(a), canonicalJSONLLine(b))
		semEqual := linesSemanticallyEqual(a, b)
		if canonEqual && !semEqual {
			t.Fatalf("canonical comparison classified DIFFERENT values as equal:\n a=%q\n b=%q", a, b)
		}
		if !canonEqual && semEqual {
			t.Fatalf("canonical comparison missed semantically equal lines:\n a=%q\n b=%q", a, b)
		}
	})
}

// FuzzSplitJSONLLines checks the line splitter used to slice raw suffixes out
// of a bundle: offsets must address the original bytes exactly, lines must
// never contain a newline, and the split must account for every byte.
func FuzzSplitJSONLLines(f *testing.F) {
	for _, s := range []string{
		"", "\n", "\n\n", "a", "a\n", "a\nb", "a\nb\n", "a\r\nb\r\n",
		"{\"a\":1}\n{\"b\":2}", "x\r\r\n\ny",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		lines, offsets := splitJSONLLines(b)
		if len(lines) != len(offsets) {
			t.Fatalf("len(lines)=%d len(offsets)=%d", len(lines), len(offsets))
		}
		next := 0
		for i, line := range lines {
			if bytes.ContainsRune(line, '\n') {
				t.Fatalf("line %d contains newline: %q", i, line)
			}
			if offsets[i] != next {
				t.Fatalf("offset %d = %d, want %d (input %q)", i, offsets[i], next, b)
			}
			if got := b[offsets[i] : offsets[i]+len(line)]; !bytes.Equal(got, line) {
				t.Fatalf("offset %d does not address line bytes: %q vs %q", i, got, line)
			}
			next = offsets[i] + len(line) + 1 // +1 for the terminating '\n'
		}
		// The split must cover the whole input: the position after the last
		// line (plus its newline, if present) equals len(b).
		switch {
		case len(lines) == 0:
			if len(b) != 0 {
				t.Fatalf("non-empty input %q produced no lines", b)
			}
		case bytes.HasSuffix(b, []byte{'\n'}):
			if next != len(b)+0 {
				t.Fatalf("split does not cover input: next=%d len=%d (%q)", next, len(b), b)
			}
		default:
			if next != len(b)+1 { // last line has no terminator
				t.Fatalf("split does not cover input: next=%d len=%d (%q)", next, len(b), b)
			}
		}
	})
}

func addMergePairSeeds(f *testing.F) {
	metaLinux := `{"timestamp":"2026-07-01T10:00:00Z","type":"session_meta","payload":{"id":"f1","cwd":"/p"}}`
	metaMac := `{"payload":{"cwd":"/p","id":"f1"},"type":"session_meta","timestamp":"2026-07-01T10:00:00Z"}`
	msgLinux := `{"timestamp":"2026-07-01T10:01:00Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`
	msgMac := `{"payload":{"message":"hi","type":"user_message"},"timestamp":"2026-07-01T10:01:00Z","type":"event_msg"}`
	extra := `{"timestamp":"2026-07-01T10:02:00Z","type":"event_msg","payload":{"type":"user_message","message":"more"}}`
	seeds := [][2]string{
		{metaMac + "\n" + msgMac + "\n", metaLinux + "\n" + msgLinux + "\n"},                // identical, reordered keys
		{metaMac + "\n" + msgMac + "\n" + extra + "\n", metaLinux + "\n" + msgLinux + "\n"}, // canonical fast-forward
		{metaMac + "\r\n" + msgMac + "\r\n", metaLinux + "\n" + msgLinux + "\n"},            // CRLF bundle vs LF local
		{metaMac + "\n" + msgMac, metaLinux + "\n" + msgLinux + "\n"},                       // bundle missing final newline
		{metaMac + "\n" + extra + "\n", metaLinux + "\n" + msgLinux + "\n"},                 // genuinely diverged
		{metaMac + "\n" + msgMac + "\n" + `{"broken":`, metaLinux + "\n" + msgLinux},        // corrupted last bundle line
		{"", metaLinux + "\n"}, // empty bundle
		{metaMac + "\n", ""},   // empty local
	}
	for _, s := range seeds {
		f.Add([]byte(s[0]), []byte(s[1]))
	}
}

// FuzzPlanCanonicalMerge feeds arbitrary bundle/local byte pairs through the
// canonical merge planner and checks its safety contract:
//
//   - it never errors on plain .jsonl input,
//   - whenever it does NOT report a conflict, every shared line really is
//     semantically equal (the never-false-equality invariant),
//   - a fast-forward preserves the local bytes verbatim and appends only the
//     bundle's raw surplus lines,
//   - merging is idempotent: re-planning bundle vs merged yields "already ahead".
func FuzzPlanCanonicalMerge(f *testing.F) {
	addMergePairSeeds(f)
	rel := "sessions/2026/07/01/rollout-2026-07-01T10-00-00-fuzz.jsonl"
	f.Fuzz(func(t *testing.T, bundlePlain, localPlain []byte) {
		item := &ImportItem{}
		action, err := planCanonicalMerge(item, rel, bundlePlain, localPlain)
		if err != nil {
			t.Fatalf("planCanonicalMerge errored on plain jsonl: %v", err)
		}
		bundleLines, _ := splitJSONLLines(bundlePlain)
		localLines, _ := splitJSONLLines(localPlain)

		switch action {
		case ActionConflict:
			return // conservative outcome is always allowed
		case ActionSkipAhead, ActionUpdate:
			// No-conflict verdicts assert equality over the shared prefix; that
			// claim must hold semantically, not just canonically.
			common := len(localLines)
			if len(bundleLines) < common {
				common = len(bundleLines)
			}
			for i := 0; i < common; i++ {
				if !linesSemanticallyEqual(localLines[i], bundleLines[i]) {
					t.Fatalf("action %s but line %d differs semantically:\n local=%q\n bundle=%q",
						action, i, localLines[i], bundleLines[i])
				}
			}
		default:
			t.Fatalf("unexpected action %q", action)
		}

		if action != ActionUpdate {
			return
		}
		merged := item.content
		if merged == nil {
			t.Fatal("ActionUpdate without merged content")
		}
		if !bytes.HasPrefix(merged, localPlain) {
			t.Fatalf("fast-forward rewrote local bytes:\n local=%q\n merged=%q", localPlain, merged)
		}
		mergedLines, _ := splitJSONLLines(merged)
		if len(mergedLines) != len(bundleLines) {
			t.Fatalf("merged has %d lines, bundle has %d", len(mergedLines), len(bundleLines))
		}
		for i := len(localLines); i < len(bundleLines); i++ {
			if !bytes.Equal(mergedLines[i], bundleLines[i]) {
				t.Fatalf("appended line %d not byte-identical to bundle:\n merged=%q\n bundle=%q",
					i, mergedLines[i], bundleLines[i])
			}
		}
		if item.LinesAdded != len(bundleLines)-len(localLines) {
			t.Fatalf("LinesAdded=%d, want %d", item.LinesAdded, len(bundleLines)-len(localLines))
		}
		// Idempotence: importing the same bundle again on top of the merged
		// file must resolve as already-ahead, never as another update/conflict.
		again := &ImportItem{}
		action2, err := planCanonicalMerge(again, rel, bundlePlain, merged)
		if err != nil {
			t.Fatalf("re-plan errored: %v", err)
		}
		if action2 != ActionSkipAhead {
			t.Fatalf("merge not idempotent: re-plan gave %q, want %q", action2, ActionSkipAhead)
		}
	})
}

// FuzzPlanCanonicalMergeCompressed exercises the .jsonl.zst output path: a
// canonical fast-forward of a compressed session must produce valid zstd whose
// plaintext obeys the same preserve-local/append-raw contract. Requires the
// external zstd tool (present in CI; skipped where unavailable).
func FuzzPlanCanonicalMergeCompressed(f *testing.F) {
	if !zstdcli.Available() {
		f.Skip("zstd tool not installed")
	}
	addMergePairSeeds(f)
	rel := "sessions/2026/07/01/rollout-2026-07-01T10-00-00-fuzz.jsonl.zst"
	f.Fuzz(func(t *testing.T, bundlePlain, localPlain []byte) {
		item := &ImportItem{}
		action, err := planCanonicalMerge(item, rel, bundlePlain, localPlain)
		if err != nil {
			t.Fatalf("planCanonicalMerge: %v", err)
		}
		if action != ActionUpdate {
			return
		}
		plain, err := zstdcli.Decompress(item.content)
		if err != nil {
			t.Fatalf("merged .zst content is not valid zstd: %v", err)
		}
		if !bytes.HasPrefix(plain, localPlain) {
			t.Fatalf("compressed fast-forward rewrote local bytes:\n local=%q\n merged=%q", localPlain, plain)
		}
		mergedLines, _ := splitJSONLLines(plain)
		bundleLines, _ := splitJSONLLines(bundlePlain)
		if len(mergedLines) != len(bundleLines) {
			t.Fatalf("merged has %d lines, bundle has %d", len(mergedLines), len(bundleLines))
		}
	})
}
