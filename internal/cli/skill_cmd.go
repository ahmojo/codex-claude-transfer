package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ahmojo/codex-claude-transfer/internal/config"
	"github.com/ahmojo/codex-claude-transfer/internal/skill"
)

const skillUsage = `usage: cct skill <install | print | path | init | show>
  install   write the ` + skill.Name + ` skill into your Claude Code home
  print     print the same instructions (--plain drops the skill frontmatter,
            for pasting into Codex's AGENTS.md or another agent's instructions)
  path      print where install would write the file
  init      write .cct/sessions.json + .cct/README.md into this project, so an
            agent (and you) can find the private session store that holds its
            history. Commit them.
  show      read this project's .cct/sessions.json and explain the layout and
            the exact save/restore commands (--json for the raw reference)
options: [--claude-home <path>] [--project <dir>] [--repo <git-url>]
         [--force] [--dry-run] [--plain] [--json]

The skill teaches a coding agent one workflow: keep this project's sessions in
a git repo — either the project's own under .cct/, or a separate private session
store the project only points at — and restore them after a clone on another
machine. It is instructions only: installing it changes no session file.`

// runSkill installs or prints the bundled agent workflow skill. Everything it
// writes lives inside the agent's skills directory; session files and the
// agent's index are never touched.
func runSkill(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, skillUsage)
		return 2
	}
	sub := args[0]
	f, err := parseFlags(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	switch sub {
	case "print":
		if f.plain {
			fmt.Fprint(stdout, skill.Body())
		} else {
			fmt.Fprint(stdout, skill.Document())
		}
		return 0
	case "path":
		home, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		fmt.Fprintln(stdout, skill.Path(home.Root))
		return 0
	case "install":
		home, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		res, err := skill.Install(home.Root, skill.Options{Force: f.force, DryRun: f.dryRun})
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			if errors.Is(err, skill.ErrDiffers) {
				fmt.Fprintln(stderr, "It may be your own edit, or an older cct's copy. Re-run with --force to")
				fmt.Fprintln(stderr, "replace it (the current file is kept as a .cct-bak-* copy next to it),")
				fmt.Fprintln(stderr, "or compare it against `cct skill print` first.")
			}
			return 1
		}
		printSkillInstall(stdout, res)
		return 0
	case "init":
		return runSkillInit(f, stdout, stderr)
	case "show":
		return runSkillShow(f, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown skill subcommand %q\n\n%s\n", sub, skillUsage)
		return 2
	}
}

// projectDirFlag resolves the project directory a skill subcommand acts on:
// --project when given, otherwise the current directory.
func projectDirFlag(f commonFlags) (string, error) {
	dir := f.project
	if dir == "" {
		dir = "."
	}
	return filepath.Abs(dir)
}

// runSkillInit writes the reference files that point this project at a separate
// private session store. It writes only inside the project's .cct/ directory.
func runSkillInit(f commonFlags, stdout, stderr io.Writer) int {
	dir, err := projectDirFlag(f)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot resolve the project directory: %v\n", err)
		return 1
	}
	defaults := loadDefaults()
	repo := f.repo
	if repo == "" {
		repo = defaults.RepoSyncRepo
	}
	if repo == "" {
		fmt.Fprintln(stderr, "error: no session store repo. Create a PRIVATE git repo for your session")
		fmt.Fprintln(stderr, "history, then either pass --repo <git-url> or save it once with:")
		fmt.Fprintln(stderr, "  cct config set repo-sync-repo <git-url>")
		return 2
	}
	encryption := skill.EncryptionPlain
	if defaults.RepoSync == config.RepoSyncEncrypted {
		encryption = skill.EncryptionAge
	}
	ref, err := skill.NewReference(dir, repo, "", encryption, defaults.RepoSyncRecipient)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	res, err := skill.WriteReference(dir, ref, skill.Options{Force: f.force, DryRun: f.dryRun})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		if errors.Is(err, skill.ErrRefDiffers) {
			fmt.Fprintln(stderr, "This project already points at a session store. Check it with `cct skill show`;")
			fmt.Fprintln(stderr, "re-run with --force only if you really mean to repoint it.")
		}
		return 1
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "Would write %s and %s (%s).\n", res.Path, skill.RefReadmePath(dir), res.Status)
		return 0
	}
	fmt.Fprintf(stdout, "Wrote %s\n      %s\n", res.Path, skill.RefReadmePath(dir))
	fmt.Fprintf(stdout, "Project folder in the store: %s\n", safeTerminal(ref.Path))
	fmt.Fprintln(stdout, "Commit both files. They contain no local paths and no private keys.")
	if defaults.RepoSyncRepo == "" {
		fmt.Fprintln(stdout, "Tip: `cct config set repo-sync-repo <git-url>` saves the store for the next project.")
	}
	return 0
}

