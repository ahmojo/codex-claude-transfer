// Package claudehome resolves the location of the local Claude Code home
// directory (~/.claude) and the per-project session store inside it.
//
// Claude Code stores each session as a JSONL transcript under
// ~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl. The encoded folder name
// is the project's working directory with every character outside [A-Za-z0-9_-]
// replaced by '-' (verified empirically against a live install; see
// docs/research/claude-code-sessions-investigation.md). The encoding is lossy, so
// the per-line "cwd" field — not the folder name — is the source of truth for the
// real project path.
//
// Like the codexhome package, this only locates directories and never writes
// anything. Claude Code rediscovers a dropped-in transcript on its next run with
// no registry/database to update (also verified empirically).
package claudehome

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultDirName is the conventional Claude Code home directory name under
	// the user's home directory.
	DefaultDirName = ".claude"

	// ProjectsSubdir is the directory under the home that groups per-project
	// session transcripts.
	ProjectsSubdir = "projects"

	// SessionExt is the transcript file extension.
	SessionExt = ".jsonl"

	// MemorySubdir is the per-project directory Claude Code keeps its auto
	// memory in, alongside that project's transcripts:
	// ~/.claude/projects/<encoded-cwd>/memory/. It is keyed by the same encoded
	// path as the transcripts, so a project that moves leaves its memory behind
	// unless that directory moves with it.
	MemorySubdir = "memory"
)

// Home describes a resolved Claude Code home directory and its session store.
type Home struct {
	// Root is the absolute path to the Claude home directory (e.g. ~/.claude).
	Root string
	// ProjectsDir is Root/projects.
	ProjectsDir string
	// Source records how Root was resolved: "flag", "env", or "default".
	Source string
}

// Detect resolves the Claude Code home directory using, in order of precedence:
//
//  1. the override argument (the --claude-home CLI flag), when non-empty;
//  2. the CLAUDE_HOME environment variable, when set;
//  3. <user home>/.claude.
//
// Detect does not require any of the paths to exist; existence reporting is the
// doctor command's job.
func Detect(override string) (Home, error) {
	root, source, err := resolveRoot(override)
	if err != nil {
		return Home{}, err
	}
	return Home{
		Root:        root,
		ProjectsDir: filepath.Join(root, ProjectsSubdir),
		Source:      source,
	}, nil
}

func resolveRoot(override string) (root, source string, err error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", "", err
		}
		return abs, "flag", nil
	}
	if env := os.Getenv("CLAUDE_HOME"); env != "" {
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

// RootExists reports whether the Claude home root directory exists.
func (h Home) RootExists() bool { return dirExists(h.Root) }

// ProjectsDirExists reports whether the projects directory exists.
func (h Home) ProjectsDirExists() bool { return dirExists(h.ProjectsDir) }

// ProjectDir returns the folder Claude Code keeps a project's files in:
// <home>/projects/<encoded-cwd>.
func (h Home) ProjectDir(cwd string) string {
	return filepath.Join(h.ProjectsDir, EncodeCWD(cwd))
}

// MemoryDir returns a project's auto-memory directory inside its project folder.
// It need not exist: a project only gets one once Claude Code has something to
// remember about it. Because it is keyed by the same encoded path as the
// transcripts, a project that moves leaves its memory behind unless this
// directory moves too.
func (h Home) MemoryDir(cwd string) string {
	return filepath.Join(h.ProjectDir(cwd), MemorySubdir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// EncodeCWD converts a project working directory into the folder name Claude Code
// uses under projects/. Every character that is not a letter, digit, or '-'
// becomes '-', per character; letters (case preserved), digits and '-' pass
// through. Notably '_' is NOT preserved — it becomes '-' too (verified against a
// live install: this project's own ~/.claude/projects folder is
// "C--Users-faruk-Documents-Codex-sync" for "C:\\Users\\faruk\\Documents\\Codex_sync").
//
// The transform is intentionally lossy and not reversible (a '-' could have come
// from '\\', '/', ':', '.', '_', a space, ...), exactly like Claude Code's own
// encoding — so two different real paths can map to the same folder, and the
// per-line cwd remains the authoritative project path.
func EncodeCWD(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
