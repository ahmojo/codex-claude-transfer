// Package safety provides path validation and atomic file writes used when
// importing bundles. The goal is to never let a malicious or malformed bundle
// write outside the Codex sessions directory, and to never partially overwrite
// a file.
package safety

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// sessionEntryRe matches the only Codex bundle paths we will import: rollout
// files under sessions/YYYY/MM/DD/. Compressed (.jsonl.zst) variants are allowed.
var sessionEntryRe = regexp.MustCompile(`^sessions/\d{4}/\d{2}/\d{2}/rollout-[^/]+\.jsonl(\.zst)?$`)

// claudeEntryRe matches the only Claude Code bundle paths we will import:
// transcripts under projects/<encoded-cwd>/<uuid>.jsonl. The encoded folder and
// the uuid are single path segments (no nested directories, no traversal).
var claudeEntryRe = regexp.MustCompile(`^projects/[^/]+/[^/]+\.jsonl$`)

// CleanRelPath validates a ZIP entry name and returns it as a safe, canonical,
// forward-slash relative path. It rejects anything that could escape the
// destination: absolute paths, Windows drive/volume prefixes, backslashes,
// empty or "."/".." segments, and non-canonical forms.
func CleanRelPath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("backslash in path %q", name)
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute path %q", name)
	}
	if strings.ContainsRune(name, ':') {
		return "", fmt.Errorf("volume/drive in path %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("illegal path segment in %q", name)
		}
	}
	if cleaned := path.Clean(name); cleaned != name {
		return "", fmt.Errorf("non-canonical path %q", name)
	}
	return name, nil
}

// IsSessionEntry reports whether a (already cleaned) relative path is a Codex
// rollout file under sessions/YYYY/MM/DD/.
func IsSessionEntry(rel string) bool {
	return sessionEntryRe.MatchString(rel)
}

// IsClaudeSessionEntry reports whether a (already cleaned) relative path is a
// Claude Code transcript under projects/<encoded-cwd>/<uuid>.jsonl.
func IsClaudeSessionEntry(rel string) bool {
	return claudeEntryRe.MatchString(rel)
}

// DestPath joins a cleaned relative bundle path onto the Codex home root and
// verifies, as defense in depth, that the result stays within root.
func DestPath(root, rel string) (string, error) {
	dest := filepath.Join(root, filepath.FromSlash(rel))
	rootClean := filepath.Clean(root)
	if dest != rootClean && !strings.HasPrefix(dest, rootClean+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes Codex home", rel)
	}
	return dest, nil
}
