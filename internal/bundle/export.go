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

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// ExportOptions configures an export.
type ExportOptions struct {
	// Tool selects which agent's sessions to export ("" / agent.Codex scans the
	// Codex home; agent.Claude scans ClaudeHome). The chosen tool is recorded in
	// the manifest so import knows how to place the files back.
	Tool agent.Kind
	// ClaudeHome is the resolved Claude Code home, used only when Tool is
	// agent.Claude. (The Codex home is passed to Export directly.)
	ClaudeHome claudehome.Home
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
	// SessionID, when non-empty, exports exactly the single session whose
	// thread id equals (or uniquely begins with) this value, regardless of
	// cwd. It is mutually exclusive with project/all filtering.
	SessionID string
	// OnlyThreadIDs, when non-empty, selects exactly the sessions whose thread id
	// is in this set (exact match), bypassing project/all/since/single selection.
	// It is used by LAN sync to bundle precisely the sessions a peer is missing.
	OnlyThreadIDs []string
	// WithGit forces capture of the project's git metadata (remote, branch,
	// commit, dirty/unpushed) into the manifest even when ProjectPath is empty
	// (e.g. with --all or --session). When ProjectPath is set, git metadata is
	// always captured regardless of this flag.
	WithGit bool
	// StripImages replaces inline base64 image payloads in each session with a
	// short placeholder before adding it to the bundle. It is lossy (the picture
	// bytes are dropped) and opt-in, used to shrink image-heavy bundles. The
	// conversation text is preserved; compressed .jsonl.zst sessions are stripped
	// too when the zstd tool is available (otherwise copied as-is with a warning).
	StripImages bool
}

// ExportResult summarizes what was exported.
type ExportResult struct {
	BundlePath        string
	Manifest          Manifest
	IncludedCount     int
	TotalScanned      int
	CompressedSkipped int   // compressed sessions skipped by cwd filter (cwd unknown)
	ImagesStripped    int   // images replaced when StripImages is set
	BytesSaved        int64 // bundle bytes saved by stripping (original minus stored)
	Warnings          []string
}

// Export scans the Codex home, selects sessions (optionally filtered to a
// project's cwd), and writes a .codexbundle ZIP containing the rollout files,
// a manifest, and a checksum map. It never reads or writes Codex's SQLite.
func Export(home codexhome.Home, opts ExportOptions) (ExportResult, error) {
	var result ExportResult
	kind := agent.Normalize(opts.Tool)

	var scan sessions.ScanResult
	var err error
	if kind == agent.Claude {
		scan, err = claudesessions.Scan(opts.ClaudeHome, claudesessions.ScanOptions{
			IncludeArchived: opts.IncludeArchived,
		})
	} else {
		scan, err = sessions.Scan(home, sessions.ScanOptions{
			IncludeArchived:      opts.IncludeArchived,
			DecompressCompressed: true,
		})
	}
	if err != nil {
		return result, fmt.Errorf("scan sessions: %w", err)
	}
	result.TotalScanned = scan.Files
	result.Warnings = append(result.Warnings, scan.Warnings...)

	candidates := scan.Sessions
	if !opts.Since.IsZero() {
		candidates = filterSince(candidates, opts.Since)
	}

	var selected []sessions.Session
	if len(opts.OnlyThreadIDs) > 0 {
		selected = selectByThreadIDSet(candidates, opts.OnlyThreadIDs)
	} else if opts.SessionID != "" {
		selected, err = selectByThreadID(candidates, opts.SessionID)
		if err != nil {
			return result, err
		}
	} else {
		var compressedSkipped int
		var warns []string
		selected, compressedSkipped, warns = selectSessions(candidates, opts.ProjectPath)
		result.CompressedSkipped = compressedSkipped
		result.Warnings = append(result.Warnings, warns...)
	}
	if len(selected) == 0 {
		return result, fmt.Errorf("no sessions selected for export")
	}

	manifest := newManifest(home, opts)
	if kind == agent.Claude {
		manifest.Tool = string(agent.Claude)
		manifest.SourceCodexHome = opts.ClaudeHome.Root
	}
	gi, gitWarns := captureGit(opts, selected)
	if gi != nil {
		manifest.Git = gi
	}
	// Append git warnings whether or not metadata was found: the "no repository
	// found" notice is itself returned with a nil Info, and must not be dropped.
	result.Warnings = append(result.Warnings, gitWarns...)

	if err := writeBundle(opts, selected, &manifest, kind, &result); err != nil {
		return result, err
	}

	result.BundlePath = opts.OutputPath
	result.Manifest = manifest
	result.IncludedCount = len(selected)
	return result, nil
}

