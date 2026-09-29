package bundle

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

const sampleRel = "sessions/2026/06/13/rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl"
const archivedSampleRel = "archived_sessions/rollout-2026-06-13T18-22-01-aaaa1111-2222-3333-4444-555566667777.jsonl"

type rawEntry struct {
	name string
	data []byte
}

func writeBundleZip(t *testing.T, path string, entries []rawEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create %s: %v", e.name, err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatalf("zip write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// buildBundle writes a bundle containing a single session file. overrideChecksums,
// when non-nil, replaces the auto-computed checksum map (used for tamper tests).
func buildBundle(t *testing.T, dir, sessionRel string, sessionData []byte, cwd string, overrideChecksums map[string]string) string {
	t.Helper()
	manifest := Manifest{
		FormatVersion:   FormatVersion,
		SourceOS:        "testos",
		SourceCodexHome: "/source/.codex",
		Sessions: []ManifestSession{{
			ThreadID:    "aaaa1111-2222-3333-4444-555566667777",
			BundlePath:  sessionRel,
			OriginalCWD: cwd,
			SizeBytes:   int64(len(sessionData)),
			SHA256:      sha256Hex(sessionData),
		}},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")

	checks := overrideChecksums
	if checks == nil {
		checks = map[string]string{
			sessionRel:   sha256Hex(sessionData),
			ManifestName: sha256Hex(manifestBytes),
		}
	}
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")

	path := filepath.Join(dir, "test.codexbundle")
	writeBundleZip(t, path, []rawEntry{
		{sessionRel, sessionData},
		{ManifestName, manifestBytes},
		{ChecksumsName, checkBytes},
	})
	return path
}

func listFilesRel(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

func TestImportIntoEmptyHome(t *testing.T) {
	dir := t.TempDir()
	data := []byte("session line one\nsession line two\n")
	bundlePath := buildBundle(t, dir, sampleRel, data, "/proj/x", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || res.SkippedIdentical != 0 || res.Conflicts != 0 {
		t.Fatalf("counts: imported=%d skipped=%d conflicts=%d", res.Imported, res.SkippedIdentical, res.Conflicts)
	}
	// Layout preserved.
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("imported file missing: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content differs after import")
	}
}

func TestImportTwiceSkipsIdentical(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("hello"), "/proj/x", nil)
	target := fakeHome(t)

	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if res.Imported != 0 || res.SkippedIdentical != 1 {
		t.Fatalf("second import counts: imported=%d skippedIdentical=%d", res.Imported, res.SkippedIdentical)
	}
}

func TestImportConflictNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("NEW content"), "/proj/x", nil)
	target := fakeHome(t)

	// Pre-create the target with different content.
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("EXISTING different content"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Conflicts != 1 || res.Imported != 0 {
		t.Fatalf("counts: conflicts=%d imported=%d", res.Conflicts, res.Imported)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "EXISTING different content" {
		t.Errorf("conflict overwrote existing file: %q", string(got))
	}
}

func TestImportReplaceWithBackup(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("NEW content"), "/proj/x", nil)
	target := fakeHome(t)

	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	const local = "EXISTING different content"
	if err := os.WriteFile(dest, []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ReplaceWithBackup: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Replaced != 1 || res.Conflicts != 0 || res.Imported != 0 {
		t.Fatalf("counts: replaced=%d conflicts=%d imported=%d", res.Replaced, res.Conflicts, res.Imported)
	}
	// Target now holds the bundle's version.
	if got, _ := os.ReadFile(dest); string(got) != "NEW content" {
		t.Errorf("target not replaced: %q", string(got))
	}
	// A backup with the original local content must exist...
	var backup string
	for _, item := range res.Items {
		if item.Action == ActionReplace {
			backup = item.BackupPath
		}
	}
	if backup == "" {
		t.Fatalf("no BackupPath recorded on the replaced item")
	}
	if got, _ := os.ReadFile(backup); string(got) != local {
		t.Errorf("backup content = %q, want original %q", string(got), local)
	}
	// ...and the backup must NOT look like an importable rollout file, so Codex
	// ignores it on its next scan.
	rel, _ := filepath.Rel(target.Root, backup)
	if safety.IsSessionEntry(filepath.ToSlash(rel)) {
		t.Errorf("backup path %q matches a session entry; Codex would treat it as a session", rel)
	}
}

func TestImportReplaceWithBackupDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("NEW content"), "/proj/x", nil)
	target := fakeHome(t)

	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	const local = "EXISTING different content"
	if err := os.WriteFile(dest, []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	before := listFilesRel(t, target.Root)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ReplaceWithBackup: true, DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Replaced != 1 {
		t.Fatalf("replaced = %d, want 1 (planned)", res.Replaced)
	}
	if got, _ := os.ReadFile(dest); string(got) != local {
		t.Errorf("dry-run replaced the file: %q", string(got))
	}
	if after := listFilesRel(t, target.Root); len(after) != len(before) {
		t.Errorf("dry-run wrote/backed up files: before=%v after=%v", before, after)
	}
}

func TestImportArchivedRequiresOptInAndUsesNormalSafetyPath(t *testing.T) {
	dir := t.TempDir()
	oldCWD := "/old/project"
	newCWD := "/new/project"
	data := []byte(`{"type":"session_meta","payload":{"id":"aaaa1111-2222-3333-4444-555566667777","cwd":"/old/project"}}` + "\n")
	bundlePath := buildBundle(t, dir, archivedSampleRel, data, oldCWD, nil)
	target := fakeHome(t)
	dest := filepath.Join(target.Root, filepath.FromSlash(archivedSampleRel))

	skipped, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("default import: %v", err)
	}
	if skipped.SkippedOther != 1 || len(skipped.Items) != 1 || skipped.Items[0].Action != ActionSkipArchived {
		t.Fatalf("default archived result: %+v", skipped)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("default import wrote archived destination or stat failed: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("create archived destination: %v", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatalf("write archived destination: %v", err)
	}
	result, err := Import(target, ImportOptions{
		BundlePath:        bundlePath,
		IncludeArchived:   true,
		MapCWD:            []CWDMapping{{Old: oldCWD, New: newCWD}},
		ReplaceWithBackup: true,
	})
	if err != nil {
		t.Fatalf("opt-in archived import: %v", err)
	}
	if result.Replaced != 1 || result.Mapped != 1 || result.SkippedOther != 0 {
		t.Fatalf("opt-in archived result: %+v", result)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read relocated archived session: %v", err)
	}
	if !strings.Contains(string(got), `"cwd":"/new/project"`) {
		t.Fatalf("archived cwd was not remapped: %s", got)
	}
	if result.Items[0].BackupPath == "" {
		t.Fatal("archived replacement did not keep a backup")
	}
	if backup, err := os.ReadFile(result.Items[0].BackupPath); err != nil || string(backup) != string(data) {
		t.Fatalf("archived backup data=%q err=%v", backup, err)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("hello"), "/proj/x", nil)
	target := fakeHome(t)

	before := listFilesRel(t, target.Root)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.DryRun {
		t.Errorf("expected DryRun=true")
	}
	after := listFilesRel(t, target.Root)
	if len(after) != len(before) {
		t.Errorf("dry-run wrote files: before=%v after=%v", before, after)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(sampleRel))
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dry-run created the target file")
	}
}

func TestImportRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	evil := "sessions/../../evil.jsonl"
	data := []byte("evil")
	checks := map[string]string{evil: sha256Hex(data)}
	// Manifest with matching checksum so only the path is the problem.
	manifest := Manifest{FormatVersion: FormatVersion, SourceOS: "x"}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	checks[ManifestName] = sha256Hex(mb)
	cb, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	bundlePath := filepath.Join(dir, "evil.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{{evil, data}, {ManifestName, mb}, {ChecksumsName, cb}})

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err == nil {
		t.Fatalf("expected zip-slip to be rejected")
	}
	if files := listFilesRel(t, target.Root); len(files) != 0 {
		t.Errorf("zip-slip wrote files: %v", files)
	}
}

func buildMemoryBundle(t *testing.T, entry, rel string, data []byte) string {
	t.Helper()
	manifest := Manifest{
		FormatVersion: FormatVersion,
		Tool:          "claude",
		Memory: []ManifestMemory{{
			ProjectCWD: "/proj/x",
			Rel:        rel,
			BundlePath: entry,
			SHA256:     sha256Hex(data),
		}},
	}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := json.Marshal(Checksums{entry: sha256Hex(data), ManifestName: sha256Hex(mb)})
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "memory.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{{entry, data}, {ManifestName, mb}, {ChecksumsName, cb}})
	return bundlePath
}

