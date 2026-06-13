// Package sessions discovers and defensively parses Codex rollout files.
//
// Codex stores each session as a JSONL rollout file under
// ~/.codex/sessions/YYYY/MM/DD/. Files may be plain (.jsonl) or zstd-compressed
// (.jsonl.zst). The JSONL format is Codex's durable, canonical record; SQLite is
// only an index. codex-sync therefore treats these rollout files as the source
// of truth and never touches SQLite.
//
// The Codex on-disk format can change. All parsing here is best-effort and
// defensive: unknown fields are ignored, invalid lines are reported as warnings
// rather than fatal errors, and a single bad file never aborts a whole scan.
package sessions

import "time"

const (
	// FilePrefix is the rollout filename prefix.
	FilePrefix = "rollout-"
	// PlainExt is the uncompressed rollout extension.
	PlainExt = ".jsonl"
	// CompressedExt is the zstd-compressed rollout extension.
	CompressedExt = ".jsonl.zst"
)

// Session is a single discovered rollout file plus any metadata that could be
// extracted from it. Metadata fields are best-effort and may be empty when a
// file is compressed (not parsed in v0.1) or when parsing was incomplete.
type Session struct {
	// File location.
	Path     string // absolute path on this device
	RelPath  string // path relative to the sessions root (e.g. 2026/06/13/rollout-...jsonl)
	FileName string

	// File-level facts (always populated).
	Compressed bool // true for .jsonl.zst
	Archived   bool // true if found under archived_sessions/
	SizeBytes  int64
	ModTime    time.Time // file modification time; used as updated_at

	// Best-effort metadata parsed from the rollout contents.
	ThreadID         string
	CWD              string
	Originator       string
	CLIVersion       string
	Source           string // e.g. "cli", "appServer"
	ModelProvider    string
	GitBranch        string
	GitSHA           string
	GitOriginURL     string
	CreatedAt        string // SessionMeta timestamp, or derived from filename
	FirstUserMessage string
	Preview          string

	// Parsing status.
	Parsed   bool     // true if a SessionMeta line was found and parsed
	Warnings []string // non-fatal parsing warnings
}

// UpdatedAt returns the file modification time, which Codex uses as the
// session's updated_at.
func (s Session) UpdatedAt() time.Time { return s.ModTime }

// ScanResult aggregates the outcome of scanning a Codex home for sessions.
type ScanResult struct {
	Sessions []Session
	// Files is the total number of rollout files detected (valid or not).
	Files int
	// Valid is the number of sessions whose SessionMeta parsed successfully.
	Valid int
	// Invalid is the number of files that produced no usable SessionMeta.
	Invalid int
	// Compressed is the number of .jsonl.zst files detected (not parsed in v0.1).
	Compressed int
	// Warnings are scan-level warnings (e.g. unreadable directories).
	Warnings []string
}
