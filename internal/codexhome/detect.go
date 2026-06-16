// Package codexhome resolves the location of the local Codex home directory
// (~/.codex) and the session storage directories inside it.
//
// cct never modifies Codex's SQLite state DB. This package only locates
// directories; it does not create or write anything.
package codexhome

import (
	"os"
	"path/filepath"
)

const (
	// DefaultDirName is the conventional Codex home directory name under the
	// user's home directory.
	DefaultDirName = ".codex"

	// SessionsSubdir matches Codex's SESSIONS_SUBDIR constant.
	SessionsSubdir = "sessions"

	// ArchivedSessionsSubdir matches Codex's ARCHIVED_SESSIONS_SUBDIR constant.
	ArchivedSessionsSubdir = "archived_sessions"
)

// Home describes a resolved Codex home directory and its session directories.
// Paths are always populated (computed from Root); use the Exists helpers to
// check what is actually present on disk.
type Home struct {
	// Root is the absolute path to the Codex home directory (e.g. ~/.codex).
	Root string
	// SessionsDir is Root/sessions.
	SessionsDir string
	// ArchivedSessionsDir is Root/archived_sessions.
	ArchivedSessionsDir string
	// Source records how Root was resolved: "flag", "env", or "default".
	Source string
}

// Detect resolves the Codex home directory using, in order of precedence:
//
//  1. the override argument (the --codex-home CLI flag), when non-empty;
//  2. the CODEX_HOME environment variable, when set;
//  3. <user home>/.codex.
//
// Detect does not require any of the paths to exist; existence reporting is the
// job of the doctor command. It only fails if it cannot determine a base path
// at all (e.g. no override, no CODEX_HOME, and no resolvable user home).
func Detect(override string) (Home, error) {
	root, source, err := resolveRoot(override)
	if err != nil {
		return Home{}, err
	}
	return newHome(root, source), nil
}

func resolveRoot(override string) (root, source string, err error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", "", err
		}
		return abs, "flag", nil
	}
	if env := os.Getenv("CODEX_HOME"); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", "", err
		}
		return abs, "env", nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(userHome, DefaultDirName), "default", nil
}

func newHome(root, source string) Home {
	return Home{
		Root:                root,
		SessionsDir:         filepath.Join(root, SessionsSubdir),
		ArchivedSessionsDir: filepath.Join(root, ArchivedSessionsSubdir),
		Source:              source,
	}
}

// RootExists reports whether the Codex home root directory exists.
func (h Home) RootExists() bool { return dirExists(h.Root) }

// SessionsDirExists reports whether the active sessions directory exists.
func (h Home) SessionsDirExists() bool { return dirExists(h.SessionsDir) }

// ArchivedSessionsDirExists reports whether the archived sessions directory exists.
func (h Home) ArchivedSessionsDirExists() bool { return dirExists(h.ArchivedSessionsDir) }

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
