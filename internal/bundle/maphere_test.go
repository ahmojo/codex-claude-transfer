package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// sampleRel2 is a second, distinct session path for multi-session bundles.
const sampleRel2 = "sessions/2026/06/13/rollout-2026-06-13T18-30-00-bbbb1111-2222-3333-4444-555566667777.jsonl"

// buildTwoCWDBundle writes a bundle with two sessions recorded under two
// different project cwds, for testing the multi-project ambiguity guard.
func buildTwoCWDBundle(t *testing.T, dir, cwdA, cwdB string) string {
	t.Helper()
	dataA := metaSessionJSONL(t, cwdA)
	dataB := metaSessionJSONL(t, cwdB)
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Sessions: []ManifestSession{
			{ThreadID: "aaaa", BundlePath: sampleRel, OriginalCWD: cwdA, SizeBytes: int64(len(dataA)), SHA256: sha256Hex(dataA)},
			{ThreadID: "bbbb", BundlePath: sampleRel2, OriginalCWD: cwdB, SizeBytes: int64(len(dataB)), SHA256: sha256Hex(dataB)},
		},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{
		sampleRel:    sha256Hex(dataA),
		sampleRel2:   sha256Hex(dataB),
		ManifestName: sha256Hex(manifestBytes),
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	path := filepath.Join(dir, "two.codexbundle")
	writeBundleZip(t, path, []rawEntry{
		{sampleRel, dataA},
		{sampleRel2, dataB},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})
	return path
}

// TestMapCWDHereMapsSingleProject: --map-cwd-here rewrites the one recorded cwd to
// the caller's directory without the caller listing the old path.
func TestMapCWDHereMapsSingleProject(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWDHere: true,
		HereDir:    "/new/here",
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Mapped != 1 {
		t.Fatalf("counts: imported=%d mapped=%d", res.Imported, res.Mapped)
	}
	scan, err := sessions.Scan(target, sessions.ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(scan.Sessions) != 1 || scan.Sessions[0].CWD != "/new/here" {
		t.Fatalf("cwd not remapped to /new/here: %+v", scan.Sessions)
	}
}

// TestMapCWDHereRejectsMultiProject: a bundle spanning two projects is ambiguous
// for --map-cwd-here and must be rejected before anything is written.
func TestMapCWDHereRejectsMultiProject(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildTwoCWDBundle(t, dir, "/proj/a", "/proj/b")

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{BundlePath: bundlePath, MapCWDHere: true, HereDir: "/new/here"})
	if err == nil {
		t.Fatal("expected an ambiguity error for a multi-project bundle")
	}
	if !strings.Contains(err.Error(), "spans") {
		t.Errorf("unexpected error: %v", err)
	}
	if got := listFilesRel(t, target.Root); len(got) != 0 {
		t.Errorf("files were written despite the rejection: %v", got)
	}
}

// TestMapCWDHereRejectsCombinedWithMapCWD: the two map flags are mutually exclusive.
func TestMapCWDHereRejectsCombinedWithMapCWD(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{
		BundlePath: bundlePath,
		MapCWDHere: true,
		HereDir:    "/new/here",
		MapCWD:     []CWDMapping{{Old: "/source/proj", New: "/other"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected mutual-exclusivity error, got %v", err)
	}
}

// TestMapCWDHereAlreadyHereImportsAsIs: when the sole recorded cwd already equals
// the current dir, no rewrite happens and the session imports normally.
func TestMapCWDHereAlreadyHereImportsAsIs(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/source/proj")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/source/proj", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, MapCWDHere: true, HereDir: "/source/proj"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.Mapped != 0 {
		t.Fatalf("counts: imported=%d mapped=%d (expected import without a remap)", res.Imported, res.Mapped)
	}
}
