package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
)

// exportChoices holds the answers gathered by the interactive export wizard.
// It is converted to a flag argv by buildExportArgs, which is pure and tested.
type exportChoices struct {
	mode        string // "current" | "all" | "session" | "project"
	tool        agent.Kind
	projectPath string
	sessionID   string
	withGit     bool
	since       string
	encryptMode string // "none" | "passphrase" | "recipient"
	recipient   string
	output      string
	codexHome   string
	claudeHome  string
}

// importChoices holds the answers gathered by the interactive import wizard.
type importChoices struct {
	bundle        string
	merge         bool
	replaceBackup bool
	importAsCopy  bool
	project       string
	mapCWD        []string
	mapCWDHere    bool
	hereDir       string
	clone         string
	decryptMode   string // "" | "passphrase" | "identity"
	identity      string
	codexHome     string
	claudeHome    string
}

// buildExportArgs turns export wizard answers into the equivalent CLI argv. It
// mirrors exactly what a user would type, so the wizard can both show and run
// the real command.
func buildExportArgs(c exportChoices) []string {
	args := []string{"export"}
	if c.tool == agent.Claude {
		args = append(args, "--tool", "claude")
	}
	switch c.mode {
	case "all":
		args = append(args, "--all")
	case "session":
		args = append(args, "--session", c.sessionID)
	case "project":
		args = append(args, "--project", stripQuotes(c.projectPath))
	default: // "current"
		args = append(args, "--project", ".")
	}
	if c.withGit {
		args = append(args, "--with-git")
	}
	if c.since != "" {
		args = append(args, "--since", c.since)
	}
	switch c.encryptMode {
	case "passphrase":
		args = append(args, "--passphrase")
	case "recipient":
		if c.recipient != "" {
			args = append(args, "--encrypt-to", c.recipient)
		}
	}
	if c.output != "" {
		args = append(args, "--output", stripQuotes(c.output))
	}
	if c.codexHome != "" {
		args = append(args, "--codex-home", c.codexHome)
	}
	if c.claudeHome != "" {
		args = append(args, "--claude-home", c.claudeHome)
	}
	return args
}

// buildImportArgs turns import wizard answers into the equivalent CLI argv.
// dryRun controls whether --dry-run is appended, so the same answers can drive
// both the safety preview and the real run.
func buildImportArgs(c importChoices, dryRun bool) []string {
	args := []string{"import", stripQuotes(c.bundle)}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if c.merge {
		args = append(args, "--merge")
	}
	if c.replaceBackup {
		args = append(args, "--replace-with-backup")
	}
	if c.importAsCopy {
		args = append(args, "--import-as-copy")
	}
	if c.project != "" {
		args = append(args, "--project", stripQuotes(c.project))
	}
	if c.mapCWDHere {
		args = append(args, "--map-cwd-here")
	}
	for _, m := range c.mapCWD {
		if m != "" {
			args = append(args, "--map-cwd", m)
		}
	}
	if c.clone != "" {
		args = append(args, "--clone", stripQuotes(c.clone))
	}
	switch c.decryptMode {
	case "passphrase":
		args = append(args, "--passphrase")
	case "identity":
		if c.identity != "" {
			args = append(args, "--identity", stripQuotes(c.identity))
		}
	}
	if c.codexHome != "" {
		args = append(args, "--codex-home", c.codexHome)
	}
	if c.claudeHome != "" {
		args = append(args, "--claude-home", c.claudeHome)
	}
	return args
}

// buildInspectArgs turns an inspect selection into the equivalent CLI argv.
func buildInspectArgs(path, decryptMode, identity string) []string {
	args := []string{"inspect", stripQuotes(path)}
	switch decryptMode {
	case "passphrase":
		args = append(args, "--passphrase")
	case "identity":
		if identity != "" {
			args = append(args, "--identity", stripQuotes(identity))
		}
	}
	return args
}

