package bundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// multiSession describes one session for buildMultiBundle.
type multiSession struct {
	id      string
	cwd     string
	updated time.Time
	data    []byte
}

// buildMultiBundle writes a bundle containing several plain .jsonl sessions, each
// with its own thread id, recorded cwd, and update time — the shape a selective
// import filters over.
func buildMultiBundle(t *testing.T, dir string, sessions []multiSession) string {
	t.Helper()
	manifest := Manifest{
		FormatVersion:   FormatVersion,
		SourceOS:        "testos",
		SourceCodexHome: "/source/.codex",
	}
	entries := []rawEntry{}
	checks := map[string]string{}
	for _, s := range sessions {
		rel := "sessions/2026/06/13/rollout-2026-06-13T18-22-01-" + s.id + ".jsonl"
		sum := sha256Hex(s.data)
		manifest.Sessions = append(manifest.Sessions, ManifestSession{
			ThreadID:    s.id,
			BundlePath:  rel,
			OriginalCWD: s.cwd,
			UpdatedAt:   s.updated.UTC().Format(time.RFC3339),
			Preview:     "preview for " + s.id,
			SizeBytes:   int64(len(s.data)),
			SHA256:      sum,
		})
		entries = append(entries, rawEntry{rel, s.data})
		checks[rel] = sum
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	checks[ManifestName] = sha256Hex(manifestBytes)
	checkBytes, _ := json.MarshalIndent(Checksums(checks), "", "  ")
	entries = append(entries, rawEntry{ManifestName, manifestBytes}, rawEntry{ChecksumsName, checkBytes})

	path := filepath.Join(dir, "multi.codexbundle")
	writeBundleZip(t, path, entries)
	return path
}

func sampleSessions() []multiSession {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return []multiSession{
		{id: "1111aaaa-0000-0000-0000-000000000001", cwd: "/proj/alpha", updated: base.AddDate(0, 0, -30), data: []byte(`{"type":"session_meta"}` + "\n" + `{"message":"rate limiter tuning"}` + "\n")},
		{id: "2222bbbb-0000-0000-0000-000000000002", cwd: "/proj/alpha", updated: base.AddDate(0, 0, -1), data: []byte(`{"type":"session_meta"}` + "\n" + `{"message":"auth refactor notes"}` + "\n")},
		{id: "3333cccc-0000-0000-0000-000000000003", cwd: "/proj/beta", updated: base.AddDate(0, 0, -2), data: []byte(`{"type":"session_meta"}` + "\n" + `{"message":"beta project stuff"}` + "\n")},
	}
}

func TestImportFilterByProject(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{
		BundlePath:    bundlePath,
		ProjectPath:   "/proj/alpha",
		ProjectFilter: true,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 2 {
		t.Errorf("imported = %d, want 2 (the two /proj/alpha sessions)", res.Imported)
	}
	if res.SkippedDeselected != 1 {
		t.Errorf("skipped-deselected = %d, want 1 (the /proj/beta session)", res.SkippedDeselected)
	}
	// The beta session must not be on disk.
	for _, f := range listFilesRel(t, target.Root) {
		if strings.Contains(f, "000000000003") {
			t.Errorf("beta session was imported despite the project filter: %s", f)
		}
	}
}

func TestImportFilterBySince(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	// Cutoff 7 days before base keeps the two recent sessions, drops the 30-day-old one.
	since := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Since: since})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 2 {
		t.Errorf("imported = %d, want 2 (sessions newer than the cutoff)", res.Imported)
	}
	if res.SkippedDeselected != 1 {
		t.Errorf("skipped-deselected = %d, want 1 (the 30-day-old session)", res.SkippedDeselected)
	}
}

func TestImportFilterByMatch(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, Match: "auth refactor"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("imported = %d, want 1 (only the auth-refactor session)", res.Imported)
	}
	if res.SkippedDeselected != 2 {
		t.Errorf("skipped-deselected = %d, want 2", res.SkippedDeselected)
	}
}

func TestImportFiltersCombineAND(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	// project=alpha AND match="rate limiter" -> only session 1.
	res, err := Import(target, ImportOptions{
		BundlePath:    bundlePath,
		ProjectPath:   "/proj/alpha",
		ProjectFilter: true,
		Match:         "rate limiter",
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("imported = %d, want 1 (alpha AND rate-limiter)", res.Imported)
	}
}

func TestImportFilterNoMatchIsError(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	_, err := Import(target, ImportOptions{BundlePath: bundlePath, Match: "nonexistent xyzzy"})
	if err == nil {
		t.Fatal("expected an error when filters select no session")
	}
}

func TestImportNoFilterImportsAll(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 3 || res.SkippedDeselected != 0 {
		t.Errorf("no-filter import: imported=%d deselected=%d, want 3/0", res.Imported, res.SkippedDeselected)
	}
}

// TestImportProjectPathAdvisoryStillWorks confirms the pre-existing advisory
// behavior is preserved: ProjectPath without ProjectFilter warns on cwd mismatch
// but still imports everything (backward compatibility).
func TestImportProjectPathAdvisoryStillWorks(t *testing.T) {
	dir := t.TempDir()
	bundlePath := buildMultiBundle(t, dir, sampleSessions())
	target := fakeHome(t)

	res, err := Import(target, ImportOptions{BundlePath: bundlePath, ProjectPath: "/proj/alpha"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 3 {
		t.Errorf("advisory ProjectPath imported = %d, want 3 (no filtering)", res.Imported)
	}
	if res.CWDMismatchCount != 1 {
		t.Errorf("cwd mismatch count = %d, want 1 (the beta session)", res.CWDMismatchCount)
	}
}
