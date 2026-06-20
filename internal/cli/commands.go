// Package cli implements the cct command-line interface. The CLI is a
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

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
	"github.com/ahmojo/codex-claude-transfer/internal/doctor"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/repair"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/webui"
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
	case "repair-times":
		return runRepairTimes(rest, stdout, stderr)
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
	claudeHome      string
	tool            string
	to              string
	includeArchived bool
	project         string
	output          string
	dryRun          bool
	all             bool
	since           string
	sessions        []string
	withGit         bool
	gitPush         bool
	stripImages     bool
	cloneDir        string
	mapCWD          []string
	mapCWDHere      bool
	encryptTo       []string
	recipientsFile  string
	passphrase      bool
	identity        string
	replaceBackup   bool
	importAsCopy    bool
	merge           bool
	flat            bool
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
		case arg == "--claude-home":
			val, err := takeValue(args, &i, "--claude-home")
			if err != nil {
				return f, err
			}
			f.claudeHome = val
		case hasPrefix(arg, "--claude-home="):
			f.claudeHome = arg[len("--claude-home="):]
		case arg == "--tool":
			val, err := takeValue(args, &i, "--tool")
			if err != nil {
				return f, err
			}
			f.tool = val
		case hasPrefix(arg, "--tool="):
			f.tool = arg[len("--tool="):]
		case arg == "--to":
			val, err := takeValue(args, &i, "--to")
			if err != nil {
				return f, err
			}
			f.to = val
		case hasPrefix(arg, "--to="):
			f.to = arg[len("--to="):]
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
		case arg == "--map-cwd-here":
			f.mapCWDHere = true
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
		case arg == "--strip-images":
			f.stripImages = true
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
		case arg == "--merge":
			f.merge = true
		case arg == "--flat":
			f.flat = true
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

func resolveClaudeHome(f commonFlags, stderr io.Writer) (claudehome.Home, bool) {
	home, err := claudehome.Detect(f.claudeHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Claude Code home: %v\n", err)
		return claudehome.Home{}, false
	}
	return home, true
}

// resolveTool decides which agent a command targets. An explicit --tool wins;
// otherwise it auto-detects: if a Claude Code home exists and a Codex home does
// not, it picks Claude, else it defaults to Codex (the original, backward-
// compatible behavior). The returned bool is false when --tool is invalid.
func resolveTool(f commonFlags, stderr io.Writer) (agent.Kind, bool) {
	if f.tool != "" {
		kind, err := agent.Parse(f.tool)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return "", false
		}
		return kind, true
	}
	ch, _ := codexhome.Detect(f.codexHome)
	clh, _ := claudehome.Detect(f.claudeHome)
	if clh.RootExists() && !ch.RootExists() {
		return agent.Claude, true
	}
	return agent.Codex, true
}

// resolveImportTarget reads the bundle's recorded tool and returns the matching
// destination home (as a codexhome.Home carrier whose Root is the agent's home).
// The bundle, not --tool, is authoritative; a disagreeing --tool is reported and
// ignored so a bundle is always written to the right place.
func resolveImportTarget(f commonFlags, bundlePath string, stderr io.Writer) (agent.Kind, codexhome.Home, bool) {
	insp, err := bundle.Inspect(bundlePath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return "", codexhome.Home{}, false
	}
	kind := agent.Normalize(agent.Kind(insp.Manifest.Tool))
	if f.tool != "" {
		if want, perr := agent.Parse(f.tool); perr == nil && want != kind {
			fmt.Fprintf(stderr, "note: --tool %s ignored; this bundle contains %s sessions\n", want, kind.Label())
		}
	}
	if kind == agent.Claude {
		clh, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return "", codexhome.Home{}, false
		}
		return kind, codexhome.Home{Root: clh.Root, SessionsDir: clh.ProjectsDir, Source: clh.Source}, true
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return "", codexhome.Home{}, false
	}
	return kind, home, true
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
		CodexHome:  f.codexHome,
		ClaudeHome: f.claudeHome,
		Port:       f.port,
		NoBrowser:  f.noBrowser,
	}, stdout, stderr)
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	var report doctor.Report
	if kind == agent.Claude {
		home, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		report = doctor.RunClaude(home)
	} else {
		home, ok := resolveHome(f, stderr)
		if !ok {
			return 1
		}
		report = doctor.Run(home)
	}
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
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	var scan sessions.ScanResult
	if kind == agent.Claude {
		home, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		scan, err = claudesessions.Scan(home, claudesessions.ScanOptions{IncludeArchived: f.includeArchived})
	} else {
		home, ok := resolveHome(f, stderr)
		if !ok {
			return 1
		}
		scan, err = sessions.Scan(home, sessions.ScanOptions{
			IncludeArchived:      f.includeArchived,
			DecompressCompressed: true,
		})
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
		return 1
	}
	if f.jsonOut {
		printListJSON(stdout, scan)
	} else {
		printList(stdout, kind, scan, f.flat)
	}
	return 0
}

