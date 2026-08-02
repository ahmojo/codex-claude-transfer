package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
)

// A session store is a second, private git repository that holds the bundles
// for many projects, instead of keeping each project's bundle inside the
// project's own repo. The project repo then carries only a small reference
// file, .cct/sessions.json, which tells an agent (or a person) where the
// history lives and how it is organized.
//
// The reference deliberately records nothing machine-specific: no local paths,
// no keys beyond an age *recipient* (a public key). Where the store is cloned
// on this machine is the user's own config (repo-sync-dir), so committing the
// reference leaks nothing about the developer's filesystem.

// RefSubdir is the directory the reference files live in, inside a project.
const RefSubdir = ".cct"

// RefFileName is the machine-readable reference file within RefSubdir.
const RefFileName = "sessions.json"

// RefReadmeName is the human-readable companion written next to it.
const RefReadmeName = "README.md"

// RefVersion is the reference file's schema version.
const RefVersion = 1

// Encryption modes recorded in a reference.
const (
	EncryptionPlain = "plain"
	EncryptionAge   = "age"
)

// DefaultStoreDir is the conventional local clone path for the session store,
// used when the user configured none.
const DefaultStoreDir = "~/cct-sessions"

// Reference is the content of .cct/sessions.json: where this project's session
// history is kept and how it is stored.
type Reference struct {
	Version int `json:"version"`
	// Note is a sentence for whoever opens the file without context.
	Note string `json:"note,omitempty"`
	// Repo is the session store's git remote (a private repository).
	Repo string `json:"repo"`
	// Project is this project's folder name inside the store.
	Project string `json:"project"`
	// Path is the project's full path within the store, i.e. projects/<Project>.
	Path string `json:"path"`
	// Encryption is "plain" or "age".
	Encryption string `json:"encryption"`
	// Recipient is the age recipient bundles are encrypted to. A recipient is a
	// public key: it can encrypt, never decrypt, so it is safe to commit.
	Recipient string `json:"recipient,omitempty"`
}

const refNote = "This project's Codex/Claude Code session history lives in the private git repo named below, not in this repo. See .cct/README.md, or run `cct skill show`."

// ProjectsSubdir is the store directory that holds one folder per project.
const ProjectsSubdir = "projects"