// stripQuotes removes a single pair of matching surrounding quotes (and trims
// whitespace) from a path the user typed. In a shell the shell removes quotes,
// but inside an interactive text field they survive as literal characters and
// would corrupt the path (e.g. turn an absolute path into a relative one). This
// makes the wizard forgiving of a habitually quoted Windows path.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// formatArgv renders an argv the way a user would type it, quoting any argument
// that contains a space so the echoed command is copy-pasteable.
func formatArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t") {
			parts[i] = `"` + a + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// runUI drives the interactive wizard. It collects answers with huh forms,
// composes the equivalent argv, echoes it, and runs it through the same Run
// entrypoint as the flag CLI — so behavior is identical and nothing is hidden.
func runUI(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	// The wizard needs an interactive terminal. In a pipe, CI, or redirected
	// input there is nothing to drive it, so fail fast with guidance instead of
	// blocking on a prompt that can never be answered.
	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(stderr, "error: `cct ui` needs an interactive terminal.")
		fmt.Fprintln(stderr, "Run it directly in your terminal, or use the flag-based commands")
		fmt.Fprintln(stderr, "(see `cct help`).")
		return 2
	}

	// Decide which agent this session targets. An explicit --tool wins; otherwise
	// auto-detect, and if both Codex and Claude Code are installed, let the user
	// choose. Import always follows the bundle's own tool regardless of this.
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	if f.tool == "" {
		ch, _ := codexhome.Detect(f.codexHome)
		clh, _ := claudehome.Detect(f.claudeHome)
		if ch.RootExists() && clh.RootExists() {
			var t string
			if !runField(huh.NewSelect[string]().
				Title("Which tool's sessions?").
				Options(
					huh.NewOption("Codex (~/.codex)", "codex"),
					huh.NewOption("Claude Code (~/.claude)", "claude"),
				).
				Value(&t), stderr) {
				return 0
			}
			kind, _ = agent.Parse(t)
		}
	}

	for {
		var action string
		err := huh.NewSelect[string]().
			Title("cct — what would you like to do?").
			Options(
				huh.NewOption("Export sessions to a bundle", "export"),
				huh.NewOption("Import a bundle", "import"),
				huh.NewOption("Search sessions", "search"),
				huh.NewOption("Inspect a bundle", "inspect"),
				huh.NewOption("List local sessions", "list"),
				huh.NewOption("Scan for secrets", "scan"),
				huh.NewOption("Doctor (health check)", "doctor"),
				huh.NewOption("Quit", "quit"),
			).
			Value(&action).
			Run()
		if err != nil {
			// Treat abort/EOF (Ctrl-C, no TTY) as "quit" rather than an error.
			if errors.Is(err, huh.ErrUserAborted) {
				return 0
			}
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}

		switch action {
		case "quit":
			return 0
		case "export":
			uiExport(f, kind, stdout, stderr)
		case "import":
			uiImport(f, stdout, stderr)
		case "inspect":
			uiInspect(f, stdout, stderr)
		case "search":
			uiSearch(f, kind, stdout, stderr)
		case "list":
			runBuilt(withToolHome([]string{"list"}, kind, f), stdout, stderr)
		case "scan":
			runBuilt(withToolHome([]string{"scan"}, kind, f), stdout, stderr)
		case "doctor":
			runBuilt(withToolHome([]string{"doctor"}, kind, f), stdout, stderr)
		}
	}
}

// withToolHome appends the agent selector and the matching home override (when
// the user passed one to `ui`) so the echoed command targets the chosen tool.
func withToolHome(argv []string, kind agent.Kind, f commonFlags) []string {
	if kind == agent.Claude {
		argv = append(argv, "--tool", "claude")
		if f.claudeHome != "" {
			argv = append(argv, "--claude-home", f.claudeHome)
		}
		return argv
	}
	if f.codexHome != "" {
		argv = append(argv, "--codex-home", f.codexHome)
	}
	return argv
}

