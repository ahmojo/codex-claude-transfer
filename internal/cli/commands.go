// Package cli implements the codex-sync command-line interface. The CLI is a
// thin layer over the reusable core packages (codexhome, sessions, doctor) so
// the same core can later back a desktop app.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ahmojo/Codex_Sync/internal/bundle"
	"github.com/ahmojo/Codex_Sync/internal/codexhome"
	"github.com/ahmojo/Codex_Sync/internal/crypt"
	"github.com/ahmojo/Codex_Sync/internal/doctor"
	"github.com/ahmojo/Codex_Sync/internal/git"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
	"github.com/ahmojo/Codex_Sync/internal/webui"
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
	case "ui":
		return runUI(rest, stdout, stderr)
	case "app":
		return runApp(rest, stdout, stderr)
	case "version", "--version", "-V":
		printVersion(stdout)
		return 0
	case "completion":
		return runCompletion(rest, stdout, stderr)
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
	sessions        []string
	withGit         bool
	gitPush         bool
	cloneDir        string
	mapCWD          []string
	encryptTo       []string
	recipientsFile  string
	passphrase      bool
	identity        string
	replaceBackup   bool
	importAsCopy    bool
	jsonOut         bool
	port            int
	noBrowser       bool
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
		case arg == "--session":
			val, err := takeValue(args, &i, "--session")
			if err != nil {
				return f, err
			}
			f.sessions = append(f.sessions, val)
		case hasPrefix(arg, "--session="):
			f.sessions = append(f.sessions, arg[len("--session="):])
		case arg == "--with-git":
			f.withGit = true
		case arg == "--git-push":
			f.gitPush = true
		case arg == "--clone":
			val, err := takeValue(args, &i, "--clone")
			if err != nil {
				return f, err
			}
			f.cloneDir = val
		case hasPrefix(arg, "--clone="):
			f.cloneDir = arg[len("--clone="):]
		case arg == "--encrypt-to":
			val, err := takeValue(args, &i, "--encrypt-to")
			if err != nil {
				return f, err
			}
			f.encryptTo = append(f.encryptTo, val)
		case hasPrefix(arg, "--encrypt-to="):
			f.encryptTo = append(f.encryptTo, arg[len("--encrypt-to="):])
		case arg == "--recipients-file":
			val, err := takeValue(args, &i, "--recipients-file")
			if err != nil {
				return f, err
			}
			f.recipientsFile = val
		case hasPrefix(arg, "--recipients-file="):
			f.recipientsFile = arg[len("--recipients-file="):]
		case arg == "--passphrase":
			f.passphrase = true
		case arg == "--identity":
			val, err := takeValue(args, &i, "--identity")
			if err != nil {
				return f, err
			}
			f.identity = val
		case hasPrefix(arg, "--identity="):
			f.identity = arg[len("--identity="):]
		case arg == "--replace-with-backup":
			f.replaceBackup = true
		case arg == "--import-as-copy":
			f.importAsCopy = true
		case arg == "--include-archived":
			f.includeArchived = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--no-browser":
			f.noBrowser = true
		case arg == "--port":
			val, err := takeValue(args, &i, "--port")
			if err != nil {
				return f, err
			}
			n, perr := strconv.Atoi(val)
			if perr != nil || n < 0 || n > 65535 {
				return f, fmt.Errorf("invalid --port %q", val)
			}
			f.port = n
		case hasPrefix(arg, "--port="):
			val := arg[len("--port="):]
			n, perr := strconv.Atoi(val)
			if perr != nil || n < 0 || n > 65535 {
				return f, fmt.Errorf("invalid --port %q", val)
			}
			f.port = n
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

