package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

func TestClassifyGrowth(t *testing.T) {
	local := []byte("a\nb\n")
	cases := []struct {
		name          string
		bundle, lpath []byte
		want          growthRelation
	}{
		{"equal", local, local, growthEqual},
		{"bundle extends local", []byte("a\nb\nc\n"), local, growthBundleExtends},
		{"local ahead of bundle", local, []byte("a\nb\nc\n"), growthLocalAhead},
		{"diverged", []byte("a\nX\n"), local, growthDiverged},
		{"empty local, bundle has content", []byte("a\n"), nil, growthBundleExtends},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyGrowth(tc.bundle, tc.lpath); got != tc.want {
				t.Errorf("classifyGrowth = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCountAppendedLines(t *testing.T) {
	cases := []struct {
		local, bundle string
		want          int
	}{
		{"a\nb\n", "a\nb\nc\nd\n", 2},
		{"a\nb\n", "a\nb\nc\n", 1},
		{"a\nb\n", "a\nb\nc", 1}, // trailing line with no final newline still counts
		{"a\nb\n", "a\nb\n", 0},
	}
	for _, tc := range cases {
		if got := countAppendedLines([]byte(tc.local), []byte(tc.bundle)); got != tc.want {
			t.Errorf("countAppendedLines(%q,%q) = %d, want %d", tc.local, tc.bundle, got, tc.want)
		}
	}
}

// writeLocal seeds the destination for sampleRel with the given content.
func writeLocal(t *testing.T, home string, content []byte) string {
	t.Helper()
	dest := filepath.Join(home, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return dest
}

// TestMergeAppendsGrownSession: the bundle's version is the local file plus extra
// lines; --merge updates it in place, losslessly appending the new lines.
func TestMergeAppendsGrownSession(t *testing.T) {
	dir := t.TempDir()
	grown := []byte("line one\nline two\nline three\n")
	bundlePath := buildBundle(t, dir, sampleRel, grown, "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, []byte("line one\nline two\n"))

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Updated != 1 || res.Conflicts != 0 || res.Imported != 0 {
		t.Fatalf("counts: updated=%d conflicts=%d imported=%d", res.Updated, res.Conflicts, res.Imported)
	}
	if res.LinesAdded != 1 {
		t.Errorf("LinesAdded = %d, want 1", res.LinesAdded)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(grown) {
		t.Errorf("dest = %q, want the grown version %q", string(got), string(grown))
	}
}

// TestMergeLocalAheadIsSkipped: the local file already contains everything in the
// bundle (and more); --merge leaves it untouched.
func TestMergeLocalAheadIsSkipped(t *testing.T) {
	dir := t.TempDir()
	bundleData := []byte("line one\nline two\n")
	bundlePath := buildBundle(t, dir, sampleRel, bundleData, "/proj/x", nil)
	target := fakeHome(t)
	ahead := []byte("line one\nline two\nline three\nline four\n")
	dest := writeLocal(t, target.Root, ahead)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.AlreadyAhead != 1 || res.Updated != 0 || res.Conflicts != 0 {
		t.Fatalf("counts: ahead=%d updated=%d conflicts=%d", res.AlreadyAhead, res.Updated, res.Conflicts)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(ahead) {
		t.Errorf("local file was modified: %q", string(got))
	}
}

// TestMergeDivergedStaysConflict: both sides changed independently, so --merge
// alone cannot resolve it and the session remains a skipped conflict.
func TestMergeDivergedStaysConflict(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("line one\nbundle change\n"), "/proj/x", nil)
	target := fakeHome(t)
	local := []byte("line one\nlocal change\n")
	dest := writeLocal(t, target.Root, local)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.Updated != 0 || res.AlreadyAhead != 0 {
		t.Fatalf("counts: conflicts=%d updated=%d ahead=%d", res.Conflicts, res.Updated, res.AlreadyAhead)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(local) {
		t.Errorf("conflict overwrote the local file: %q", string(got))
	}
}

// TestMergeComposesWithReplace: --merge resolves a grown session, while a
// genuinely diverged one in the same import is handled by --replace-with-backup.
func TestMergeComposesWithReplace(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("line one\nbundle change\n"), "/proj/x", nil)
	target := fakeHome(t)
	dest := writeLocal(t, target.Root, []byte("line one\nlocal change\n"))

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true, ReplaceWithBackup: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Replaced != 1 || res.Conflicts != 0 || res.Updated != 0 {
		t.Fatalf("counts: replaced=%d conflicts=%d updated=%d", res.Replaced, res.Conflicts, res.Updated)
	}
	if got, _ := os.ReadFile(dest); string(got) != "line one\nbundle change\n" {
		t.Errorf("dest not replaced: %q", string(got))
	}
}

// TestMergeCompressedSessionGrows: a .jsonl.zst session that grew on the other
// device is merged in place — both sides are decompressed to compare, and the
// bundle's longer compressed version is written. Requires the zstd tool.
func TestMergeCompressedSessionGrows(t *testing.T) {
	if !zstdcli.Available() {
		t.Skip("zstd not installed; compressed merge needs it")
	}
	dir := t.TempDir()
	rel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-bbbb2222-3333-4444-5555-666677778888.jsonl.zst"
	localPlain := []byte("line one\nline two\n")
	grownPlain := []byte("line one\nline two\nline three\n")
	localZ, err := zstdcli.Compress(localPlain)
	if err != nil {
		t.Fatalf("compress local: %v", err)
	}
	grownZ, err := zstdcli.Compress(grownPlain)
	if err != nil {
		t.Fatalf("compress grown: %v", err)
	}

	bundlePath := buildBundle(t, dir, rel, grownZ, "/proj/x", nil)
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
	if res.Updated != 1 || res.Conflicts != 0 {
		t.Fatalf("counts: updated=%d conflicts=%d", res.Updated, res.Conflicts)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	gotPlain, err := zstdcli.Decompress(got)
	if err != nil {
		t.Fatalf("decompress dest: %v", err)
	}
	if string(gotPlain) != string(grownPlain) {
		t.Errorf("dest decompressed = %q, want the grown version %q", gotPlain, grownPlain)
	}
}

// TestMergeDryRunWritesNothing: a planned update reports Updated but writes nothing.
func TestMergeDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("line one\nline two\nline three\n"), "/proj/x", nil)
	target := fakeHome(t)
	local := []byte("line one\nline two\n")
	dest := writeLocal(t, target.Root, local)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Merge: true, DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Updated != 1 {
		t.Fatalf("Updated = %d, want 1 (planned)", res.Updated)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(local) {
		t.Errorf("dry-run modified the file: %q", string(got))
	}
}
