package bundle

// Tests for the serialization-tolerant (canonical) merge comparison that fixes
// issue #6: after a cross-platform transfer, semantically identical JSONL
// records can differ in JSON key order (and escaping), which the byte-prefix
// check misread as divergence, producing false conflicts under --merge.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// Two serializations of the SAME two records, differing only in key order
// (macOS-style vs linux-style), plus one extra record only the bundle has.
const (
	metaLocal  = `{"type":"session_meta","timestamp":"2026-07-01T10:00:00Z","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":"/proj/x"}}`
	metaBundle = `{"payload":{"cwd":"/proj/x","id":"aaaa1111-2222-3333-4444-555566667777"},"timestamp":"2026-07-01T10:00:00Z","type":"session_meta"}`
	msg1Local  = `{"type":"event_msg","timestamp":"2026-07-01T10:01:00Z","payload":{"type":"user_message","message":"hello"}}`
	msg1Bundle = `{"payload":{"message":"hello","type":"user_message"},"timestamp":"2026-07-01T10:01:00Z","type":"event_msg"}`
	msg2Bundle = `{"payload":{"message":"appended on the other machine","type":"user_message"},"timestamp":"2026-07-01T10:02:00Z","type":"event_msg"}`
)

func TestCanonicalJSONLLine(t *testing.T) {
	eq := func(a, b string) bool {
		return bytes.Equal(canonicalJSONLLine([]byte(a)), canonicalJSONLLine([]byte(b)))
	}
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"key order normalized", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"nested key order normalized", `{"o":{"x":1,"y":2},"t":"s"}`, `{"t":"s","o":{"y":2,"x":1}}`, true},
		{"real records equal across orders", metaLocal, metaBundle, true},
		{"different values stay different", `{"a":1}`, `{"a":2}`, false},
		{"insignificant whitespace normalized", `{"a": 1,  "b": 2}`, `{"b":2,"a":1}`, true},
		{"unicode escape equals literal", `{"s":"café"}`, `{"s":"café"}`, true},
		// Number literals are preserved exactly: values beyond float64 precision
		// must never collapse into false equality...
		{"big int precision kept", `{"ts":1755043200000000001}`, `{"ts":1755043200000000002}`, false},
		// ...and even semantically equal numbers with different literals stay
		// distinct (conservative direction: never falsely equal).
		{"distinct number literals stay distinct", `{"n":1e3}`, `{"n":1000}`, false},
		{"same big int equal across orders", `{"ts":1755043200000000001,"a":1}`, `{"a":1,"ts":1755043200000000001}`, true},
		{"invalid JSON falls back to raw (differs)", `not json A`, `not json B`, false},
		{"invalid JSON falls back to raw (same)", `not json A`, `not json A`, true},
		{"trailing garbage compares raw", `{"a":1}garbage`, `{"a":1}`, false},
		{"CRLF equals LF", `{"a":1}` + "\r", `{"a":1}`, true},
		{"empty lines equal", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eq(tc.a, tc.b); got != tc.want {
				t.Errorf("canonical equality(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSplitJSONLLines(t *testing.T) {
	lines, offsets := splitJSONLLines([]byte("aa\nbbb\ncc"))
	if len(lines) != 3 || string(lines[0]) != "aa" || string(lines[1]) != "bbb" || string(lines[2]) != "cc" {
		t.Fatalf("lines = %q", lines)
	}
	if offsets[0] != 0 || offsets[1] != 3 || offsets[2] != 7 {
		t.Fatalf("offsets = %v", offsets)
	}
	// Trailing newline: no phantom empty final line.
	lines, _ = splitJSONLLines([]byte("aa\n"))
	if len(lines) != 1 {
		t.Fatalf("trailing-newline lines = %q", lines)
	}
	if l, o := splitJSONLLines(nil); len(l) != 0 || len(o) != 0 {
		t.Fatalf("empty input should yield no lines")
	}
}

// TestMergeCanonicalIdenticalKeyOrder is the reporter's "13 of 15 sessions were
// identical" case: same records, different key order. --merge must report the
// session as already up to date — not a conflict — and leave the local file
// byte-for-byte untouched.
func TestMergeCanonicalIdenticalKeyOrder(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte(metaBundle + "\n" + msg1Bundle + "\n")
	localData := []byte(metaLocal + "\n" + msg1Local + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 {
		t.Fatalf("false conflict: conflicts=%d (issue #6 regression)", res.Conflicts)
	}
	if res.AlreadyAhead != 1 || res.Updated != 0 || res.Imported != 0 {
		t.Fatalf("counts: ahead=%d updated=%d imported=%d, want 1/0/0", res.AlreadyAhead, res.Updated, res.Imported)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(localData) {
		t.Errorf("local file was modified:\n got=%q\nwant=%q", got, localData)
	}
}

// TestMergeCanonicalFastForward is the reporter's "1 session was local plus one
// appended record" case: the shared records differ only in key order and the
// bundle has one extra line. --merge must fast-forward: keep the local bytes
// exactly as they are and append only the bundle's raw new line.
func TestMergeCanonicalFastForward(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte(metaBundle + "\n" + msg1Bundle + "\n" + msg2Bundle + "\n")
	localData := []byte(metaLocal + "\n" + msg1Local + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 || res.Updated != 1 {
		t.Fatalf("counts: conflicts=%d updated=%d, want 0/1", res.Conflicts, res.Updated)
	}
	if res.LinesAdded != 1 {
		t.Errorf("LinesAdded = %d, want 1", res.LinesAdded)
	}
	got, _ := os.ReadFile(dest)
	want := string(localData) + msg2Bundle + "\n"
	if string(got) != want {
		t.Fatalf("merged file wrong:\n got=%q\nwant=%q", got, want)
	}
	// The local serialization of the shared prefix must be preserved verbatim —
	// the merge must not rewrite it into the bundle's key order.
	if !strings.HasPrefix(string(got), string(localData)) {
		t.Error("local prefix bytes were rewritten")
	}

	// Idempotence: importing the same bundle again is a no-op, not a conflict.
	res2, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.Conflicts != 0 || res2.Updated != 0 || res2.AlreadyAhead != 1 {
		t.Fatalf("re-import counts: conflicts=%d updated=%d ahead=%d, want 0/0/1", res2.Conflicts, res2.Updated, res2.AlreadyAhead)
	}
	if got2, _ := os.ReadFile(dest); string(got2) != want {
		t.Errorf("re-import changed the file: %q", got2)
	}
}

// TestMergeCanonicalWithMapCWD is the exact cross-platform scenario from the
// issue: the bundle records a macOS cwd and foreign key order; the local machine
// holds the same session under a Linux path. With --map-cwd (old→new) plus
// --merge, the extra record fast-forwards — no conflict.
func TestMergeCanonicalWithMapCWD(t *testing.T) {
	const macMeta = `{"payload":{"cwd":"/Users/mac/proj","id":"aaaa1111-2222-3333-4444-555566667777"},"timestamp":"2026-07-01T10:00:00Z","type":"session_meta"}`
	const wslMeta = `{"type":"session_meta","timestamp":"2026-07-01T10:00:00Z","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":"/home/wsl/proj"}}`

	dir := t.TempDir()
	bundleData := []byte(macMeta + "\n" + msg1Bundle + "\n" + msg2Bundle + "\n")
	localData := []byte(wslMeta + "\n" + msg1Local + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/Users/mac/proj", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	mappings, err := ParseCWDMappings([]string{"/Users/mac/proj=/home/wsl/proj"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true, MapCWD: mappings})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 || res.Updated != 1 {
		t.Fatalf("cross-platform merge: conflicts=%d updated=%d, want 0/1 (issue #6 regression)", res.Conflicts, res.Updated)
	}
	got, _ := os.ReadFile(dest)
	if !strings.HasPrefix(string(got), string(localData)) {
		t.Errorf("local prefix not preserved:\n%q", got)
	}
	if !strings.Contains(string(got), "appended on the other machine") {
		t.Errorf("appended record missing:\n%q", got)
	}
	if strings.Contains(string(got), "/Users/mac/proj") {
		t.Errorf("unmapped macOS path leaked into the merged file:\n%q", got)
	}
}

// TestMergeCanonicalRealDivergenceStillConflicts: a genuine VALUE difference
// (not just serialization) must still be reported as a conflict, and the local
// file must stay untouched.
func TestMergeCanonicalRealDivergenceStillConflicts(t *testing.T) {
	dir := t.TempDir()
	divergedBundle := []byte(metaBundle + "\n" + `{"payload":{"message":"bundle version","type":"user_message"},"timestamp":"2026-07-01T10:01:00Z","type":"event_msg"}` + "\n")
	localData := []byte(metaLocal + "\n" + `{"type":"event_msg","timestamp":"2026-07-01T10:01:00Z","payload":{"type":"user_message","message":"local version"}}` + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, divergedBundle, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.Updated != 0 || res.AlreadyAhead != 0 {
		t.Fatalf("counts: conflicts=%d updated=%d ahead=%d, want 1/0/0", res.Conflicts, res.Updated, res.AlreadyAhead)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(localData) {
		t.Errorf("conflict modified the local file: %q", got)
	}
}

// TestMergeCanonicalDivergedPrefixConflicts: when a record WITHIN the shared
// length differs by value, extra bundle lines must not be appended.
func TestMergeCanonicalDivergedPrefixConflicts(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte(metaBundle + "\n" + `{"payload":{"message":"different","type":"user_message"},"timestamp":"2026-07-01T10:01:00Z","type":"event_msg"}` + "\n" + msg2Bundle + "\n")
	localData := []byte(metaLocal + "\n" + msg1Local + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.Updated != 0 {
		t.Fatalf("counts: conflicts=%d updated=%d, want 1/0", res.Conflicts, res.Updated)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(localData) {
		t.Errorf("diverged merge wrote to the local file: %q", got)
	}
}

// TestMergeCanonicalDryRunWritesNothing mirrors the reporter's actual
// invocation (`import --merge --dry-run`): the plan must show the resolution
// (no false conflicts) while writing nothing.
func TestMergeCanonicalDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte(metaBundle + "\n" + msg1Bundle + "\n" + msg2Bundle + "\n")
	localData := []byte(metaLocal + "\n" + msg1Local + "\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true, DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 || res.Updated != 1 || res.LinesAdded != 1 {
		t.Fatalf("dry-run plan: conflicts=%d updated=%d lines=%d, want 0/1/1", res.Conflicts, res.Updated, res.LinesAdded)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(localData) {
		t.Errorf("dry-run modified the file: %q", got)
	}
}

// TestMergeCanonicalCompressed: the same canonical fast-forward works for a
// .jsonl.zst session — both sides are decompressed for comparison and the merged
// result is recompressed, preserving the local prefix bytes inside.
func TestMergeCanonicalCompressed(t *testing.T) {
	if !zstdcli.Available() {
		t.Skip("zstd not installed; compressed merge needs it")
	}
	dir := t.TempDir()
	rel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-bbbb2222-3333-4444-5555-666677778888.jsonl.zst"
	bundlePlain := []byte(metaBundle + "\n" + msg1Bundle + "\n" + msg2Bundle + "\n")
	localPlain := []byte(metaLocal + "\n" + msg1Local + "\n")
	bundleZ, err := zstdcli.Compress(bundlePlain)
	if err != nil {
		t.Fatal(err)
	}
	localZ, err := zstdcli.Compress(localPlain)
	if err != nil {
		t.Fatal(err)
	}

	bundlePath := buildBundle(t, dir, rel, bundleZ, "/proj/x", nil)
	target := fakeHome(t)
	dest := filepath.Join(target.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, localZ, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 || res.Updated != 1 {
		t.Fatalf("counts: conflicts=%d updated=%d, want 0/1", res.Conflicts, res.Updated)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	gotPlain, err := zstdcli.Decompress(got)
	if err != nil {
		t.Fatalf("decompress merged dest: %v", err)
	}
	want := string(localPlain) + msg2Bundle + "\n"
	if string(gotPlain) != want {
		t.Errorf("merged compressed content:\n got=%q\nwant=%q", gotPlain, want)
	}
}

// TestMergeCanonicalNoFinalNewline: a local file whose last line lacks a final
// newline still fast-forwards cleanly (a separating newline is inserted before
// the appended raw suffix).
func TestMergeCanonicalNoFinalNewline(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte(metaBundle + "\n" + msg1Bundle + "\n" + msg2Bundle + "\n")
	localData := []byte(metaLocal + "\n" + msg1Local) // no trailing \n
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, localData)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 0 || res.Updated != 1 {
		t.Fatalf("counts: conflicts=%d updated=%d, want 0/1", res.Conflicts, res.Updated)
	}
	got, _ := os.ReadFile(dest)
	want := string(localData) + "\n" + msg2Bundle + "\n"
	if string(got) != want {
		t.Fatalf("merged file wrong:\n got=%q\nwant=%q", got, want)
	}
}