// runApp launches the local desktop GUI: a loopback-only web server the user
// drives from their browser. It blocks until interrupted.
func runApp(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	return webui.Run(webui.Options{
		CodexHome: f.codexHome,
		Port:      f.port,
		NoBrowser: f.noBrowser,
	}, stdout, stderr)
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
	if f.jsonOut {
		printDoctorJSON(stdout, report)
	} else {
		printReport(stdout, report)
	}
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
	scan, err := sessions.Scan(home, sessions.ScanOptions{
		IncludeArchived:      f.includeArchived,
		DecompressCompressed: true,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
		return 1
	}
	if f.jsonOut {
		printListJSON(stdout, scan)
	} else {
		printList(stdout, scan)
	}
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
	// Export targets exactly one session; import can take several. Reject more
	// than one --session here so the single-session output name stays meaningful.
	if len(f.sessions) > 1 {
		fmt.Fprintln(stderr, "error: export accepts only one --session (use --all to export several)")
		return 2
	}
	session := ""
	if len(f.sessions) == 1 {
		session = f.sessions[0]
	}
	if session != "" && (f.all || f.project != "") {
		fmt.Fprintln(stderr, "error: --session cannot be combined with --all or --project")
		return 2
	}

	encryptRequested := len(f.encryptTo) > 0 || f.recipientsFile != "" || f.passphrase
	if f.passphrase && (len(f.encryptTo) > 0 || f.recipientsFile != "") {
		fmt.Fprintln(stderr, "error: --passphrase cannot be combined with --encrypt-to or --recipients-file")
		return 2
	}
	if encryptRequested && !crypt.Available() {
		fmt.Fprintln(stderr, "error: "+ageMissingMessage)
		return 1
	}

	var since time.Time
	if f.since != "" {
		since, err = parseSince(f.since)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	}

	// Without --all or --session, export the current project by default (cwd).
	// With --all the project path is empty so every session is considered;
	// with --session the single matching session is selected regardless of cwd.
	var absProject string
	if !f.all && session == "" {
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

	// Opt-in --git-push completes the handoff: it pushes the project's code to its
	// own git remote so the commit recorded in the bundle is actually fetchable on
	// the other machine. It pushes CODE to YOUR remote only — never sessions, never
	// to any codex-sync server — and runs before the export so the bundle records
	// the now-pushed state. It is scoped to a single project (not --all/--session).
	if f.gitPush {
		if code := pushProject(absProject, session, f.all, stdout, stderr); code != 0 {
			return code
		}
	}

	output := f.output
	if output == "" {
		switch {
		case session != "":
			output = "session-" + sanitizeForFilename(session) + ".codexbundle"
		case f.all:
			output = "codex-sessions.codexbundle"
		default:
			output = defaultBundleName(absProject)
		}
	}

	result, err := bundle.Export(home, bundle.ExportOptions{
		ProjectPath:     absProject,
		OutputPath:      output,
		IncludeArchived: f.includeArchived,
		Since:           since,
		SessionID:       session,
		WithGit:         f.withGit,
	})
	if err != nil {
		for _, w := range result.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", w)
		}
		fmt.Fprintf(stderr, "error: export failed: %v\n", err)
		return 1
	}

	if encryptRequested {
		encPath := output + crypt.Extension
		err := crypt.Encrypt(output, encPath, crypt.EncryptOptions{
			Recipients:     f.encryptTo,
			RecipientsFile: f.recipientsFile,
			Passphrase:     f.passphrase,
		})
		// The plaintext bundle is intermediate; remove it whether or not
		// encryption succeeded so a clear bundle is never left behind.
		os.Remove(output)
		if err != nil {
			os.Remove(encPath)
			fmt.Fprintf(stderr, "error: encrypt failed: %v\n", err)
			return 1
		}
		result.BundlePath = encPath
	}

	if f.jsonOut {
		printExportJSON(stdout, result)
	} else {
		printExport(stdout, absProject, session, result)
	}
	return 0
}

// ageMissingMessage is the shared guidance shown when encryption/decryption is
// requested but the age binary is not installed.
const ageMissingMessage = "age is not installed or not on PATH; install age (https://github.com/FiloSottile/age) to use bundle encryption"

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

// sanitizeForFilename keeps only characters safe in a filename (thread ids are
// UUIDs, so this is just a guard against an unexpected prefix value).
func sanitizeForFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "codex-session"
	}
	return out
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
	bundlePath, cleanup, code := resolveBundlePath(f, stderr)
	if code != 0 {
		return code
	}
	defer cleanup()

	res, err := bundle.Inspect(bundlePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if f.jsonOut {
		printInspectJSON(stdout, f.positional[0], res)
	} else {
		printInspect(stdout, f.positional[0], res)
	}
	return 0
}