// selectSessions filters by project cwd when projectPath is set. It returns the
// chosen sessions, how many compressed sessions were skipped because their cwd
// is unknown (could not be recovered), and any warnings.
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

// selectByThreadIDSet returns the sessions whose thread id is in the given set
// (exact match), preserving scan order. Unknown ids are silently ignored.
func selectByThreadIDSet(all []sessions.Session, ids []string) []sessions.Session {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			want[id] = true
		}
	}
	var out []sessions.Session
	for _, s := range all {
		if s.ThreadID != "" && want[s.ThreadID] {
			out = append(out, s)
		}
	}
	return out
}

// selectByThreadID returns the single session whose thread id equals idPrefix,
// or, failing an exact match, the single session whose thread id begins with
// idPrefix. An empty prefix, no match, or an ambiguous prefix is an error.
func selectByThreadID(all []sessions.Session, idPrefix string) ([]sessions.Session, error) {
	if idPrefix == "" {
		return nil, fmt.Errorf("empty thread id")
	}
	var prefixMatches []sessions.Session
	for _, s := range all {
		if s.ThreadID == idPrefix {
			return []sessions.Session{s}, nil
		}
		if s.ThreadID != "" && strings.HasPrefix(s.ThreadID, idPrefix) {
			prefixMatches = append(prefixMatches, s)
		}
	}
	switch len(prefixMatches) {
	case 0:
		return nil, fmt.Errorf("no session matches thread id %q", idPrefix)
	case 1:
		return prefixMatches, nil
	default:
		return nil, fmt.Errorf("thread id %q is ambiguous: it matches %d sessions (use a longer prefix)", idPrefix, len(prefixMatches))
	}
}

// captureGit discovers git metadata for the export when appropriate. It returns
// nil when there is no repository to describe. Discovery runs against the
// explicit project path when given, otherwise (with --with-git) against the
// recorded cwd of the selected sessions. It also returns warnings when the
// captured commit is dirty or unpushed, since the other machine then cannot
// reproduce or fetch the exact state.
func captureGit(opts ExportOptions, selected []sessions.Session) (*git.Info, []string) {
	dir := opts.ProjectPath
	if dir == "" {
		if !opts.WithGit {
			return nil, nil
		}
		dir = firstCWD(selected)
		if dir == "" {
			return nil, []string{"--with-git: no session has a recorded cwd to discover git metadata from"}
		}
	}
	gi := git.Discover(dir)
	if gi.Empty() {
		// Nothing was found. Stay quiet for automatic discovery (a plain
		// --project export), but when the user explicitly opted into git
		// (--with-git), tell them why no git metadata was recorded instead of
		// silently producing a bundle with no git block.
		if opts.WithGit {
			if !git.Available() {
				return nil, []string{"--with-git: git is not installed or not on PATH; no git metadata was recorded"}
			}
			return nil, []string{fmt.Sprintf("--with-git: %s is not a git repository; no git metadata was recorded", dir)}
		}
		return nil, nil
	}
	var warns []string
	if gi.RemoteURL == "" {
		warns = append(warns, "git: no remote configured; the other machine has no URL to clone or fetch from")
	}
	if gi.Dirty {
		warns = append(warns, "git: the working tree has uncommitted changes; the recorded commit does not capture the exact state")
	}
	if gi.Unpushed {
		warns = append(warns, fmt.Sprintf("git: commit %s is not on any remote; push it first or the other machine cannot fetch it", shortSHA(gi.CommitSHA)))
	}
	return &gi, warns
}