// runBuilt echoes the composed command and runs it through the normal CLI path.
func runBuilt(argv []string, stdout, stderr io.Writer) {
	fmt.Fprintf(stdout, "\n$ cct %s\n\n", formatArgv(argv))
	Run(argv, stdout, stderr)
	fmt.Fprintln(stdout)
}

// uiSearch prompts for a query and runs `cct search`, echoing the command.
func uiSearch(f commonFlags, kind agent.Kind, stdout, stderr io.Writer) {
	var query string
	if !runField(huh.NewInput().
		Title("Search your sessions for…\n(matches the conversation text)").
		Value(&query), stderr) {
		return
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	var asRegex bool
	if !runField(huh.NewConfirm().
		Title("Treat the query as a regular expression?").
		Affirmative("Yes").
		Negative("No, plain text").
		Value(&asRegex), stderr) {
		return
	}
	argv := []string{"search", query}
	if asRegex {
		argv = append(argv, "--regex")
	}
	runBuilt(withToolHome(argv, kind, f), stdout, stderr)
}

func uiExport(f commonFlags, kind agent.Kind, stdout, stderr io.Writer) {
	c := exportChoices{tool: kind, codexHome: f.codexHome, claudeHome: f.claudeHome}
	if !runField(huh.NewSelect[string]().
		Title("What do you want to export?").
		Options(
			huh.NewOption("Sessions from one project folder (pick from a list)", "project"),
			huh.NewOption("Everything (all sessions)", "all"),
			huh.NewOption("One session (pick from a list)", "session"),
			huh.NewOption("This project (the folder I'm running from)", "current"),
		).
		Value(&c.mode), stderr) {
		return
	}

	switch c.mode {
	case "project":
		path, ok := pickProjectFolder(f, kind, stdout, stderr)
		if !ok {
			return
		}
		c.projectPath = path
	case "session":
		id, ok := pickSession(f, kind, stdout, stderr)
		if !ok {
			return
		}
		c.sessionID = id
	}

	if !runField(huh.NewConfirm().
		Title("Also record the project's git remote/commit? (--with-git)").
		Value(&c.withGit), stderr) {
		return
	}
	// Give immediate feedback: if we know the project folder and it is not a git
	// repository, there is nothing to record — say so now rather than after the
	// export runs, so the user is not surprised by a missing clone option later.
	if c.withGit {
		if dir := exportGitDir(c); dir != "" && !git.IsRepo(dir) {
			fmt.Fprintf(stdout, "\nNote: %s is not a git repository, so no git remote/commit\nwill be recorded (the import side will have nothing to clone).\n\n", dir)
		}
	}

	ok := runField(huh.NewInput().
		Title("Only sessions updated since? (blank = all; e.g. 7d or 2026-06-01)").
		Value(&c.since), stderr) &&
		runField(huh.NewSelect[string]().
			Title("Encrypt the bundle?").
			Options(
				huh.NewOption("No", "none"),
				huh.NewOption("Yes, with a passphrase", "passphrase"),
				huh.NewOption("Yes, to an age recipient", "recipient"),
			).
			Value(&c.encryptMode), stderr)
	if !ok {
		return
	}
	if c.encryptMode == "recipient" {
		if !runField(huh.NewInput().
			Title("age recipient (age1... or ssh-ed25519 ...)").
			Value(&c.recipient), stderr) {
			return
		}
	}
	if !runField(huh.NewInput().
		Title("Output path (blank = default name)").
		Value(&c.output), stderr) {
		return
	}

	runBuilt(buildExportArgs(c), stdout, stderr)
}

func uiImport(f commonFlags, stdout, stderr io.Writer) {
	c := importChoices{codexHome: f.codexHome, claudeHome: f.claudeHome}

	// 1) Which bundle? (Tolerate a path pasted with surrounding quotes.)
	if !runField(huh.NewInput().
		Title("Bundle file to import\n(drag the .codexbundle or .age file into this window, or paste its path)").
		Value(&c.bundle).
		Validate(fileMustExist), stderr) {
		return
	}
	c.bundle = stripQuotes(c.bundle)

	// 2) If the bundle is encrypted, we need to know how to decrypt it before we
	//    can read it and guide the rest of the wizard.
	if strings.HasSuffix(strings.ToLower(c.bundle), ".age") {
		if !runField(huh.NewSelect[string]().
			Title("This bundle is encrypted. Decrypt with?").
			Options(
				huh.NewOption("Passphrase", "passphrase"),
				huh.NewOption("Identity (private key) file", "identity"),
			).
			Value(&c.decryptMode), stderr) {
			return
		}
		if c.decryptMode == "identity" {
			if !runField(huh.NewInput().
				Title("age identity file").
				Value(&c.identity).
				Validate(fileMustExist), stderr) {
				return
			}
			c.identity = stripQuotes(c.identity)
		}
	}

	// 3) Read the bundle (decrypting to a temp file if needed) so we can guide
	//    the user with real data instead of asking them to type paths from memory.
	plain, cleanup, ok := resolveBundleForRead(c, stderr)
	if !ok {
		return
	}
	defer cleanup()

	res, err := bundle.Inspect(plain)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot read bundle: %v\n", err)
		return
	}
	// The bundle records which agent it came from; that determines the
	// destination home and the wording below (Codex vs Claude Code).
	bkind := agent.Normalize(agent.Kind(res.Manifest.Tool))
	summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)

	fmt.Fprintf(stdout, "\nThis bundle contains %s from %s:\n",
		plural(len(res.Manifest.Sessions), "session"), plural(len(summary.Dirs), "project folder"))
	for _, d := range summary.Dirs {
		where := "found on this computer"
		if !d.ExistsLocal {
			where = "NOT on this computer"
		}
		fmt.Fprintf(stdout, "  - %s\n      %s, %s\n", safeTerminal(d.Path), plural(d.Count, "session"), where)
	}
	if summary.UnknownCWD > 0 {
		fmt.Fprintf(stdout, "  - %s with no recorded folder (compressed/unknown)\n", plural(summary.UnknownCWD, "session"))
	}
	fmt.Fprintln(stdout)

	// 4) For every missing folder, offer a clear fix in plain language. The user
	//    never types the OLD path — it comes straight from the bundle.
	if summary.MissingCount == 0 && len(summary.Dirs) > 0 {
		fmt.Fprintf(stdout, "All project folders already exist here — your sessions will\nshow up in %s automatically after import.\n", bkind.Label())
	}
	here, _ := os.Getwd()
	singleProject := len(summary.Dirs) == 1
	for _, d := range summary.Dirs {
		if d.ExistsLocal {
			continue
		}
		options := []huh.Option[string]{}
		// Offer the one-click "put it under the folder I'm in now" shortcut. It is
		// the --map-cwd-here flag, valid only for a single-project bundle; for a
		// multi-project bundle we fall back to an explicit per-folder mapping.
		if here != "" {
			options = append(options, huh.NewOption(
				fmt.Sprintf("Put these sessions under the folder I'm in now (%s)", here), "here"))
		}
		options = append(options,
			huh.NewOption("Create that exact folder here (keep the original path)", "create"),
			huh.NewOption("Point these sessions to a different folder on this computer", "remap"),
			huh.NewOption("Skip for now (they stay hidden until the folder exists)", "skip"),
		)
		var choice string
		if !runField(huh.NewSelect[string]().
			Title(fmt.Sprintf("%s recorded in this folder, which is NOT on this computer:\n  %s\nWhat should I do so these sessions appear in %s?",
				plural(d.Count, "session"), safeTerminal(d.Path), bkind.Label())).
			Options(options...).
			Value(&choice), stderr) {
			return
		}
		switch choice {
		case "here":
			if singleProject {
				// Use the dedicated flag so the printed command teaches it.
				c.mapCWDHere = true
				c.hereDir = here
			} else {
				c.mapCWD = append(c.mapCWD, d.Path+"="+here)
			}
		case "create":
			if err := os.MkdirAll(d.Path, 0o755); err != nil {
				fmt.Fprintf(stderr, "  could not create %s: %v\n  (these sessions will stay hidden until that folder exists)\n", d.Path, err)
			} else {
				fmt.Fprintf(stdout, "  Created folder: %s\n", d.Path)
			}
		case "remap":
			var np string
			if !runField(huh.NewInput().
				Title("Folder on THIS computer where these sessions should appear\n(full path, e.g. C:\\Users\\you\\Desktop\\project)").
				Value(&np).
				Validate(mustBeAbsolutePath), stderr) {
				return
			}
			np = stripQuotes(np)
			if !bundle.DirExists(np) {
				var mk bool
				if !runField(huh.NewConfirm().
					Title(fmt.Sprintf("%s doesn't exist yet. Create it?", np)).
					Value(&mk), stderr) {
					return
				}
				if mk {
					if err := os.MkdirAll(np, 0o755); err != nil {
						fmt.Fprintf(stderr, "  could not create %s: %v\n", np, err)
					} else {
						fmt.Fprintf(stdout, "  Created folder: %s\n", np)
					}
				}
			}
			c.mapCWD = append(c.mapCWD, d.Path+"="+np)
		}
	}

	// 5) Quietly dry-run the import so we can show a clean, accurate preview and
	//    only ask about conflicts if there really are any. The destination home
	//    follows the bundle's tool.
	home, ok := uiImportHome(bkind, c, stderr)
	if !ok {
		return
	}
	mappings, err := bundle.ParseCWDMappings(c.mapCWD)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return
	}
	dry, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath: plain, DryRun: true,
		MapCWD: mappings, MapCWDHere: c.mapCWDHere, HereDir: c.hereDir,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return
	}

	fmt.Fprintln(stdout, "\nHere's what will happen:")
	fmt.Fprintf(stdout, "  New sessions to add:        %d\n", dry.Imported)
	fmt.Fprintf(stdout, "  Already on this computer:   %d\n", dry.SkippedIdentical)
	if dry.Mapped > 0 {
		fmt.Fprintf(stdout, "  Redirected to a new folder: %d\n", dry.Mapped)
	}
	if dry.Conflicts > 0 {
		fmt.Fprintf(stdout, "  Differ from a local copy:   %d\n", dry.Conflicts)
	}
	fmt.Fprintln(stdout)

	if dry.Conflicts > 0 {
		var conflictChoice string
		if !runField(huh.NewSelect[string]().
			Title(fmt.Sprintf("%s already exist here but differ from the bundle.\nWhat should I do with them?", plural(dry.Conflicts, "session"))).
			Options(
				huh.NewOption("Sync — append new messages where a session just grew; keep mine where both changed", "merge"),
				huh.NewOption("Keep mine (skip the bundle's versions)", "skip"),
				huh.NewOption("Replace mine with the bundle's version (keep a backup of each)", "replace"),
				huh.NewOption("Keep both — import the bundle's versions as new sessions", "copy"),
			).
			Value(&conflictChoice), stderr) {
			return
		}
		switch conflictChoice {
		case "merge":
			c.merge = true
		case "replace":
			c.replaceBackup = true
		case "copy":
			c.importAsCopy = true
		}
	}

	// 6) If the bundle recorded a git remote, offer to clone the code too.
	if gi := res.Manifest.Git; gi != nil && !gi.Empty() && gi.RemoteURL != "" {
		var doClone bool
		if !runField(huh.NewConfirm().
			Title(fmt.Sprintf("This bundle also records the project's git remote:\n  %s\nClone the code onto this computer too?", safeTerminal(gi.RemoteURL))).
			Value(&doClone), stderr) {
			return
		}
		if doClone {
			var dir string
			if !runField(huh.NewInput().
				Title("Folder to clone the code into (full path)").
				Value(&dir).
				Validate(mustBeAbsolutePath), stderr) {
				return
			}
			c.clone = stripQuotes(dir)
		}
	}

	// 7) Final confirmation, then run the real command (echoed for transparency).
	var proceed bool
	if !runField(huh.NewConfirm().
		Title("Import now?").
		Affirmative("Import").
		Negative("Cancel").
		Value(&proceed), stderr) {
		return
	}
	if !proceed {
		fmt.Fprintln(stdout, "Cancelled; nothing was changed.")
		return
	}
	runBuilt(buildImportArgs(c, false), stdout, stderr)
}

