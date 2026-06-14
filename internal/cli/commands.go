// Package cli implements the codex-sync command-line interface. The CLI is a
// thin layer over the reusable core packages (codexhome, sessions, doctor) so
// the same core can later back a desktop app.
package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ahmojo/Codex_Sync/internal/bundle"
	"github.com/ahmojo/Codex_Sync/internal/codexhome"
	"github.com/ahmojo/Codex_Sync/internal/doctor"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
)

// Run parses args (excluding the program name) and executes the requested
// command, writing to stdout/stderr. It returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "doctor":
		return runDoctor(rest, stdout, stderr)
	case "list":
		return runList(rest, stdout, stderr)
	case "export":
		return runExport(rest, stdout, stderr)
	case "inspect":
		return runInspect(rest, stdout, stderr)
	case "import":
		return runImport(rest, stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %q\n\n", command)
		printUsage(stderr)
		return 2
	}
}

// commonFlags holds flags shared by commands.
type commonFlags struct {
	codexHome       string
	includeArchived bool
	project         string
	output          string
	dryRun          bool
	all             bool
	since           string
	mapCWD          []string
	positional      []string
}

// parseFlags is a tiny, dependency-free flag parser for the flags we support in
// v0.1. Unknown flags are reported as errors.
func parseFlags(args []string) (commonFlags, error) {
	var f commonFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--codex-home":
			val, err := takeValue(args, &i, "--codex-home")
			if err != nil {
				return f, err
			}
			f.codexHome = val
		case hasPrefix(arg, "--codex-home="):
			f.codexHome = arg[len("--codex-home="):]
		case arg == "--project":
			val, err := takeValue(args, &i, "--project")
			if err != nil {
				return f, err
			}
			f.project = val
		case hasPrefix(arg, "--project="):
			f.project = arg[len("--project="):]
		case arg == "--output" || arg == "-o":
			val, err := takeValue(args, &i, arg)
			if err != nil {
				return f, err
			}
			f.output = val
		case hasPrefix(arg, "--output="):
			f.output = arg[len("--output="):]
		case arg == "--map-cwd":
			val, err := takeValue(args, &i, "--map-cwd")
			if err != nil {
				return f, err
			}
			f.mapCWD = append(f.mapCWD, val)
		case hasPrefix(arg, "--map-cwd="):
			f.mapCWD = append(f.mapCWD, arg[len("--map-cwd="):])
		case arg == "--all":
			f.all = true
		case arg == "--since":
			val, err := takeValue(args, &i, "--since")
			if err != nil {
				return f, err
			}
			f.since = val
		case hasPrefix(arg, "--since="):
			f.since = arg[len("--since="):]
		case arg == "--include-archived":
			f.includeArchived = true
		case arg == "--dry-run":
			f.dryRun = true
		case hasPrefix(arg, "-"):
			return f, fmt.Errorf("unknown flag: %q", arg)
		default:
			f.positional = append(f.positional, arg)
		}
	}
	return f, nil
}

// takeValue consumes the next arg as the value for flagName, advancing i.
func takeValue(args []string, i *int, flagName string) (string, error) {
	if *i+1 >= len(args) {
		return "", fmt.Errorf("%s requires a value", flagName)
	}
	*i++
	return args[*i], nil
}

func resolveHome(f commonFlags, stderr io.Writer) (codexhome.Home, bool) {
	home, err := codexhome.Detect(f.codexHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Codex home: %v\n", err)
		return codexhome.Home{}, false
	}
	return home, true
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return 1
	}
	report := doctor.Run(home)
	printReport(stdout, report)
	return 0
}

func runList(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return 1
	}
	scan, err := sessions.Scan(home, sessions.ScanOptions{IncludeArchived: f.includeArchived})
	if err != nil {
		fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
		return 1
	}
	printList(stdout, scan)
	return 0
}

func runExport(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return 1
	}

	if f.all && f.project != "" {
		fmt.Fprintln(stderr, "error: --all and --project are mutually exclusive")
		return 2
	}

	var since time.Time
	if f.since != "" {
		since, err = parseSince(f.since)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	}

	// Without --all, export the current project by default (cwd). With --all,
	// the project path is empty so every session is considered.
	var absProject string
	if !f.all {
		project := f.project
		if project == "" {
			project = "."
		}
		absProject, err = filepath.Abs(project)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot resolve project path %q: %v\n", project, err)
			return 1
		}
	}

	output := f.output
	if output == "" {
		if f.all {
			output = "codex-sessions.codexbundle"
		} else {
			output = defaultBundleName(absProject)
		}
	}

	result, err := bundle.Export(home, bundle.ExportOptions{
		ProjectPath:     absProject,
		OutputPath:      output,
		IncludeArchived: f.includeArchived,
		Since:           since,
	})
	if err != nil {
		for _, w := range result.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", w)
		}
		fmt.Fprintf(stderr, "error: export failed: %v\n", err)
		return 1
	}
	printExport(stdout, absProject, result)
	return 0
}