// resolveBundlePath returns a plaintext bundle path usable by inspect/import. If
// the positional input is an encrypted (.age) bundle, it is decrypted to a
// temporary file (requiring --identity or --passphrase) and the returned cleanup
// removes that temporary file. For a plain bundle the input is returned as-is
// with a no-op cleanup. The returned code is non-zero on error.
func resolveBundlePath(f commonFlags, stderr io.Writer) (string, func(), int) {
	in := f.positional[0]
	noop := func() {}
	if !strings.EqualFold(filepath.Ext(in), crypt.Extension) {
		return in, noop, 0
	}
	if f.identity == "" && !f.passphrase {
		fmt.Fprintln(stderr, "error: "+in+" is an encrypted bundle; pass --identity <file> or --passphrase to decrypt it")
		return "", noop, 2
	}
	if !crypt.Available() {
		fmt.Fprintln(stderr, "error: "+ageMissingMessage)
		return "", noop, 1
	}
	tmpDir, err := os.MkdirTemp("", "codex-sync-dec-")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot create temp dir: %v\n", err)
		return "", noop, 1
	}
	cleanup := func() { os.RemoveAll(tmpDir) }
	// Fixed inner name in a fresh dir so age never refuses to overwrite.
	out := filepath.Join(tmpDir, "bundle.codexbundle")
	if err := crypt.Decrypt(in, out, crypt.DecryptOptions{
		IdentityFile: f.identity,
		Passphrase:   f.passphrase,
	}); err != nil {
		cleanup()
		fmt.Fprintf(stderr, "error: decrypt failed: %v\n", err)
		return "", noop, 1
	}
	return out, cleanup, 0
}

func runImport(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) != 1 {
		fmt.Fprintln(stderr, "usage: codex-sync import <file.codexbundle> [--dry-run] [--session <id>] [--project <path>] [--map-cwd OLD=NEW] [--replace-with-backup] [--import-as-copy] [--clone <dir>]")
		return 2
	}
	if f.replaceBackup && f.importAsCopy {
		fmt.Fprintln(stderr, "error: --replace-with-backup and --import-as-copy are mutually exclusive (they resolve conflicts in opposite ways)")
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

	bundlePath, cleanup, code := resolveBundlePath(f, stderr)
	if code != 0 {
		return code
	}
	defer cleanup()

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:        bundlePath,
		DryRun:            f.dryRun,
		ProjectPath:       absProject,
		MapCWD:            mappings,
		ReplaceWithBackup: f.replaceBackup,
		ImportAsCopy:      f.importAsCopy,
		SessionIDs:        f.sessions,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: import failed: %v\n", err)
		return 1
	}
	if f.jsonOut {
		printImportJSON(stdout, f.positional[0], res)
	} else {
		printImport(stdout, f.positional[0], res)
	}

	if f.cloneDir != "" {
		// In --json mode keep stdout pure JSON: clone progress goes to stderr.
		cloneOut := stdout
		if f.jsonOut {
			cloneOut = stderr
		}
		if code := cloneProject(f, res, cloneOut, stderr); code != 0 {
			return code
		}
	}
	return 0
}

