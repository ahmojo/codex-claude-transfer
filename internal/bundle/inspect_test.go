package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReadsManifestAndChecksumsWithoutExtracting(t *testing.T) {
	// Build a real bundle via Export.
	home := fakeHome(t)
	project := "/proj/inspect"
	day := filepath.Join(home.SessionsDir, "2026", "06", "13")
	writeSession(t, day, "rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl",
		"aaaa1111-2222-3333-4444-555566667777", project)

	bundleDir := t.TempDir()
	out := filepath.Join(bundleDir, "inspect.codexbundle")
	if _, err := Export(home, ExportOptions{ProjectPath: project, OutputPath: out}); err != nil {
		t.Fatalf("export: %v", err)
	}

	res, err := Inspect(out)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if res.Manifest.FormatVersion != FormatVersion {
		t.Errorf("format version = %q", res.Manifest.FormatVersion)
	}
	if len(res.Manifest.Sessions) != 1 {
		t.Fatalf("sessions = %d", len(res.Manifest.Sessions))
	}
	if res.Manifest.Sessions[0].OriginalCWD != project {
		t.Errorf("cwd = %q", res.Manifest.Sessions[0].OriginalCWD)
	}
	if len(res.Checksums) == 0 {
		t.Errorf("expected checksums to be read")
	}
	// Inspect must not extract anything into the bundle's directory.
	entries, _ := os.ReadDir(bundleDir)
	if len(entries) != 1 {
		t.Errorf("inspect extracted files: dir has %d entries", len(entries))
	}
}

func TestInspectRejectsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.codexbundle")
	writeBundleZip(t, path, []rawEntry{{ChecksumsName, []byte("{}")}})
	if _, err := Inspect(path); err == nil {
		t.Fatalf("expected error for bundle missing manifest")
	}
}

func TestInspectRejectsBadFormatVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.codexbundle")
	manifest := []byte(`{"format_version":"something-else-v9","source_os":"x","sessions":[]}`)
	writeBundleZip(t, path, []rawEntry{
		{ManifestName, manifest},
		{ChecksumsName, []byte("{}")},
	})
	if _, err := Inspect(path); err == nil {
		t.Fatalf("expected error for unrecognized format version")
	}
}