// parseSince accepts either an absolute date (YYYY-MM-DD, interpreted as UTC
// midnight) or a relative duration ending in d/h/m (e.g. 7d, 48h, 90m) measured
// back from now. It returns the cutoff instant; sessions updated at or after it
// are exported.
func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if d, err := parseDayDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use a date (YYYY-MM-DD) or a duration like 7d, 48h, 90m", s)
}

// parseDayDuration extends time.ParseDuration with a "d" (days) unit, which the
// standard library does not support.
func parseDayDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}

// defaultBundleName derives <project-base>.codexbundle in the current directory.
func defaultBundleName(absProject string) string {
	base := filepath.Base(absProject)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "codex-sessions"
	}
	return base + ".codexbundle"
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) != 1 {
		fmt.Fprintln(stderr, "usage: codex-sync inspect <file.codexbundle>")
		return 2
	}
	res, err := bundle.Inspect(f.positional[0])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	printInspect(stdout, f.positional[0], res)
	return 0
}

func runImport(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) != 1 {
		fmt.Fprintln(stderr, "usage: codex-sync import <file.codexbundle> [--dry-run] [--project <path>] [--map-cwd OLD=NEW]")
		return 2
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return 1
	}

	var absProject string
	if f.project != "" {
		absProject, err = filepath.Abs(f.project)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot resolve project path %q: %v\n", f.project, err)
			return 1
		}
	}

	mappings, err := bundle.ParseCWDMappings(f.mapCWD)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:  f.positional[0],
		DryRun:      f.dryRun,
		ProjectPath: absProject,
		MapCWD:      mappings,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: import failed: %v\n", err)
		return 1
	}
	printImport(stdout, f.positional[0], res)
	return 0
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `codex-sync — local Codex session portability (unofficial)

  Export. Move. Import. Continue your local Codex sessions anywhere.

Usage:
  codex-sync <command> [flags]

Commands:
  doctor    Read-only health check: find your Codex home, count sessions,
            and confirm SQLite will not be modified
  list      List discovered Codex sessions (preview, thread id, cwd, time)
  export    Export sessions for a project into a .codexbundle
  inspect   Show a bundle's manifest and contents, read-only (no extraction)
  import    Import a .codexbundle into your Codex home (never overwrites)
  help      Show this help

Flags:
  --codex-home <path>   Use a specific Codex home instead of ~/.codex
                        (also honors $CODEX_HOME)
  --include-archived    list, export: also consider archived sessions
  --project <path>      export: filter sessions by recorded cwd
                        import: warn on cwd mismatch (never rewrites paths)
  --all                 export: include every session (no cwd filter);
                        mutually exclusive with --project
  --since <when>        export: only sessions updated at/after <when>, where
                        <when> is a date (YYYY-MM-DD) or a duration (7d, 48h, 90m)
  --output, -o <path>   export: bundle output path (default <project>.codexbundle)
  --dry-run             import: validate and report only, write nothing
  --map-cwd OLD=NEW     import: rewrite a session's recorded cwd from OLD to NEW
                        so it lands in the right local project (repeatable;
                        plain .jsonl only — .zst sessions are not rewritten)

Examples:
  codex-sync doctor
  codex-sync list
  codex-sync export --project .            # -> <project>.codexbundle
  codex-sync export --all                  # -> codex-sessions.codexbundle
  codex-sync export --all --since 7d       # everything updated in the last 7 days
  codex-sync export --project . --since 2026-06-01
  codex-sync inspect ./my-project.codexbundle
  codex-sync import ./my-project.codexbundle --dry-run
  codex-sync import ./my-project.codexbundle
  codex-sync import ./my-project.codexbundle --map-cwd "/old/path=/new/path"

After importing, restart the Codex App (or run Codex again) so it scans and
reconciles the imported rollout files.

Notes:
  codex-sync never modifies Codex's SQLite state DB; Codex rebuilds its own
  index from the JSONL files on its next scan.
  .codexbundle files may contain prompts, code, terminal output, paths, and
  secrets — do not share them publicly. See docs/safety.md.
`)
}
