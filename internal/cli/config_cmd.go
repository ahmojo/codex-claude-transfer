package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/ahmojo/codex-claude-transfer/internal/cctpaths"
	"github.com/ahmojo/codex-claude-transfer/internal/config"
)

// cctConfigDir resolves cct's config directory (honoring $CCT_CONFIG_DIR).
func cctConfigDir() string {
	d, _ := cctpaths.ConfigDir("")
	return d
}

// loadDefaults returns the user's saved config defaults (empty on any error, so
// a missing/garbage file never blocks a command — flags always work).
func loadDefaults() config.Config {
	c, _ := config.Load(cctConfigDir())
	return c
}

// defaultCodexHome applies precedence flag > $CODEX_HOME > config > ~/.codex for
// the Codex home override. The env check keeps tests that set $CODEX_HOME working
// regardless of any saved config.
func defaultCodexHome(flagVal string) string {
	if flagVal != "" || os.Getenv("CODEX_HOME") != "" {
		return flagVal
	}
	return loadDefaults().CodexHome
}

// defaultClaudeHome mirrors defaultCodexHome for the Claude Code home.
func defaultClaudeHome(flagVal string) string {
	if flagVal != "" || os.Getenv("CLAUDE_HOME") != "" {
		return flagVal
	}
	return loadDefaults().ClaudeHome
}

const configUsage = `usage: cct config <list | get <key> | set <key> <value> | path>
  list              show the values you've set
  get <key>         print one value
  set <key> <value> save a default (empty value clears it)
  path              print the config file location

keys: tool (codex|claude), codex-home, claude-home, port,
      repo-sync (plain|encrypted), repo-sync-recipient (age1...)
These are just defaults; an explicit flag always wins.`

// runConfig manages cct's small set of saved user defaults.
func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	dir := cctConfigDir()
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	switch args[0] {
	case "list":
		entries := cfg.Entries()
		if len(entries) == 0 {
			fmt.Fprintf(stdout, "No config set. File would live at:\n  %s\n", config.FilePath(dir))
			return 0
		}
		for _, e := range entries {
			fmt.Fprintf(stdout, "%-20s %s\n", e[0], safeTerminal(e[1]))
		}
		return 0
	case "path":
		fmt.Fprintln(stdout, config.FilePath(dir))
		return 0
	case "get":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: cct config get <key>")
			return 2
		}
		v, err := cfg.Get(args[1])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, v)
		return 0
	case "set":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "usage: cct config set <key> <value>")
			return 2
		}
		value := ""
		if len(args) >= 3 {
			value = args[2]
		}
		if err := cfg.Set(args[1], value); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		if err := cfg.Save(dir); err != nil {
			fmt.Fprintf(stderr, "error: cannot save config: %v\n", err)
			return 1
		}
		if value == "" {
			fmt.Fprintf(stdout, "Cleared %s.\n", args[1])
		} else {
			fmt.Fprintf(stdout, "Set %s = %s\n", args[1], safeTerminal(value))
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown config subcommand %q\n\n%s\n", args[0], configUsage)
		return 2
	}
}
