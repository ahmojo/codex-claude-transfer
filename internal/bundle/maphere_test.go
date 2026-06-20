package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// TestImportPreservesOriginalMtime: an imported session file keeps the source's
// last-activity time (manifest updated_at), not today's import time, so the
// agent's index does not see it as "newer than indexed" and re-parse it on every
// open. See docs/research/milestone-0-codex-source-investigation.md.
func TestImportPreservesOriginalMtime(t *testing.T) {
	dir := t.TempDir()
	data := metaSessionJSONL(t, "/proj/x")
	want := time.Date(2026, 6, 14, 15, 48, 0, 0, time.UTC)

	// Build a bundle and stamp the manifest session's updated_at by hand.
	bundlePath := buildBundle(t, dir, sampleRel, data, "/proj/x", nil)
	rewriteManifestUpdatedAt(t, bundlePath, want)

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err != nil {
		t.Fatalf("import: %v", err)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat imported file: %v", err)
	}
	if !info.ModTime().Equal(want) {
		t.Errorf("imported mtime = %v, want %v (original updated_at)", info.ModTime().UTC(), want)
	}
}

// rewriteManifestUpdatedAt rewrites the single session's updated_at inside a
// bundle's manifest (and the manifest checksum) so the import has a known source
// time to restore. It re-zips the bundle in place.
func rewriteManifestUpdatedAt(t *testing.T, bundlePath string, ts time.Time) {
	t.Helper()
	data := metaSessionJSONL(t, "/proj/x")
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Sessions: []ManifestSession{{
			ThreadID:    "aaaa1111-2222-3333-4444-555566667777",
			BundlePath:  sampleRel,
			OriginalCWD: "/proj/x",
			UpdatedAt:   ts.UTC().Format(time.RFC3339),
			SizeBytes:   int64(len(data)),
			SHA256:      sha256Hex(data),
		}},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{
		sampleRel:    sha256Hex(data),
		ManifestName: sha256Hex(manifestBytes),
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	writeBundleZip(t, bundlePath, []rawEntry{
		{sampleRel, data},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})
}

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
