package bundle

import (
	"archive/zip"
	"io"
	"path"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/secrets"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// SecretScanResult summarizes a scan of a bundle's session contents.
type SecretScanResult struct {
	SessionsWithSecrets int // number of session entries containing >=1 finding
	TotalFindings       int // total secret findings across all sessions
}

// Any reports whether the bundle contains any likely secret.
func (r SecretScanResult) Any() bool { return r.TotalFindings > 0 }

// ScanBundleSecrets opens a (plaintext) bundle and scans each session entry for
// likely secrets, so the pre-egress gate can refuse to export/sync credentials
// unless the user opts in. It scans the exact bytes that would leave the machine.
// Compressed entries are decompressed when zstd is available and skipped (counted
// as no findings) when it is not.
func ScanBundleSecrets(bundlePath string) (SecretScanResult, error) {
	var res SecretScanResult
	zr, err := zip.OpenReader(bundlePath)
	if err != nil {
		return res, err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if !isSessionEntry(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return res, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return res, err
		}
		if strings.HasSuffix(f.Name, compressedSessionSuffix) {
			if !zstdcli.Available() {
				continue
			}
			plain, derr := zstdcli.Decompress(data)
			if derr != nil {
				continue
			}
			data = plain
		}
		if found := secrets.Scan(data); len(found) > 0 {
			res.SessionsWithSecrets++
			res.TotalFindings += len(found)
		}
	}
	return res, nil
}

// isSessionEntry reports whether a ZIP entry path carries session content
// (under sessions/, archived_sessions/, or projects/ — including a project's
// auto-memory files) rather than the manifest or checksums file. Everything it
// accepts is read by the pre-egress secret gate.
func isSessionEntry(name string) bool {
	// Memory files are ordinary notes and need not have an extension, so they
	// are recognized by shape rather than by the "has a file extension" rule the
	// transcript paths can rely on.
	if safety.IsClaudeMemoryEntry(name) {
		return true
	}
	top := name
	if i := strings.IndexByte(name, '/'); i >= 0 {
		top = name[:i]
	}
	switch top {
	case codexhome.SessionsSubdir, codexhome.ArchivedSessionsSubdir, claudehome.ProjectsSubdir:
		return path.Ext(name) != "" // a file, not the directory entry
	default:
		return false
	}
}