// pushProject handles the opt-in --git-push step on export: it pushes the
// project's current branch to its git remote. This is the only outbound action
// on export, it is explicit, and it uploads your code to your own remote — never
// your sessions, and never to any codex-sync service. It returns a non-zero exit
// code on failure so the export aborts before writing a bundle that would
// misleadingly claim a commit is fetchable.
func pushProject(absProject, session string, all bool, stdout, stderr io.Writer) int {
	if all || session != "" {
		fmt.Fprintln(stderr, "error: --git-push pushes one project's code, so it is not valid with --all or --session")
		return 2
	}
	if !git.Available() {
		fmt.Fprintln(stderr, "error: git is not installed or not on PATH; cannot --git-push")
		return 1
	}
	if !git.IsRepo(absProject) {
		fmt.Fprintf(stderr, "error: %s is not a git repository; nothing to --git-push\n", absProject)
		return 1
	}
	fmt.Fprintln(stdout, "Pushing your project's code to its git remote (--git-push)…")
	remote, branch, err := git.Push(absProject)
	if err != nil {
		fmt.Fprintf(stderr, "error: git push failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Pushed branch %q to remote %q.\n", branch, remote)
	fmt.Fprintln(stdout, "(This uploads your code to your own git remote only — codex-sync never uploads your sessions.)")
	fmt.Fprintln(stdout)
	return 0
}

// cloneProject handles the opt-in --clone step: it clones the bundle's recorded
// git remote into the target directory. It is intentionally separate from the
// session import (which never touches the network or files outside Codex home).
func cloneProject(f commonFlags, res bundle.ImportResult, stdout, stderr io.Writer) int {
	if f.dryRun {
		fmt.Fprintln(stdout, "\n--clone skipped because --dry-run was used.")
		return 0
	}
	gi := res.Manifest.Git
	if gi == nil || gi.RemoteURL == "" {
		fmt.Fprintln(stderr, "error: --clone given but the bundle records no git remote URL")
		return 1
	}
	fmt.Fprintf(stdout, "\nCloning %s into %s ...\n", gi.RemoteURL, f.cloneDir)
	if err := git.Clone(gi.RemoteURL, f.cloneDir, gi.CommitSHA); err != nil {
		fmt.Fprintf(stderr, "error: clone failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Clone complete.")
	if gi.CommitSHA != "" {
		fmt.Fprintf(stdout, "Checked out commit %s.\n", gi.CommitSHA)
	}
	abs, err := filepath.Abs(f.cloneDir)
	if err == nil {
		fmt.Fprintf(stdout, "If the session's recorded cwd differs from %s, re-import with\n--map-cwd \"<old-cwd>=%s\" so it appears under that project in Codex.\n", abs, abs)
	}
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
  ui        Interactive mode: a guided menu that builds and runs the commands
            below for you (shows the equivalent command each time)
  app       Launch the local desktop GUI in your browser (loopback-only,
            nothing is uploaded)
  version   Print the codex-sync version (also --version)
  completion Print a shell completion script (bash, zsh, or fish)
  help      Show this help

Flags:
  --codex-home <path>   Use a specific Codex home instead of ~/.codex
                        (also honors $CODEX_HOME)
  --include-archived    list, export: also consider archived sessions
  --json                doctor/list/inspect/export/import: print a machine-
                        readable JSON summary on stdout instead of human text
  --port <n>            app: serve the desktop GUI on this port (default: a free
                        port chosen automatically)
  --no-browser          app: do not auto-open the browser; just print the URL
  --project <path>      export: filter sessions by recorded cwd
                        import: warn on cwd mismatch (never rewrites paths)
  --all                 export: include every session (no cwd filter);
                        mutually exclusive with --project
  --session <id>        export: export only the session with this thread id
                        (a unique prefix is enough); ignores cwd filtering
                        import: import only the session(s) with this thread id
                        (a unique prefix is enough); repeatable to pick several
  --since <when>        export: only sessions updated at/after <when>, where
                        <when> is a date (YYYY-MM-DD) or a duration (7d, 48h, 90m)
  --with-git            export: also record the project's git remote/branch/
                        commit (and dirty/unpushed status) in the bundle, even
                        with --all or --session
  --git-push            export: push the project's current branch to its git
                        remote first, so the recorded commit is fetchable on the
                        other machine. Uploads your code to your own remote only,
                        never your sessions. Opt-in; needs a project and a remote
  --output, -o <path>   export: bundle output path (default <project>.codexbundle)
  --dry-run             import: validate and report only, write nothing
  --map-cwd OLD=NEW     import: rewrite a session's recorded cwd from OLD to NEW
                        so it lands in the right local project (repeatable;
                        plain .jsonl only — .zst sessions are not rewritten)
  --replace-with-backup import: on a conflict (a local session changed since a
                        previous import), overwrite the local file with the
                        bundle's version after saving a backup next to it
                        (default is to skip conflicts and never overwrite)
  --import-as-copy      import: on a conflict, import the bundle's version as a
                        brand-new session (fresh id + filename) instead of
                        skipping it, leaving the local session untouched
                        (mutually exclusive with --replace-with-backup;
                        plain .jsonl only — .zst conflicts stay skipped)
  --clone <dir>         import: after importing, clone the bundle's recorded git
                        remote into <dir> and check out the recorded commit
  --encrypt-to <rcpt>   export: encrypt the bundle to an age recipient
                        (age1.../ssh-ed25519 ...) -> <output>.age (repeatable)
  --recipients-file <f> export: encrypt to every age recipient listed in <f>
  --passphrase          export: encrypt with an interactive passphrase
                        import/inspect: decrypt a passphrase-encrypted bundle
  --identity <file>     import/inspect: age identity (private key) file used to
                        decrypt a .age bundle

Optional external tools (only needed for the matching feature; the core commands
need none):
  age   encryption (--encrypt-to/--passphrase, decrypting .age bundles)
        https://github.com/FiloSottile/age
  git   --with-git on export and --clone on import
  zstd  reading metadata of compressed .jsonl.zst sessions (export/list/inspect)
        https://github.com/facebook/zstd
If a tool is missing, the matching feature errors with guidance or is skipped;
nothing else is affected. .age bundles are auto-detected on import/inspect.

Examples:
  codex-sync ui                            # interactive, guided menu
  codex-sync doctor
  codex-sync list
  codex-sync export --project .            # -> <project>.codexbundle
  codex-sync export --project . --with-git # also record git remote/commit
  codex-sync export --all                  # -> codex-sessions.codexbundle
  codex-sync export --all --since 7d       # everything updated in the last 7 days
  codex-sync export --project . --since 2026-06-01
  codex-sync export --session 9f3c1a2b     # one session by thread-id prefix
  codex-sync inspect ./my-project.codexbundle
  codex-sync import ./my-project.codexbundle --dry-run
  codex-sync import ./my-project.codexbundle
  codex-sync import ./my-project.codexbundle --map-cwd "/old/path=/new/path"
  codex-sync import ./my-project.codexbundle --replace-with-backup
  codex-sync import ./my-project.codexbundle --import-as-copy
  codex-sync import ./my-project.codexbundle --clone ~/dev/project
  codex-sync export --project . --encrypt-to age1qz...   # -> <project>.codexbundle.age
  codex-sync export --all --passphrase                   # passphrase-encrypted
  codex-sync import ./my-project.codexbundle.age --identity ~/.age/key.txt
  codex-sync inspect ./my-project.codexbundle.age --passphrase

After importing, restart the Codex App (or run Codex again) so it scans and
reconciles the imported rollout files.

Notes:
  codex-sync never modifies Codex's SQLite state DB; Codex rebuilds its own
  index from the JSONL files on its next scan.
  .codexbundle files may contain prompts, code, terminal output, paths, and
  secrets — do not share them publicly. See docs/safety.md.
`)
}