// uiImportHome resolves the destination home for the import wizard's dry-run,
// matching the bundle's recorded tool (the same rule the real import uses).
func uiImportHome(bkind agent.Kind, c importChoices, stderr io.Writer) (codexhome.Home, bool) {
	if bkind == agent.Claude {
		clh, err := claudehome.Detect(c.claudeHome)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot determine Claude Code home: %v\n", err)
			return codexhome.Home{}, false
		}
		return codexhome.Home{Root: clh.Root, SessionsDir: clh.ProjectsDir, Source: clh.Source}, true
	}
	home, err := codexhome.Detect(c.codexHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Codex home: %v\n", err)
		return codexhome.Home{}, false
	}
	return home, true
}

// resolveBundleForRead returns a plaintext bundle path the wizard can read
// (Inspect / dry-run Import) without writing anything, decrypting a .age bundle
// to a temp file when needed. The returned cleanup removes any temp file.
func resolveBundleForRead(c importChoices, stderr io.Writer) (string, func(), bool) {
	f := commonFlags{
		positional: []string{c.bundle},
		identity:   c.identity,
		passphrase: c.decryptMode == "passphrase",
	}
	path, cleanup, code := resolveBundlePath(f, stderr)
	if code != 0 {
		return "", func() {}, false
	}
	return path, cleanup, true
}