// runSkillShow explains a project's session store: where it is, how it is laid
// out, and the exact commands for this machine.
func runSkillShow(f commonFlags, stdout, stderr io.Writer) int {
	dir, err := projectDirFlag(f)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot resolve the project directory: %v\n", err)
		return 1
	}
	ref, err := skill.ReadReference(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: no %s in %s.\n", filepath.Join(skill.RefSubdir, skill.RefFileName), dir)
			fmt.Fprintln(stderr, "This project keeps no reference to a session store; run `cct skill init --repo <git-url>`")
			fmt.Fprintln(stderr, "to add one, or keep the bundle in this repo under .cct/ instead.")
			return 1
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	storeDir := skill.StoreDir(loadDefaults().RepoSyncDir)

	if f.jsonOut {
		printSkillShowJSON(stdout, ref, storeDir)
		return 0
	}

	// Every value below came out of a file in the repository, so it is scrubbed
	// before it reaches the terminal.
	fmt.Fprintf(stdout, "Session store (from %s):\n", filepath.Join(skill.RefSubdir, skill.RefFileName))
	fmt.Fprintf(stdout, "  repo        %s\n", safeTerminal(ref.Repo))
	fmt.Fprintf(stdout, "  project     %s\n", safeTerminal(ref.Path))
	fmt.Fprintf(stdout, "  encryption  %s\n", safeTerminal(ref.Encryption))
	if ref.Recipient != "" {
		fmt.Fprintf(stdout, "  recipient   %s\n", safeTerminal(ref.Recipient))
	}
	// storeDir comes from local config rather than the repo, but a config file
	// can be hand-edited or generated, and every other printer here scrubs, so
	// it is scrubbed too. os.Stat still uses the unmodified path.
	fmt.Fprintf(stdout, "  clone       %s", safeTerminal(storeDir))
	if _, serr := os.Stat(storeDir); serr == nil {
		fmt.Fprintln(stdout, "  (present)")
	} else {
		fmt.Fprintln(stdout, "  (not cloned yet)")
	}
	fmt.Fprintln(stdout)
	for _, tool := range []string{"claude", "codex"} {
		fmt.Fprintf(stdout, "  %-6s bundle  %s\n", tool, safeTerminal(skill.BundlePathIn(storeDir, ref, tool)))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "That repo URL comes from this repository, not from you. If you did not set it")
	fmt.Fprintln(stdout, "up, confirm it with whoever did before cloning or importing from it.")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Full explanation: %s\n", skill.RefReadmePath(dir))
	return 0
}

// skillShowJSON is the machine-readable form of `cct skill show`: the project's
// reference plus the paths it resolves to on this machine.
type skillShowJSON struct {
	Repo        string            `json:"repo"`
	Project     string            `json:"project"`
	Path        string            `json:"path"`
	Encryption  string            `json:"encryption"`
	Recipient   string            `json:"recipient,omitempty"`
	StoreDir    string            `json:"store_dir"`
	StoreCloned bool              `json:"store_cloned"`
	Bundles     map[string]string `json:"bundles"`
}

func printSkillShowJSON(w io.Writer, ref skill.Reference, storeDir string) {
	_, err := os.Stat(storeDir)
	out := skillShowJSON{
		Repo:        ref.Repo,
		Project:     ref.Project,
		Path:        ref.Path,
		Encryption:  ref.Encryption,
		Recipient:   ref.Recipient,
		StoreDir:    storeDir,
		StoreCloned: err == nil,
		Bundles: map[string]string{
			"claude": skill.BundlePathIn(storeDir, ref, "claude"),
			"codex":  skill.BundlePathIn(storeDir, ref, "codex"),
		},
	}
	writeJSON(w, out)
}

// printSkillInstall reports what the install did and what the user has to do to
// make the agent notice it.
func printSkillInstall(stdout io.Writer, res skill.Result) {
	switch {
	case res.DryRun && res.Status == skill.StatusUnchanged:
		fmt.Fprintf(stdout, "Already up to date: %s\n", res.Path)
		return
	case res.DryRun:
		fmt.Fprintf(stdout, "Would write %s (%s).\n", res.Path, res.Status)
		return
	case res.Status == skill.StatusUnchanged:
		fmt.Fprintf(stdout, "Already up to date: %s\n", res.Path)
	case res.Status == skill.StatusUpdated:
		fmt.Fprintf(stdout, "Updated %s\n", res.Path)
		if res.Backup != "" {
			fmt.Fprintf(stdout, "The previous file is kept at %s\n", res.Backup)
		}
	default:
		fmt.Fprintf(stdout, "Installed %s\n", res.Path)
	}
	fmt.Fprintf(stdout, "Restart Claude Code, then ask it to sync this project's sessions (/%s).\n", skill.Name)
	fmt.Fprintln(stdout, "For Codex, append `cct skill print --plain` to your AGENTS.md instead.")
}