// NewReference builds a reference for a project directory, filling in the
// derived and default fields. Repo must be a git remote; project may be empty
// to derive a slug from the directory name.
func NewReference(projectDir, repo, project, encryption, recipient string) (Reference, error) {
	if err := git.ValidateRemoteURL(repo); err != nil {
		return Reference{}, err
	}
	if strings.ContainsAny(repo, "\r\n") {
		return Reference{}, errors.New("the store repo URL contains a line break")
	}
	if project == "" {
		project = Slug(filepath.Base(strings.TrimRight(projectDir, `/\`)))
	} else {
		project = Slug(project)
	}
	if project == "" {
		return Reference{}, errors.New("cannot derive a project name; pass one explicitly")
	}
	switch encryption {
	case "", EncryptionPlain:
		encryption = EncryptionPlain
		recipient = ""
	case EncryptionAge:
		if recipient == "" {
			return Reference{}, errors.New("encrypted mode needs an age recipient (cct config set repo-sync-recipient age1...)")
		}
	default:
		return Reference{}, fmt.Errorf("unknown encryption mode %q (want %s or %s)", encryption, EncryptionPlain, EncryptionAge)
	}
	return Reference{
		Version:    RefVersion,
		Note:       refNote,
		Repo:       repo,
		Project:    project,
		Path:       ProjectsSubdir + "/" + project,
		Encryption: encryption,
		Recipient:  recipient,
	}, nil
}

// Slug reduces a name to the characters that are safe in a path segment on
// every supported platform, so a project folder name never needs quoting.
func Slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.' || r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Validate checks a reference that was read from disk. It is deliberately
// strict: the file comes from a repository, which means it can be written by
// anyone who can open a pull request.
func (r Reference) Validate() error {
	if r.Version <= 0 || r.Version > RefVersion {
		return fmt.Errorf("unsupported reference version %d (this cct understands up to %d)", r.Version, RefVersion)
	}
	if err := git.ValidateRemoteURL(r.Repo); err != nil {
		return err
	}
	// The values are printed to a terminal and pasted into commands, and they
	// arrive from a file anyone with commit access can write, so control
	// characters (ANSI escapes, line breaks) are refused outright.
	for field, v := range map[string]string{"repo": r.Repo, "recipient": r.Recipient, "path": r.Path} {
		if i := strings.IndexFunc(v, isControl); i >= 0 {
			return fmt.Errorf("the reference's %s contains a control character at byte %d", field, i)
		}
	}
	if r.Project == "" || Slug(r.Project) != r.Project {
		return fmt.Errorf("invalid project name %q in the reference", r.Project)
	}
	want := ProjectsSubdir + "/" + r.Project
	if r.Path != "" && r.Path != want {
		return fmt.Errorf("reference path %q does not match project %q (want %q)", r.Path, r.Project, want)
	}
	switch r.Encryption {
	case EncryptionPlain, EncryptionAge:
	default:
		return fmt.Errorf("unknown encryption mode %q in the reference", r.Encryption)
	}
	if r.Encryption == EncryptionAge && r.Recipient == "" {
		return errors.New("the reference says encrypted but names no recipient")
	}
	if strings.HasPrefix(r.Recipient, "AGE-SECRET-KEY-") {
		return errors.New("the reference contains an age PRIVATE key; treat it as compromised and rotate it")
	}
	return nil
}

// RefPath and RefReadmePath locate the two reference files in a project.
func RefPath(projectDir string) string {
	return filepath.Join(projectDir, RefSubdir, RefFileName)
}

// RefReadmePath is the reference's human-readable companion.
func RefReadmePath(projectDir string) string {
	return filepath.Join(projectDir, RefSubdir, RefReadmeName)
}

// ReadReference loads and validates a project's reference file.
func ReadReference(projectDir string) (Reference, error) {
	var ref Reference
	data, err := os.ReadFile(RefPath(projectDir))
	if err != nil {
		return ref, err
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		return ref, fmt.Errorf("%s is not valid JSON: %w", RefPath(projectDir), err)
	}
	if err := ref.Validate(); err != nil {
		return ref, err
	}
	return ref, nil
}

// WriteReference writes .cct/sessions.json and .cct/README.md into projectDir.
// An existing reference that differs is only replaced with Force, so a stray
// `skill init` cannot silently repoint a repo's history at another store.
func WriteReference(projectDir string, ref Reference, opts Options) (Result, error) {
	if err := ref.Validate(); err != nil {
		return Result{}, err
	}
	path := RefPath(projectDir)
	res := Result{Path: path, DryRun: opts.DryRun}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return res, err
	}
	data = append(data, '\n')

	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Status = StatusInstalled
	case err != nil:
		return res, err
	case normText(existing) == normText(data):
		res.Status = StatusUnchanged
	case !opts.Force:
		return res, fmt.Errorf("%w at %s", ErrRefDiffers, path)
	default:
		res.Status = StatusUpdated
	}

	if opts.DryRun {
		return res, nil
	}
	if res.Status != StatusUnchanged {
		if err := safety.CopyAtomic(path, strings.NewReader(string(data))); err != nil {
			return res, err
		}
	}
	// The README is generated from the reference, so it is always rewritten to
	// match; it carries no user content of its own.
	if err := safety.CopyAtomic(RefReadmePath(projectDir), strings.NewReader(Explain(ref))); err != nil {
		return res, err
	}
	return res, nil
}

// ErrRefDiffers reports an existing reference file with different contents.
var ErrRefDiffers = errors.New("a different .cct/sessions.json already exists")

// isControl reports the C0/C1 control characters that must never appear in a
// value cct prints or pastes into a command.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// normText normalizes line endings for comparison, mirroring the skill install.
func normText(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }

// StoreDir resolves where the session store is cloned on this machine:
// the configured directory, or the conventional default, with a leading ~
// expanded.
func StoreDir(configured string) string {
	dir := strings.TrimSpace(configured)
	if dir == "" {
		dir = DefaultStoreDir
	}
	return ExpandHome(dir)
}

// ExpandHome replaces a leading ~ with the user's home directory. An
// unresolvable home leaves the path as written, so the caller still shows the
// user something meaningful.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// BundleName is the file name of a tool's full bundle inside the store.
func BundleName(tool, encryption string) string {
	name := tool + "-all.codexbundle"
	if encryption == EncryptionAge {
		name += ".age"
	}
	return name
}

// ProjectDirIn returns a project's directory inside a cloned store.
func ProjectDirIn(storeDir string, ref Reference) string {
	return filepath.Join(storeDir, ProjectsSubdir, ref.Project)
}

// BundlePathIn returns a tool's full-bundle path inside a cloned store.
func BundlePathIn(storeDir string, ref Reference, tool string) string {
	return filepath.Join(ProjectDirIn(storeDir, ref), tool, BundleName(tool, ref.Encryption))
}

// GroupPathIn returns a named group bundle's path inside a cloned store.
func GroupPathIn(storeDir string, ref Reference, tool, group string) string {
	name := Slug(group) + ".codexbundle"
	if ref.Encryption == EncryptionAge {
		name += ".age"
	}
	return filepath.Join(ProjectDirIn(storeDir, ref), tool, "groups", name)
}

// Explain renders the human- and agent-readable description of a reference:
// what the store looks like, and the exact commands for this project. It is
// written to .cct/README.md and printed by `cct skill show`.
func Explain(ref Reference) string {
	var b strings.Builder
	enc := ""
	restoreExtra := ""
	if ref.Encryption == EncryptionAge {
		enc = " \\\n  --encrypt-to " + ref.Recipient
		restoreExtra = " --identity <your-age-key-file>"
	}
	full := BundleName("claude", ref.Encryption)

	fmt.Fprintf(&b, `# Session history for %s

This repository holds no chat history. It lives in a separate **private** git
repo, so the project's code and the agent transcripts stay apart:

    %s

The file next to this one, `+"`sessions.json`"+`, is the machine-readable version of
the same fact — that is what a coding agent reads.

## How it is organized

    <session store>/
      projects/
        %s/
          claude/
            %s
            groups/
              <name>.codexbundle
          codex/
            %s
            groups/

`, ref.Project, ref.Repo, ref.Project, full, BundleName("codex", ref.Encryption))

	fmt.Fprintf(&b, `The full bundle is the source of truth: it always contains every session for
this project. A group is an extra, smaller bundle for one topic — handy for
sharing or for restoring just one thread. Groups overlap with the full bundle on
purpose.

## Save (before you stop working)

    cd <session store> && git pull
    cct export --project <this repo> --tool claude \
      -o <session store>/projects/%s/claude/%s%s
    cd <session store> && git add -A && git commit -m "Update %s sessions"
    # push only after checking what is staged

## Restore (on the other machine, after cloning this repo)

    git clone %s <session store>
    cd <this repo>          # run the import from the project, see below
    cct import <session store>/projects/%s/claude/%s \
      --merge --map-cwd-here%s

`+"`--map-cwd-here`"+` re-points the sessions at *the current directory*, which is why
the import runs from the project and not from the store. `+"`--merge`"+` keeps a
repeated restore incremental. Restart the agent afterwards so it rescans, then
`+"`cct resume <thread-id>`"+`.

## Groups

    cct export --project . --tool claude --match "auth refactor" \
      -o <session store>/projects/%s/claude/groups/auth-refactor.codexbundle
    cct export --project . --tool claude --session 9f3c \
      -o <session store>/projects/%s/claude/groups/that-one-chat.codexbundle

`, ref.Project, full, enc, ref.Project, ref.Repo, ref.Project, full, restoreExtra, ref.Project, ref.Project)

	if ref.Encryption == EncryptionAge {
		fmt.Fprintf(&b, `## Encryption

Bundles are encrypted to the age recipient `+"`%s`"+`, which is a
public key — it can encrypt, never decrypt. Keep the matching private key out of
both repositories; every machine that restores needs it.

`, ref.Recipient)
	} else {
		b.WriteString(`## No encryption

Bundles are committed as-is, so the session store repo MUST be private: it
contains prompts, code, and command output, and git history keeps them after a
deletion. Switch with ` + "`cct config set repo-sync encrypted`" + ` and re-export.

`)
	}

	b.WriteString(`## A word of caution

This file and ` + "`sessions.json`" + ` come from the repository, so whoever can commit
here can change where they point. Before cloning or importing from a store URL
you did not set up yourself, check it with the person who did.

Written by cct — https://github.com/ahmojo/codex-claude-transfer
`)
	return b.String()
}