// exportGitDir returns the local folder git discovery will run against for an
// export, when it is knowable up front: the picked project folder, or the
// current directory. For "all"/"session" exports the folder comes from a
// session's recorded cwd at export time, so we cannot pre-check it and return "".
func exportGitDir(c exportChoices) string {
	switch c.mode {
	case "project":
		return stripQuotes(c.projectPath)
	case "current":
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	return ""
}

// uiScan scans the local sessions for the chosen agent (Codex or Claude Code),
// so the export pickers list the right tool's projects and sessions.
func uiScan(f commonFlags, kind agent.Kind, stderr io.Writer) (sessions.ScanResult, bool) {
	if kind == agent.Claude {
		home, err := claudehome.Detect(f.claudeHome)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot determine Claude Code home: %v\n", err)
			return sessions.ScanResult{}, false
		}
		scan, err := claudesessions.Scan(home, claudesessions.ScanOptions{})
		if err != nil {
			fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
			return sessions.ScanResult{}, false
		}
		return scan, true
	}
	home, err := codexhome.Detect(f.codexHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Codex home: %v\n", err)
		return sessions.ScanResult{}, false
	}
	scan, err := sessions.Scan(home, sessions.ScanOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
		return sessions.ScanResult{}, false
	}
	return scan, true
}

