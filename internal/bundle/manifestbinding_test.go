package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportRejectsHiddenSession: a bundle whose manifest lists no sessions but
// which carries a valid, checksummed session-shaped entry must be rejected — the
// entry would otherwise be imported while invisible in inspect/preview.
func TestImportRejectsHiddenSession(t *testing.T) {
	dir := t.TempDir()
	hidden := []byte("hidden rollout content\n")
	manifest := Manifest{
		FormatVersion:   FormatVersion,
		SourceCodexHome: "/src/.codex",
		Sessions:        nil, // empty: the entry below is undeclared
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{
		sampleRel:    sha256Hex(hidden),
		ManifestName: sha256Hex(manifestBytes),
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	bundlePath := filepath.Join(dir, "hidden.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{
		{sampleRel, hidden},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err == nil {
		t.Fatal("import accepted a bundle with a session not listed in its manifest")
	}
	if !strings.Contains(err.Error(), "hidden sessions") {
		t.Errorf("unexpected error: %v", err)
	}
	// Nothing should have been written.
	if got := listFilesRel(t, target.Root); len(got) != 0 {
		t.Errorf("files were written despite rejection: %v", got)
	}
}

// TestImportRejectsHiddenArchivedSession verifies that opting in to archived
// imports cannot bypass the manifest binding required for active rollouts.
func TestImportRejectsHiddenArchivedSession(t *testing.T) {
	dir := t.TempDir()
	hidden := []byte("hidden archived rollout content\n")
	manifest := Manifest{
		FormatVersion:   FormatVersion,
		SourceCodexHome: "/src/.codex",
		Sessions:        nil,
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{
		archivedSampleRel: sha256Hex(hidden),
		ManifestName:      sha256Hex(manifestBytes),
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	bundlePath := filepath.Join(dir, "hidden-archived.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{
		{archivedSampleRel, hidden},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{
		BundlePath:      bundlePath,
		IncludeArchived: true,
	})
	if err == nil {
		t.Fatal("import accepted an archived session not listed in its manifest")
	}
	if !strings.Contains(err.Error(), "hidden sessions") {
		t.Errorf("unexpected error: %v", err)
	}
	if got := listFilesRel(t, target.Root); len(got) != 0 {
		t.Errorf("files were written despite rejection: %v", got)
	}
}

// TestImportRejectsManifestChecksumMismatch: a session entry declared in the
// manifest but with a manifest SHA that disagrees with checksums.json is rejected.
func TestImportRejectsManifestChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	data := []byte("a real session\n")
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Sessions: []ManifestSession{{
			BundlePath: sampleRel,
			SHA256:     strings.Repeat("0", 64), // wrong on purpose
		}},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{
		sampleRel:    sha256Hex(data), // real checksum (so verifyBundle passes)
		ManifestName: sha256Hex(manifestBytes),
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	bundlePath := filepath.Join(dir, "mismatch.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{
		{sampleRel, data},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err == nil {
		t.Fatal("import accepted a manifest/checksum mismatch")
	}
}

func TestDirExistsRejectsUNC(t *testing.T) {
	// A real local dir is reported present…
	d := t.TempDir()
	if !DirExists(d) {
		t.Errorf("local dir %q should be reported present", d)
	}
	// …but UNC / device paths are never statted (would trigger SMB).
	for _, p := range []string{`\\attacker.example\share`, "//attacker.example/share", `\\?\C:\x`, `\\.\PhysicalDrive0`} {
		if DirExists(p) {
			t.Errorf("UNC/device path %q should not be reported present", p)
		}
		if isLocalStattablePath(p) {
			t.Errorf("UNC/device path %q should not be stattable", p)
		}
	}
}
