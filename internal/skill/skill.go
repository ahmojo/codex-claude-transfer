// Package skill ships cct's agent workflow document: a Claude Code skill file
// that teaches a coding agent to carry a project's session history inside the
// project's own git repository (export into .cct/, commit, import after a
// clone).
//
// The document is embedded in the binary so `cct skill install` is a single
// file write into the agent's own home. Installing writes nothing else: cct
// still never touches Codex's SQLite index or Claude Code's ~/.claude.json.
package skill

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

//go:embed SKILL.md
var document string

// Name is the skill's directory name and its frontmatter name.
const Name = "cct-session-sync"

// FileName is the file Claude Code reads inside a skill directory.
const FileName = "SKILL.md"

// SkillsSubdir is where Claude Code looks for user skills within its home.
const SkillsSubdir = "skills"

// Document returns the full skill file: YAML frontmatter plus instructions.
func Document() string { return document }

// Body returns the instructions without the YAML frontmatter, for agents that
// take plain instruction text instead of a skill file (Codex's AGENTS.md, for
// example).
func Body() string {
	const fence = "---\n"
	rest, ok := strings.CutPrefix(strings.ReplaceAll(document, "\r\n", "\n"), fence)
	if !ok {
		return document
	}
	_, body, ok := strings.Cut(rest, "\n"+fence)
	if !ok {
		return document
	}
	return strings.TrimLeft(body, "\n")
}

// Dir returns the skill's directory inside a Claude Code home root.
func Dir(claudeRoot string) string {
	return filepath.Join(claudeRoot, SkillsSubdir, Name)
}

// Path returns the skill file's full path inside a Claude Code home root.
func Path(claudeRoot string) string { return filepath.Join(Dir(claudeRoot), FileName) }

// Install statuses.
const (
	// StatusInstalled means the file did not exist and was written.
	StatusInstalled = "installed"
	// StatusUpdated means a different file was replaced (a backup was kept).
	StatusUpdated = "updated"
	// StatusUnchanged means the installed file already matches this cct.
	StatusUnchanged = "unchanged"
)

// ErrDiffers reports an existing skill file whose contents differ from the one
// this cct ships. It is returned instead of overwriting, because the file may
// be the user's own edit; --force resolves it.
var ErrDiffers = errors.New("a different SKILL.md is already installed")

// Options controls Install.
type Options struct {
	// Force replaces an existing, differing file (keeping a backup).
	Force bool
	// DryRun reports what would happen and writes nothing.
	DryRun bool
}

// Result describes what Install did (or would do with DryRun).
type Result struct {
	// Path is the skill file's location.
	Path string
	// Status is one of the Status* constants.
	Status string
	// Backup is the path of the replaced file's backup copy, when one was made.
	Backup string
	// DryRun echoes Options.DryRun, so callers can word their output.
	DryRun bool
}

// Install writes the embedded skill into claudeRoot. It creates the skill
// directory as needed and writes atomically, so a failed write never leaves a
// half-written skill behind.
//
// An existing file is only replaced when its contents differ AND Force is set,
// and the previous contents are copied to a timestamped backup first. Anything
// at the target path that is not a regular file (a directory, a symlink) is
// refused rather than followed.
func Install(claudeRoot string, opts Options) (Result, error) {
	if claudeRoot == "" {
		return Result{}, errors.New("no Claude Code home given")
	}
	path := Path(claudeRoot)
	res := Result{Path: path, DryRun: opts.DryRun}

	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Status = StatusInstalled
	case err != nil:
		return res, err
	case !info.Mode().IsRegular():
		return res, fmt.Errorf("%s exists and is not a regular file; refusing to replace it", path)
	default:
		existing, rerr := os.ReadFile(path)
		if rerr != nil {
			return res, rerr
		}
		if bytes.Equal(normalize(existing), normalize([]byte(document))) {
			res.Status = StatusUnchanged
			return res, nil
		}
		if !opts.Force {
			return res, fmt.Errorf("%w at %s", ErrDiffers, path)
		}
		res.Status = StatusUpdated
	}

	if opts.DryRun {
		return res, nil
	}

	if res.Status == StatusUpdated {
		backup, berr := backupFile(path)
		if berr != nil {
			return res, fmt.Errorf("cannot back up the existing skill: %w", berr)
		}
		res.Backup = backup
	}
	if err := safety.CopyAtomic(path, strings.NewReader(document)); err != nil {
		return res, err
	}
	return res, nil
}

// normalize makes the comparison of an installed file to the embedded one
// insensitive to line endings, so a checkout or editor that rewrote CRLF does
// not look like a user edit.
func normalize(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// backupFile copies path to a sibling ".cct-bak-<unix-nanos>" file, matching the
// backup naming the import path uses, and returns the backup's path.
func backupFile(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer src.Close()
	backup := fmt.Sprintf("%s.cct-bak-%d", path, time.Now().UnixNano())
	if err := safety.CopyAtomic(backup, src); err != nil {
		return "", err
	}
	return backup, nil
}