func TestImportRejectsSymlinkedDestinationParent(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("synthetic session\n"), "/proj/x", nil)
	target := fakeHome(t)
	outside := t.TempDir()
	link := filepath.Join(target.Root, "sessions")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Skipf("creating a directory symlink is unavailable: %v", err)
		}
		// A Windows junction exercises the same parent-path redirection without
		// requiring the developer-mode privilege needed for os.Symlink.
		if out, junctionErr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); junctionErr != nil {
			t.Skipf("creating a directory junction is unavailable: %v: %s", junctionErr, out)
		}
	}
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err == nil {
		t.Fatal("import followed a symlinked sessions directory")
	}
	outsideDest := filepath.Join(outside, "2026", "06", "13", filepath.Base(sampleRel))
	if _, err := os.Lstat(outsideDest); !os.IsNotExist(err) {
		t.Fatalf("import wrote outside the target home: %v", err)
	}
}

func TestImportRejectsCollidingDestinationPaths(t *testing.T) {
	otherRel := strings.Replace(sampleRel, "aaaa1111", "AAAA1111", 1)
	first := []byte("first synthetic session\n")
	second := []byte("second synthetic session\n")
	manifest := Manifest{FormatVersion: FormatVersion, Sessions: []ManifestSession{
		{BundlePath: sampleRel, SHA256: sha256Hex(first)},
		{BundlePath: otherRel, SHA256: sha256Hex(second)},
	}}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := json.Marshal(Checksums{
		sampleRel: sha256Hex(first), otherRel: sha256Hex(second), ManifestName: sha256Hex(mb),
	})
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "collision.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{{sampleRel, first}, {otherRel, second}, {ManifestName, mb}, {ChecksumsName, cb}})

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err == nil {
		t.Fatal("import accepted two entries targeting one destination")
	}
	if files := listFilesRel(t, target.Root); len(files) != 0 {
		t.Fatalf("import wrote files before detecting a destination collision: %v", files)
	}
}

func TestImportRejectsMemoryManifestPathEscape(t *testing.T) {
	entry := "projects/-proj-x/memory/MEMORY.md"
	data := []byte("synthetic memory\n")
	bundlePath := buildMemoryBundle(t, entry, "../rollout-evil.jsonl", data)

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath, WithMemory: true}); err == nil {
		t.Fatal("import accepted a memory path outside the memory directory")
	}
	escaped := filepath.Join(target.Root, "projects", "-proj-x", "rollout-evil.jsonl")
	if _, err := os.Lstat(escaped); !os.IsNotExist(err) {
		t.Fatalf("import wrote memory outside its directory: %v", err)
	}
}

func TestImportRejectsMemoryManifestEntryMismatch(t *testing.T) {
	entry := "projects/-proj-x/memory/MEMORY.md"
	bundlePath := buildMemoryBundle(t, entry, "other.md", []byte("synthetic memory\n"))
	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath, WithMemory: true}); err == nil {
		t.Fatal("import accepted memory metadata naming a different ZIP entry")
	}
	if files := listFilesRel(t, target.Root); len(files) != 0 {
		t.Fatalf("import wrote files with mismatched memory metadata: %v", files)
	}
}

func TestImportAllowsNestedMemoryManifestPath(t *testing.T) {
	entry := "projects/-proj-x/memory/notes/session.md"
	data := []byte("synthetic memory\n")
	bundlePath := buildMemoryBundle(t, entry, "notes/session.md", data)
	target := fakeHome(t)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, WithMemory: true})
	if err != nil {
		t.Fatalf("import nested memory: %v", err)
	}
	if res.MemoryImported != 1 {
		t.Fatalf("memory imported = %d, want 1", res.MemoryImported)
	}
	got, err := os.ReadFile(filepath.Join(target.Root, filepath.FromSlash(entry)))
	if err != nil || string(got) != string(data) {
		t.Fatalf("nested memory content = %q, err = %v", got, err)
	}
}

func TestImportRejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	evil := "/abs/evil.jsonl"
	data := []byte("evil")
	manifest := Manifest{FormatVersion: FormatVersion, SourceOS: "x"}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	checks := map[string]string{evil: sha256Hex(data), ManifestName: sha256Hex(mb)}
	cb, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	bundlePath := filepath.Join(dir, "abs.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{{evil, data}, {ManifestName, mb}, {ChecksumsName, cb}})

	target := fakeHome(t)
	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}

func TestImportChecksumMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	data := []byte("real content")
	// Override the session checksum with a wrong value.
	wrong := map[string]string{
		sampleRel: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	// Build manifest and include its (correct) checksum so only the session mismatches.
	manifest := Manifest{FormatVersion: FormatVersion, SourceOS: "x",
		Sessions: []ManifestSession{{BundlePath: sampleRel, SHA256: sha256Hex(data)}}}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	wrong[ManifestName] = sha256Hex(mb)
	cb, _ := json.MarshalIndent(Checksums(wrong), "", "  ")
	bundlePath := filepath.Join(dir, "tampered.codexbundle")
	writeBundleZip(t, bundlePath, []rawEntry{{sampleRel, data}, {ManifestName, mb}, {ChecksumsName, cb}})

	target := fakeHome(t)
	_, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err == nil {
		t.Fatalf("expected checksum mismatch to be rejected")
	}
	if files := listFilesRel(t, target.Root); len(files) != 0 {
		t.Errorf("checksum mismatch wrote files: %v", files)
	}
}

func TestImportZstByteForByte(t *testing.T) {
	dir := t.TempDir()
	zstRel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-bbbb1111-2222-3333-4444-555566667777.jsonl.zst"
	// Arbitrary "compressed" bytes; must be copied verbatim, never parsed.
	data := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01, 0x02, 0x03, 0xff, 0xfe}
	bundlePath := buildBundle(t, dir, zstRel, data, "/proj/z", nil)

	target := fakeHome(t)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("imported = %d, want 1", res.Imported)
	}
	dest := filepath.Join(target.Root, filepath.FromSlash(zstRel))
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("zst not imported: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("zst content changed: got %v want %v", got, data)
	}
}

func TestImportDoesNotTouchSQLite(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("hello"), "/proj/x", nil)

	target := fakeHome(t)
	if err := os.MkdirAll(target.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Sentinel "state DB" file at the Codex home root; must be untouched.
	sqlitePath := filepath.Join(target.Root, "codex-state.sqlite")
	const sentinel = "DO NOT MODIFY"
	if err := os.WriteFile(sqlitePath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Import(target, ImportOptions{BundlePath: bundlePath}); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, _ := os.ReadFile(sqlitePath)
	if string(got) != sentinel {
		t.Errorf("import modified the sqlite sentinel file")
	}
	// Everything written must live under sessions/.
	for _, f := range listFilesRel(t, target.Root) {
		if f == "codex-state.sqlite" {
			continue
		}
		if !strings.HasPrefix(f, "sessions/") {
			t.Errorf("import wrote outside sessions/: %q", f)
		}
	}
}

func TestImportCWDWarningNoProject(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("hello"), "/source/proj", nil)
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ProjectProvided {
		t.Errorf("expected ProjectProvided=false")
	}
	if !anyContains(res.Warnings, "cwd filtering") {
		t.Errorf("expected sidebar/cwd visibility warning, got: %v", res.Warnings)
	}
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestImportCWDMismatchWithProject(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildBundle(t, dir, sampleRel, []byte("hello"), "/source/proj", nil)
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ProjectPath: "/different/proj"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.ProjectProvided {
		t.Errorf("expected ProjectProvided=true")
	}
	if res.CWDMismatchCount != 1 {
		t.Errorf("cwd mismatch count = %d, want 1", res.CWDMismatchCount)
	}
}