func runExport(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	home, ok := resolveHome(f, stderr)
	if !ok {
		return 1
	}
	var claudeHome claudehome.Home
	if kind == agent.Claude {
		claudeHome, ok = resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
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
	// to any cct server — and runs before the export so the bundle records
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
			if kind == agent.Claude {
				output = "claude-sessions.codexbundle"
			} else {
				output = "codex-sessions.codexbundle"
			}
		default:
			output = defaultBundleName(absProject)
		}
	}

	result, err := bundle.Export(home, bundle.ExportOptions{
		Tool:            kind,
		ClaudeHome:      claudeHome,
		ProjectPath:     absProject,
		OutputPath:      output,
		IncludeArchived: f.includeArchived,
		Since:           since,
		SessionID:       session,
		WithGit:         f.withGit,
		StripImages:     f.stripImages,
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
		printExport(stdout, kind, absProject, session, result)
	}
	return 0
}

// ageMissingMessage is the shared guidance shown when encryption/decryption is
// requested but the age binary is not installed.
const ageMissingMessage = "age is not installed or not on PATH; install age (https://github.com/FiloSottile/age) to use bundle encryption"

// parseSince delegates to bundle.ParseSince so the CLI and the desktop UI share
// one definition of the --since grammar (a date or a d/h/m duration).
func parseSince(s string) (time.Time, error) {
	return bundle.ParseSince(s)
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
		fmt.Fprintln(stderr, "usage: cct inspect <file.codexbundle>")
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
	tmpDir, err := os.MkdirTemp("", "cct-dec-")
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
		fmt.Fprintln(stderr, "usage: cct import <file.codexbundle> [--dry-run] [--merge] [--session <id>] [--project <path>] [--map-cwd OLD=NEW | --map-cwd-here] [--replace-with-backup] [--import-as-copy] [--clone <dir>]")
		return 2
	}
	if f.replaceBackup && f.importAsCopy {
		fmt.Fprintln(stderr, "error: --replace-with-backup and --import-as-copy are mutually exclusive (they resolve conflicts in opposite ways)")
		return 2
	}

	var absProject string
	if f.project != "" {
		absProject, err = filepath.Abs(f.project)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot resolve project path %q: %v\n", f.project, err)
			return 1
		}
	}

	if f.mapCWDHere && len(f.mapCWD) > 0 {
		fmt.Fprintln(stderr, "error: --map-cwd and --map-cwd-here are mutually exclusive (use one or the other)")
		return 2
	}
	mappings, err := bundle.ParseCWDMappings(f.mapCWD)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	var hereDir string
	if f.mapCWDHere {
		hereDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "error: --map-cwd-here: cannot determine the current directory: %v\n", err)
			return 1
		}
	}

	bundlePath, cleanup, code := resolveBundlePath(f, stderr)
	if code != 0 {
		return code
	}
	defer cleanup()

	// --to turns import into a cross-agent handoff: translate the bundle's sessions
	// into the other agent's format and write them into that agent's home.
	if f.to != "" {
		return runTranslateImport(f, bundlePath, stdout, stderr)
	}

	// The bundle records which agent it came from; that, not --tool, decides where
	// the sessions are written. Resolve the matching home, and if --tool was given
	// and disagrees with the bundle, follow the bundle (and say so).
	kind, home, ok := resolveImportTarget(f, bundlePath, stderr)
	if !ok {
		return 1
	}

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:        bundlePath,
		DryRun:            f.dryRun,
		ProjectPath:       absProject,
		MapCWD:            mappings,
		MapCWDHere:        f.mapCWDHere,
		HereDir:           hereDir,
		ReplaceWithBackup: f.replaceBackup,
		ImportAsCopy:      f.importAsCopy,
		Merge:             f.merge,
		SessionIDs:        f.sessions,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: import failed: %v\n", err)
		return 1
	}
	if f.jsonOut {
		printImportJSON(stdout, f.positional[0], res)
	} else {
		printImport(stdout, kind, f.positional[0], res)
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

// runRepairTimes resets the modification time of session files that were imported
// with a wrong (import-time) mtime, so the agent stops re-parsing them on every
// open. It only changes file mtimes — never content, never the index/SQLite.
func runRepairTimes(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	var dirs []string
	if kind == agent.Claude {
		home, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		dirs = []string{home.ProjectsDir}
	} else {
		home, ok := resolveHome(f, stderr)
		if !ok {
			return 1
		}
		dirs = []string{home.SessionsDir}
		if f.includeArchived {
			dirs = append(dirs, home.ArchivedSessionsDir)
		}
	}
	res, err := repair.RepairTimes(dirs, repair.Options{DryRun: f.dryRun})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	printRepair(stdout, kind, res)
	return 0
}

// runTranslateImport performs a cross-agent handoff: it reads the bundle (in
// whatever agent's format it was exported), translates each session into the
// --to agent's format, and writes the results into that agent's home. The
// destination home is the --to agent's; the bundle's own tool is the source.
func runTranslateImport(f commonFlags, bundlePath string, stdout, stderr io.Writer) int {
	target, err := agent.Parse(f.to)
	if err != nil {
		fmt.Fprintf(stderr, "error: --to %v\n", err)
		return 2
	}
	var home codexhome.Home
	if target == agent.Claude {
		clh, ok := resolveClaudeHome(f, stderr)
		if !ok {
			return 1
		}
		home = codexhome.Home{Root: clh.Root, SessionsDir: clh.ProjectsDir, Source: clh.Source}
	} else {
		h, ok := resolveHome(f, stderr)
		if !ok {
			return 1
		}
		home = h
	}

	res, err := bundle.TranslateImport(home, bundle.TranslateOptions{
		BundlePath: bundlePath,
		TargetTool: target,
		DryRun:     f.dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: handoff failed: %v\n", err)
		return 1
	}
	printTranslate(stdout, f.positional[0], res)
	return 0
}

// pushProject handles the opt-in --git-push step on export: it pushes the
// project's current branch to its git remote. This is the only outbound action
// on export, it is explicit, and it uploads your code to your own remote — never
// your sessions, and never to any cct service. It returns a non-zero exit
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
	fmt.Fprintln(stdout, "(This uploads your code to your own git remote only — cct never uploads your sessions.)")
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
	fmt.Fprint(w, `cct — Codex & Claude Code session transfer (unofficial)

  Export. Move. Import. Continue your local coding-agent sessions on another machine.
  Works with both Codex (~/.codex) and Claude Code (~/.claude); pick one with --tool.

Usage:
  cct <command> [flags]

Commands:
  doctor    Read-only health check: find your Codex home, count sessions,
            and confirm SQLite will not be modified
  list      List discovered Codex sessions (preview, thread id, cwd, time)
  export    Export sessions for a project into a .codexbundle
  inspect   Show a bundle's manifest and contents, read-only (no extraction)
  import    Import a .codexbundle into your Codex home (never overwrites)
  repair-times  Reset imported session files' modification time to their real
            last-activity time, so the agent stops re-parsing them on every open
            (a one-time fix; only changes mtimes, never content or the index)
  ui        Interactive mode: a guided menu that builds and runs the commands
            below for you (shows the equivalent command each time)
  app       Launch the local desktop GUI in your browser (loopback-only,
            nothing is uploaded)
  version   Print the cct version (also --version)
  completion Print a shell completion script (bash, zsh, or fish)
  help      Show this help

Flags:
  --tool <codex|claude> Which agent's sessions to act on. Default: auto-detect
                        (Claude Code if only its home exists, otherwise Codex).
                        On import the bundle's recorded tool always wins.
  --codex-home <path>   Use a specific Codex home instead of ~/.codex
                        (also honors $CODEX_HOME)
  --claude-home <path>  Use a specific Claude Code home instead of ~/.claude
                        (also honors $CLAUDE_HOME)
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
  --strip-images        export: replace inline base64 images in each session with
                        a small placeholder, to shrink an image-heavy bundle.
                        Lossy (the pictures are dropped) and opt-in; the
                        conversation text is kept. Needs zstd for .jsonl.zst
  --output, -o <path>   export: bundle output path (default <project>.codexbundle)
  --dry-run             import: validate and report only, write nothing
  --merge               import: incremental sync. When a session already exists
                        locally but grew on the other device, append only the new
                        messages (the local file is a prefix of the bundle's, so
                        this is lossless). Sessions that changed on both sides stay
                        conflicts; combine with --replace-with-backup/--import-as-
                        copy to resolve those too
  --to <codex|claude>   import: cross-agent handoff. Instead of importing the
                        bundle's sessions natively, translate them into the OTHER
                        agent's format and write them into that agent's home. A
                        best-effort text handoff (conversation + a context
                        preamble; tool calls summarized), not a perfect clone
  --map-cwd OLD=NEW     import: rewrite a session's recorded cwd from OLD to NEW
                        so it lands in the right local project (repeatable;
                        plain .jsonl only — .zst sessions are not rewritten)
  --map-cwd-here        import: shorthand for --map-cwd that maps the bundle's
                        recorded project to the directory you run this from, so
                        you don't have to look up the old path. The sessions then
                        appear under the current folder's project (in Claude Code,
                        its sidebar group). Only for a single-project bundle; a
                        bundle spanning several projects is rejected as ambiguous
                        (use --map-cwd for those). Cannot be combined with --map-cwd
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
  cct ui                            # interactive, guided menu
  cct doctor
  cct doctor --tool claude          # check Claude Code instead of Codex
  cct list
  cct list --tool claude            # list Claude Code sessions
  cct export --project .            # -> <project>.codexbundle
  cct export --tool claude --project .   # export this project's Claude sessions
  cct export --project . --with-git # also record git remote/commit
  cct export --project . --strip-images  # drop embedded images to shrink it
  cct export --all                  # -> codex-sessions.codexbundle
  cct export --all --since 7d       # everything updated in the last 7 days
  cct export --project . --since 2026-06-01
  cct export --session 9f3c1a2b     # one session by thread-id prefix
  cct inspect ./my-project.codexbundle
  cct import ./my-project.codexbundle --dry-run
  cct import ./my-project.codexbundle
  cct import ./my-project.codexbundle --merge   # append new messages to grown sessions
  cct import ./my-project.codexbundle --map-cwd "/old/path=/new/path"
  cct import ./my-project.codexbundle --map-cwd-here   # group under the current folder
  cct import ./my-project.codexbundle --replace-with-backup
  cct import ./my-project.codexbundle --import-as-copy
  cct import ./my-project.codexbundle --to claude   # Codex bundle -> Claude Code
  cct import ./claude.codexbundle      --to codex    # Claude bundle -> Codex
  cct import ./my-project.codexbundle --clone ~/dev/project
  cct repair-times --dry-run         # preview the mtime fix for imported sessions
  cct repair-times                   # apply it (then restart Codex)
  cct export --project . --encrypt-to age1qz...   # -> <project>.codexbundle.age
  cct export --all --passphrase                   # passphrase-encrypted
  cct import ./my-project.codexbundle.age --identity ~/.age/key.txt
  cct inspect ./my-project.codexbundle.age --passphrase

After importing, run the agent again (restart the Codex App, or relaunch Claude
Code) so it discovers the imported session files.

Notes:
  cct never modifies Codex's SQLite state DB or Claude Code's ~/.claude.json;
  each agent rebuilds its own index from the JSONL files on its next scan.
  .codexbundle files may contain prompts, code, terminal output, paths, and
  secrets — do not share them publicly. See docs/safety.md.
`)
}
