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
	{"list", "List discovered Codex sessions"},
	{"export", "Export sessions into a .codexbundle"},
	{"inspect", "Show a bundle's contents (read-only)"},
	{"import", "Import a bundle into your Codex home"},
	{"ui", "Interactive guided menu"},
	{"version", "Print the codex-sync version"},
	{"completion", "Print a shell completion script"},
	{"help", "Show help"},
}

var completionFlags = []string{
	"--codex-home", "--project", "--all", "--session", "--since", "--with-git",
	"--output", "-o", "--include-archived", "--json", "--dry-run", "--map-cwd",
	"--replace-with-backup", "--import-as-copy", "--clone", "--encrypt-to",
	"--recipients-file", "--passphrase", "--identity", "--version", "--help",
}

// runCompletion prints a completion script for the requested shell.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: codex-sync completion <bash|zsh|fish>")
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
	return fmt.Sprintf(`# bash completion for codex-sync
# Enable with:  source <(codex-sync completion bash)
_codex_sync() {
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
complete -o default -F _codex_sync codex-sync
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
	return fmt.Sprintf(`#compdef codex-sync
# zsh completion for codex-sync
# Enable with:  source <(codex-sync completion zsh)
_codex_sync() {
  local -a cmds
  cmds=(
%s  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'codex-sync command' cmds
    return
  fi
  _arguments -s \
%s    '*:file:_files'
}
compdef _codex_sync codex-sync
`, cmds.String(), flags.String())
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString("# fish completion for codex-sync\n")
	b.WriteString("# Enable with:  codex-sync completion fish | source\n")
	for _, c := range completionCommands {
		fmt.Fprintf(&b, "complete -c codex-sync -n __fish_use_subcommand -a %s -d %q\n", c.name, c.desc)
	}
	for _, f := range completionFlags {
		switch {
		case f == "-o":
			// handled together with --output below
		case f == "--output":
			b.WriteString("complete -c codex-sync -s o -l output -d 'Bundle output path'\n")
		case strings.HasPrefix(f, "--"):
			fmt.Fprintf(&b, "complete -c codex-sync -l %s\n", strings.TrimPrefix(f, "--"))
		}
	}
	return b.String()
}
