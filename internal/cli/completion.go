package cli

import (
	"fmt"
	"io"
	"strings"
)

// completionCommand is one top-level command, used to generate shell completions.
type completionCommand struct {
	name string
	desc string
}

// completionCommands and completionFlags are the single source of truth the three
// shell completion scripts are generated from, so they stay in sync with the CLI.
var completionCommands = []completionCommand{
	{"doctor", "Read-only health check"},
	{"list", "List discovered sessions (Codex or Claude Code)"},
	{"search", "Full-text search across session text"},
	{"scan", "Check sessions for likely secrets"},
	{"stats", "Summarize sessions (totals, projects, activity)"},
	{"resume", "Print/launch the command to continue a session"},
	{"browse", "Interactive session browser"},
	{"tag", "Add/remove/list cct-only tags on a session"},
	{"name", "Give a session a friendly cct-only name"},
	{"config", "Save user defaults (tool, homes, port)"},
	{"export", "Export sessions into a .codexbundle"},
	{"inspect", "Show a bundle's contents (read-only)"},
	{"diff", "Preview what importing a bundle would do (read-only)"},
	{"import", "Import a bundle into the agent's home"},
	{"relocate", "Remap sessions after a project folder moves"},
	{"undo", "Reverse the most recent import or relocation"},
	{"repair-times", "Fix imported sessions' modification times"},
	{"sync", "Experimental device-to-device LAN sync"},
	{"ui", "Interactive guided menu"},
	{"app", "Launch the local desktop GUI in your browser"},
	{"version", "Print the cct version"},
	{"completion", "Print a shell completion script"},
	{"help", "Show help"},
}

var completionFlags = []string{
	"--tool", "--codex-home", "--claude-home", "--project", "--all", "--session",
	"--since", "--with-git", "--git-push", "--output", "-o", "--include-archived",
	"--json", "--dry-run", "--to", "--map-cwd", "--map-cwd-here", "--merge", "--reconcile",
	"--move-project",
	"--replace-with-backup", "--import-as-copy", "--clone", "--encrypt-to",
	"--recipients-file", "--passphrase", "--identity", "--port", "--no-browser",
	"--match", "--format", "--redact", "--allow-secrets", "--regex",
	"--case-sensitive", "--run", "--remember", "--i-understand", "--allow-public",
	"--pull-only", "--push-only", "--code", "--once", "--interval",
	"--list", "--version", "--help",
}

// runCompletion prints a completion script for the requested shell.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: cct completion <bash|zsh|fish>")
		return 2
	}
	switch args[0] {
	case "bash":
		fmt.Fprint(stdout, bashCompletion())
	case "zsh":
		fmt.Fprint(stdout, zshCompletion())
	case "fish":
		fmt.Fprint(stdout, fishCompletion())
	default:
		fmt.Fprintf(stderr, "unsupported shell %q: use bash, zsh, or fish\n", args[0])
		return 2
	}
	return 0
}

func commandNames() string {
	names := make([]string, len(completionCommands))
	for i, c := range completionCommands {
		names[i] = c.name
	}
	return strings.Join(names, " ")
}

func bashCompletion() string {
	return fmt.Sprintf(`# bash completion for cct
# Enable with:  source <(cct completion bash)
_cct() {
    local cur commands flags
    cur="${COMP_WORDS[COMP_CWORD]}"
    commands="%s"
    flags="%s"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
        return 0
    fi
    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
        return 0
    fi
    COMPREPLY=( $(compgen -f -- "$cur") )
    return 0
}
complete -o default -F _cct cct
`, commandNames(), strings.Join(completionFlags, " "))
}

func zshCompletion() string {
	var cmds strings.Builder
	for _, c := range completionCommands {
		fmt.Fprintf(&cmds, "    '%s:%s'\n", c.name, c.desc)
	}
	var flags strings.Builder
	for _, f := range completionFlags {
		fmt.Fprintf(&flags, "    '%s' \\\n", f)
	}
	return fmt.Sprintf(`#compdef cct
# zsh completion for cct
# Enable with:  source <(cct completion zsh)
_cct() {
  local -a cmds
  cmds=(
%s  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'cct command' cmds
    return
  fi
  _arguments -s \
%s    '*:file:_files'
}
compdef _cct cct
`, cmds.String(), flags.String())
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString("# fish completion for cct\n")
	b.WriteString("# Enable with:  cct completion fish | source\n")
	for _, c := range completionCommands {
		fmt.Fprintf(&b, "complete -c cct -n __fish_use_subcommand -a %s -d %q\n", c.name, c.desc)
	}
	for _, f := range completionFlags {
		switch {
		case f == "-o":
			// handled together with --output below
		case f == "--output":
			b.WriteString("complete -c cct -s o -l output -d 'Bundle output path'\n")
		case strings.HasPrefix(f, "--"):
			fmt.Fprintf(&b, "complete -c cct -l %s\n", strings.TrimPrefix(f, "--"))
		}
	}
	return b.String()
}
