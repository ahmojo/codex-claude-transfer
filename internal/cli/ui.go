package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
)

// exportChoices holds the answers gathered by the interactive export wizard.
// It is converted to a flag argv by buildExportArgs, which is pure and tested.
type exportChoices struct {
	mode        string // "current" | "all" | "session" | "project"
	projectPath string
	sessionID   string
	withGit     bool
	since       string
	encryptMode string // "none" | "passphrase" | "recipient"
	recipient   string
	output      string
	codexHome   string
}

// importChoices holds the answers gathered by the interactive import wizard.
type importChoices struct {
	bundle        string
	replaceBackup bool
	project       string
	mapCWD        []string
	clone         string
	decryptMode   string // "" | "passphrase" | "identity"
	identity      string
	codexHome     string
}

// buildExportArgs turns export wizard answers into the equivalent CLI argv. It
// mirrors exactly what a user would type, so the wizard can both show and run
// the real command.
func buildExportArgs(c exportChoices) []string {
	args := []string{"export"}
	switch c.mode {
	case "all":
		args = append(args, "--all")
	case "session":
		args = append(args, "--session", c.sessionID)
	case "project":
		args = append(args, "--project", c.projectPath)
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
		args = append(args, "--output", c.output)
	}
	if c.codexHome != "" {
		args = append(args, "--codex-home", c.codexHome)
	}
	return args
}

// buildImportArgs turns import wizard answers into the equivalent CLI argv.
// dryRun controls whether --dry-run is appended, so the same answers can drive
// both the safety preview and the real run.
func buildImportArgs(c importChoices, dryRun bool) []string {
	args := []string{"import", c.bundle}
	if dryRun {
		args = append(args, "--dry-run")
	}
	if c.replaceBackup {
		args = append(args, "--replace-with-backup")
	}
	if c.project != "" {
		args = append(args, "--project", c.project)
	}
	for _, m := range c.mapCWD {
		if m != "" {
			args = append(args, "--map-cwd", m)
		}
	}
	if c.clone != "" {
		args = append(args, "--clone", c.clone)
	}
	switch c.decryptMode {
	case "passphrase":
		args = append(args, "--passphrase")
	case "identity":
		if c.identity != "" {
			args = append(args, "--identity", c.identity)
		}
	}
	if c.codexHome != "" {
		args = append(args, "--codex-home", c.codexHome)
	}
	return args
}

// buildInspectArgs turns an inspect selection into the equivalent CLI argv.
func buildInspectArgs(path, decryptMode, identity string) []string {
	args := []string{"inspect", path}
	switch decryptMode {
	case "passphrase":
		args = append(args, "--passphrase")
	case "identity":
		if identity != "" {
			args = append(args, "--identity", identity)
		}
	}
	return args
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
		fmt.Fprintln(stderr, "error: `codex-sync ui` needs an interactive terminal.")
		fmt.Fprintln(stderr, "Run it directly in your terminal, or use the flag-based commands")
		fmt.Fprintln(stderr, "(see `codex-sync help`).")
		return 2
	}

	for {
		var action string
		err := huh.NewSelect[string]().
			Title("codex-sync — what would you like to do?").
			Options(
				huh.NewOption("Export sessions to a bundle", "export"),
				huh.NewOption("Import a bundle", "import"),
				huh.NewOption("Inspect a bundle", "inspect"),
				huh.NewOption("List local sessions", "list"),
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
			uiExport(f, stdout, stderr)
		case "import":
			uiImport(f, stdout, stderr)
		case "inspect":
			uiInspect(f, stdout, stderr)
		case "list":
			runBuilt(withHome([]string{"list"}, f), stdout, stderr)
		case "doctor":
			runBuilt(withHome([]string{"doctor"}, f), stdout, stderr)
		}
	}
}

// withHome appends --codex-home to argv when the user passed one to `ui`.
func withHome(argv []string, f commonFlags) []string {
	if f.codexHome != "" {
		argv = append(argv, "--codex-home", f.codexHome)
	}
	return argv
}

// runBuilt echoes the composed command and runs it through the normal CLI path.
func runBuilt(argv []string, stdout, stderr io.Writer) {
	fmt.Fprintf(stdout, "\n$ codex-sync %s\n\n", formatArgv(argv))
	Run(argv, stdout, stderr)
	fmt.Fprintln(stdout)
}

func uiExport(f commonFlags, stdout, stderr io.Writer) {
	c := exportChoices{codexHome: f.codexHome}
	if !runField(huh.NewSelect[string]().
		Title("What do you want to export?").
		Options(
			huh.NewOption("This project (current directory)", "current"),
			huh.NewOption("Everything (all sessions)", "all"),
			huh.NewOption("One session (pick from a list)", "session"),
			huh.NewOption("A specific project path", "project"),
		).
		Value(&c.mode), stderr) {
		return
	}

	switch c.mode {
	case "project":
		if !runField(huh.NewInput().Title("Project path").Value(&c.projectPath), stderr) {
			return
		}
	case "session":
		id, ok := pickSession(f, stdout, stderr)
		if !ok {
			return
		}
		c.sessionID = id
	}

	ok := runField(huh.NewConfirm().
		Title("Also record the project's git remote/commit? (--with-git)").
		Value(&c.withGit), stderr) &&
		runField(huh.NewInput().
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
	c := importChoices{codexHome: f.codexHome}
	if !runField(huh.NewInput().
		Title("Bundle file to import (.codexbundle or .age)").
		Value(&c.bundle).
		Validate(fileMustExist), stderr) {
		return
	}
	ok := runField(huh.NewConfirm().
		Title("If a local session conflicts, replace it (keeping a backup)? (--replace-with-backup)").
		Value(&c.replaceBackup), stderr) &&
		runField(huh.NewInput().
			Title("Project path for cwd-mismatch check (blank = none)").
			Value(&c.project), stderr)
	if !ok {
		return
	}

	// Optional cwd remapping, one or more OLD=NEW pairs.
	for {
		var add bool
		if !runField(huh.NewConfirm().
			Title("Remap a recorded cwd to a local path? (--map-cwd)").
			Value(&add), stderr) {
			return
		}
		if !add {
			break
		}
		var mapping string
		if !runField(huh.NewInput().
			Title("Mapping as OLD=NEW (e.g. /old/path=/new/local/path)").
			Value(&mapping), stderr) {
			return
		}
		if mapping != "" {
			c.mapCWD = append(c.mapCWD, mapping)
		}
	}

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
		}
	}

	// Safety first: always preview with --dry-run, then confirm the real run.
	fmt.Fprintln(stdout, "\nPreviewing the import (no files will be changed):")
	runBuilt(buildImportArgs(c, true), stdout, stderr)

	var proceed bool
	if !runField(huh.NewConfirm().
		Title("Apply this import for real now?").
		Affirmative("Import").
		Negative("Cancel").
		Value(&proceed), stderr) {
		return
	}
	if proceed {
		runBuilt(buildImportArgs(c, false), stdout, stderr)
	} else {
		fmt.Fprintln(stdout, "Cancelled; nothing was changed.")
	}
}

func uiInspect(f commonFlags, stdout, stderr io.Writer) {
	var path string
	if !runField(huh.NewInput().
		Title("Bundle file to inspect (.codexbundle or .age)").
		Value(&path).
		Validate(fileMustExist), stderr) {
		return
	}
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
func pickSession(f commonFlags, stdout, stderr io.Writer) (string, bool) {
	home, err := codexhome.Detect(f.codexHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Codex home: %v\n", err)
		return "", false
	}
	scan, err := sessions.Scan(home, sessions.ScanOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "error: scan failed: %v\n", err)
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