// firstCWD returns the recorded cwd of the first selected session that has one.
func firstCWD(selected []sessions.Session) string {
	for _, s := range selected {
		if s.CWD != "" {
			return s.CWD
		}
	}
	return ""
}

// shortSHA abbreviates a commit SHA for human-readable messages.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
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
func writeBundle(opts ExportOptions, selected []sessions.Session, manifest *Manifest, kind agent.Kind, result *ExportResult) error {
	outputPath := opts.OutputPath
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
		bundlePath := bundlePathFor(s, kind)
		sum := ""
		size := s.SizeBytes
		if opts.StripImages {
			var nImg int
			var warn string
			sum, size, nImg, warn, err = addStrippedSessionToZip(zw, bundlePath, s)
			if err != nil {
				return fmt.Errorf("add %s: %w", s.Path, err)
			}
			if warn != "" {
				result.Warnings = append(result.Warnings, warn)
			}
			result.ImagesStripped += nImg
			if saved := s.SizeBytes - size; saved > 0 {
				result.BytesSaved += saved
			}
		} else {
			sum, err = addFileToZip(zw, bundlePath, s.Path)
			if err != nil {
				return fmt.Errorf("add %s: %w", s.Path, err)
			}
		}
		checksums[bundlePath] = sum
		ms := manifestSession(s, bundlePath, sum)
		ms.SizeBytes = size
		manifest.Sessions = append(manifest.Sessions, ms)
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

// bundlePathFor returns the forward-slash path inside the ZIP for a session. For
// Codex it preserves the YYYY/MM/DD layout under sessions/ (or archived_sessions/);
// for Claude Code it preserves the projects/<encoded-cwd>/<uuid>.jsonl layout.
func bundlePathFor(s sessions.Session, kind agent.Kind) string {
	if agent.Normalize(kind) == agent.Claude {
		return path.Join(claudehome.ProjectsSubdir, s.RelPath)
	}
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

// addStrippedSessionToZip reads a session, removes inline base64 images, and adds
// the rewritten bytes to the ZIP. It returns the stored content's SHA-256, its
// stored size, the number of images stripped, and (for a compressed session that
// cannot be stripped because zstd is unavailable) a warning; in that case the file
// is copied as-is. A .jsonl.zst session is decompressed, stripped, and recompressed
// so it stays in the same on-disk format.
func addStrippedSessionToZip(zw *zip.Writer, bundlePath string, s sessions.Session) (sum string, sizeOut int64, imagesStripped int, warn string, err error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return "", 0, 0, "", err
	}
	compressed := s.Compressed || strings.HasSuffix(s.Path, compressedSessionSuffix)

	plain := raw
	if compressed {
		if !zstdcli.Available() {
			if err := addBytesToZip(zw, bundlePath, raw); err != nil {
				return "", 0, 0, "", err
			}
			return sha256Hex(raw), int64(len(raw)), 0,
				fmt.Sprintf("%s: compressed session not image-stripped (zstd not installed); copied as-is", bundlePath), nil
		}
		if plain, err = zstdcli.Decompress(raw); err != nil {
			return "", 0, 0, "", fmt.Errorf("decompress: %w", err)
		}
	}

	stripped, n := StripImagesJSONL(plain)

	outBytes := stripped
	if compressed {
		if outBytes, err = zstdcli.Compress(stripped); err != nil {
			return "", 0, 0, "", fmt.Errorf("recompress: %w", err)
		}
	}
	if err := addBytesToZip(zw, bundlePath, outBytes); err != nil {
		return "", 0, 0, "", err
	}
	return sha256Hex(outBytes), int64(len(outBytes)), n, "", nil
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
