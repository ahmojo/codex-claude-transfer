package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/ahmojo/codex-claude-transfer/internal/skill"
)

const skillUsage = `usage: cct skill <install | print | path>
  install   write the ` + skill.Name + ` skill into your Claude Code home
  print     print the same instructions (--plain drops the skill frontmatter,
            for pasting into Codex's AGENTS.md or another agent's instructions)
  path      print where install would write the file
options: [--claude-home <path>] [--force] [--dry-run] [--plain]

The skill teaches a coding agent one workflow: export this project's sessions
into .cct/ and commit them, then import them again after a clone on another
machine. It is instructions only — installing it changes no session file.`

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
	default:
		fmt.Fprintf(stderr, "unknown skill subcommand %q\n\n%s\n", sub, skillUsage)
		return 2
	}
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
