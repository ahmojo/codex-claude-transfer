package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
	"github.com/ahmojo/Codex_Sync/internal/git"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
)

// ExportOptions configures an export.
type ExportOptions struct {
	// ProjectPath, when non-empty, restricts the export to sessions whose
	// SessionMeta cwd matches this (already absolute) path. Leave empty to
	// export every session regardless of cwd (the `--all` behavior).
	ProjectPath string
	// OutputPath is the .codexbundle file to write.
	OutputPath string
	// IncludeArchived also considers archived sessions.
	IncludeArchived bool
	// Since, when non-zero, restricts the export to sessions whose file
	// modification time is at or after this instant.
	Since time.Time
}

// ExportResult summarizes what was exported.
type ExportResult struct {
	BundlePath        string
	Manifest          Manifest
	IncludedCount     int
	TotalScanned      int
	CompressedSkipped int // compressed sessions skipped by cwd filter (cwd unknown)
	Warnings          []string
}

// Export scans the Codex home, selects sessions (optionally filtered to a
// project's cwd), and writes a .codexbundle ZIP containing the rollout files,
// a manifest, and a checksum map. It never reads or writes Codex's SQLite.
func Export(home codexhome.Home, opts ExportOptions) (ExportResult, error) {
	var result ExportResult

	scan, err := sessions.Scan(home, sessions.ScanOptions{IncludeArchived: opts.IncludeArchived})
	if err != nil {
		return result, fmt.Errorf("scan sessions: %w", err)
	}
	result.TotalScanned = scan.Files
	result.Warnings = append(result.Warnings, scan.Warnings...)

	candidates := scan.Sessions
	if !opts.Since.IsZero() {
		candidates = filterSince(candidates, opts.Since)
	}

	selected, compressedSkipped, warns := selectSessions(candidates, opts.ProjectPath)
	result.CompressedSkipped = compressedSkipped
	result.Warnings = append(result.Warnings, warns...)
	if len(selected) == 0 {
		return result, fmt.Errorf("no sessions selected for export")
	}

	manifest := newManifest(home, opts)
	if opts.ProjectPath != "" {
		gi := git.Discover(opts.ProjectPath)
		if !gi.Empty() {
			manifest.Git = &gi
		}
	}

	if err := writeBundle(opts.OutputPath, selected, &manifest); err != nil {
		return result, err
	}

	result.BundlePath = opts.OutputPath
	result.Manifest = manifest
	result.IncludedCount = len(selected)
	return result, nil
}

// selectSessions filters by project cwd when projectPath is set. It returns the
// chosen sessions, how many compressed sessions were skipped because their cwd
// is unknown (not parsed in v0.1), and any warnings.
func selectSessions(all []sessions.Session, projectPath string) (selected []sessions.Session, compressedSkipped int, warnings []string) {
	if projectPath == "" {
		return all, 0, nil
	}
	for _, s := range all {
		if s.CWD != "" && pathEqual(s.CWD, projectPath) {
			selected = append(selected, s)
			continue
		}
		if s.Compressed && s.CWD == "" {
			compressedSkipped++
		}
	}
	if len(selected) == 0 {
		warnings = append(warnings, fmt.Sprintf("no sessions have a cwd matching %s", projectPath))
	}
	if compressedSkipped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d compressed session(s) skipped: cwd is unknown for .jsonl.zst in v0.1 (use --all to include them)", compressedSkipped))
	}
	return selected, compressedSkipped, warnings
}

// filterSince returns sessions whose file modification time is at or after the
// given instant. It is applied before cwd selection.
func filterSince(all []sessions.Session, since time.Time) []sessions.Session {
	var out []sessions.Session
	for _, s := range all {
		if !s.ModTime.Before(since) {
			out = append(out, s)
		}
	}
	return out
}

func newManifest(home codexhome.Home, opts ExportOptions) Manifest {
	hostname, _ := os.Hostname()
	return Manifest{
		FormatVersion:     FormatVersion,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		CreatedByDevice:   hostname,
		SourceOS:          runtime.GOOS,
		SourceCodexHome:   home.Root,
		SourceProjectPath: opts.ProjectPath,
	}
}

// writeBundle creates the ZIP atomically: it writes to a temp file in the
// destination directory and renames it into place on success.
func writeBundle(outputPath string, selected []sessions.Session, manifest *Manifest) error {
	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, ".codexbundle-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp bundle: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we fail before rename.
	defer func() {
		if tmp != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	zw := zip.NewWriter(tmp)
	checksums := Checksums{}

	for _, s := range selected {
		bundlePath := bundlePathFor(s)
		sum, err := addFileToZip(zw, bundlePath, s.Path)
		if err != nil {
			return fmt.Errorf("add %s: %w", s.Path, err)
		}
		checksums[bundlePath] = sum
		manifest.Sessions = append(manifest.Sessions, manifestSession(s, bundlePath, sum))
		if manifest.CodexVersion == "" && s.CLIVersion != "" {
			manifest.CodexVersion = s.CLIVersion
		}
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addBytesToZip(zw, ManifestName, manifestBytes); err != nil {
		return err
	}
	checksums[ManifestName] = sha256Hex(manifestBytes)

	checksumsBytes, err := json.MarshalIndent(checksums, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checksums: %w", err)
	}
	if err := addBytesToZip(zw, ChecksumsName, checksumsBytes); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize zip: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync bundle: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close bundle: %w", err)
	}
	if err := os.Rename(tmpName, outputPath); err != nil {
		return fmt.Errorf("finalize bundle: %w", err)
	}
	tmp = nil // prevent deferred cleanup
	return nil
}

func manifestSession(s sessions.Session, bundlePath, sum string) ManifestSession {
	return ManifestSession{
		ThreadID:         s.ThreadID,
		OriginalPath:     s.Path,
		BundlePath:       bundlePath,
		OriginalCWD:      s.CWD,
		Preview:          s.Preview,
		FirstUserMessage: s.FirstUserMessage,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.ModTime.UTC().Format(time.RFC3339),
		Source:           s.Source,
		ModelProvider:    s.ModelProvider,
		GitBranch:        s.GitBranch,
		GitSHA:           s.GitSHA,
		Archived:         s.Archived,
		Compressed:       s.Compressed,
		SizeBytes:        s.SizeBytes,
		SHA256:           sum,
	}
}

// bundlePathFor returns the forward-slash path inside the ZIP for a session,
// preserving the YYYY/MM/DD layout under sessions/ (or archived_sessions/).
func bundlePathFor(s sessions.Session) string {
	root := codexhome.SessionsSubdir
	if s.Archived {
		root = codexhome.ArchivedSessionsSubdir
	}
	return path.Join(root, s.RelPath)
}

// addFileToZip streams srcPath into the ZIP at bundlePath, returning its SHA-256
// computed in the same pass.
func addFileToZip(zw *zip.Writer, bundlePath, srcPath string) (string, error) {
	w, err := zw.Create(bundlePath)
	if err != nil {
		return "", err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func addBytesToZip(zw *zip.Writer, bundlePath string, data []byte) error {
	w, err := zw.Create(bundlePath)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// pathEqual compares two filesystem paths after cleaning, case-insensitively on
// Windows (matching the OS's path semantics).
func pathEqual(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}
