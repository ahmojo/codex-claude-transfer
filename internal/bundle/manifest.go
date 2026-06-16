// Package bundle creates and (later) reads .codexbundle archives: a ZIP
// containing a manifest, a checksum map, and the copied Codex rollout files.
//
// A .codexbundle is a portable, hosting-free way to move local Codex sessions
// between devices. It never includes or modifies Codex's SQLite state DB.
package bundle

import "github.com/ahmojo/codex-claude-transfer/internal/git"

// FormatVersion identifies the bundle layout. Bump this on breaking changes.
const FormatVersion = "codex-sync-bundle-v1"

// Bundle file names.
const (
	ManifestName  = "manifest.json"
	ChecksumsName = "checksums.json"
)

// Manifest describes a bundle and everything inside it. It is written as
// manifest.json at the root of the ZIP.
type Manifest struct {
	FormatVersion string `json:"format_version"`
	// Tool records which coding agent the sessions came from ("codex" or
	// "claude"). It is omitted for legacy Codex bundles, which are treated as
	// "codex" on read, so older bundles remain importable unchanged.
	Tool              string            `json:"tool,omitempty"`
	CreatedAt         string            `json:"created_at"`
	CreatedByDevice   string            `json:"created_by_device,omitempty"`
	SourceOS          string            `json:"source_os"`
	SourceCodexHome   string            `json:"source_codex_home"`
	SourceProjectPath string            `json:"source_project_path,omitempty"`
	Git               *git.Info         `json:"git,omitempty"`
	CodexVersion      string            `json:"codex_version,omitempty"`
	Sessions          []ManifestSession `json:"sessions"`
}

// ManifestSession is one rollout file recorded in the manifest.
type ManifestSession struct {
	ThreadID         string `json:"thread_id,omitempty"`
	OriginalPath     string `json:"original_path"`
	BundlePath       string `json:"bundle_path"`
	OriginalCWD      string `json:"original_cwd,omitempty"`
	Preview          string `json:"preview,omitempty"`
	FirstUserMessage string `json:"first_user_message,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	Source           string `json:"source,omitempty"`
	ModelProvider    string `json:"model_provider,omitempty"`
	GitBranch        string `json:"git_branch,omitempty"`
	GitSHA           string `json:"git_sha,omitempty"`
	Archived         bool   `json:"archived"`
	Compressed       bool   `json:"compressed"`
	SizeBytes        int64  `json:"size_bytes"`
	SHA256           string `json:"sha256"`
}