// pickProjectFolder scans local sessions, groups them by recorded project folder,
// and lets the user pick one to export — so they choose from a list instead of
// typing a path. ok is false if the user aborted or there is nothing to pick.
func pickProjectFolder(f commonFlags, kind agent.Kind, stdout, stderr io.Writer) (string, bool) {
	scan, ok := uiScan(f, kind, stderr)
	if !ok {
		return "", false
	}
	ms := make([]bundle.ManifestSession, 0, len(scan.Sessions))
	for _, s := range scan.Sessions {
		ms = append(ms, bundle.ManifestSession{OriginalCWD: s.CWD})
	}
	summary := bundle.SummarizeCWDs(ms, bundle.DirExists)
	if len(summary.Dirs) == 0 {
		fmt.Fprintln(stdout, "No project folders were found in your local sessions.")
		return "", false
	}
	opts := make([]huh.Option[string], 0, len(summary.Dirs))
	for _, d := range summary.Dirs {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", d.Path, plural(d.Count, "session")), d.Path))
	}
	var chosen string
	if !runField(huh.NewSelect[string]().
		Title("Pick the project folder to export").
		Options(opts...).
		Value(&chosen), stderr) {
		return "", false
	}
	return chosen, true
}

// mustBeAbsolutePath is a huh validator that requires a non-empty absolute path,
// tolerating surrounding quotes the user may have pasted.
func mustBeAbsolutePath(s string) error {
	s = stripQuotes(s)
	if s == "" {
		return errors.New("a folder path is required")
	}
	if !filepath.IsAbs(s) {
		return errors.New("please enter a full path, e.g. C:\\Users\\you\\project")
	}
	return nil
}

func uiInspect(f commonFlags, stdout, stderr io.Writer) {
	var path string
	if !runField(huh.NewInput().
		Title("Bundle file to inspect (.codexbundle or .age)").
		Value(&path).
		Validate(fileMustExist), stderr) {
		return
	}
	path = stripQuotes(path)
	decryptMode, identity := "", ""
	if strings.HasSuffix(strings.ToLower(path), ".age") {
		if !runField(huh.NewSelect[string]().
			Title("This bundle is encrypted. Decrypt with?").
			Options(
				huh.NewOption("Passphrase", "passphrase"),
				huh.NewOption("Identity (private key) file", "identity"),
			).
			Value(&decryptMode), stderr) {
			return
		}
		if decryptMode == "identity" {
			if !runField(huh.NewInput().
				Title("age identity file").
				Value(&identity).
				Validate(fileMustExist), stderr) {
				return
			}
		}
	}
	runBuilt(buildInspectArgs(path, decryptMode, identity), stdout, stderr)
}

// pickSession scans the local Codex home and lets the user choose one session,
// returning its thread id. ok is false if the user aborted or there is nothing
// to pick.
func pickSession(f commonFlags, kind agent.Kind, stdout, stderr io.Writer) (string, bool) {
	scan, ok := uiScan(f, kind, stderr)
	if !ok {
		return "", false
	}
	var opts []huh.Option[string]
	for _, s := range scan.Sessions {
		if s.ThreadID == "" {
			continue // cannot export a session without a thread id
		}
		opts = append(opts, huh.NewOption(sessionLabel(s), s.ThreadID))
	}
	if len(opts) == 0 {
		fmt.Fprintln(stdout, "No sessions with a thread id were found to pick from.")
		return "", false
	}
	var id string
	if !runField(huh.NewSelect[string]().
		Title("Pick a session to export").
		Options(opts...).
		Value(&id), stderr) {
		return "", false
	}
	return id, true
}

// sessionLabel renders a one-line, length-bounded description of a session for
// the picker.
func sessionLabel(s sessions.Session) string {
	title := s.Preview
	if title == "" {
		title = s.FileName
	}
	title = truncate(title, 50)
	cwd := s.CWD
	if cwd == "" {
		cwd = "(no cwd)"
	}
	return fmt.Sprintf("%s — %s — %s", title, cwd, s.UpdatedAt().Format("2006-01-02 15:04"))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// fileMustExist is a huh validator that rejects a path that is not an existing
// file, so the wizard fails fast instead of deep inside a command.
func fileMustExist(s string) error {
	s = stripQuotes(s)
	if s == "" {
		return errors.New("a file path is required")
	}
	info, err := os.Stat(s)
	if err != nil {
		return fmt.Errorf("cannot open %q: %w", s, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory, not a file", s)
	}
	return nil
}

// runField runs a single huh field standalone and reports whether to continue.
// A user abort (Ctrl-C) returns false so the wizard unwinds to the main menu
// without printing a scary error.
func runField(field interface{ Run() error }, stderr io.Writer) bool {
	err := field.Run()
	if err == nil {
		return true
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return false
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return false
}
