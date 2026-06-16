// Package doctor performs read-only health checks on the local Codex setup so
// users can confirm cct can see their sessions before exporting or
// importing. It never writes anything and never touches Codex's SQLite state DB.
package doctor

import (
	"fmt"
	"os"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// Status is the result level of a single check.
type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusInfo
)

// Check is a single diagnostic line.
type Check struct {
	Status  Status
	Message string
}

// Report is the full set of diagnostics.
type Report struct {
	Home   codexhome.Home
	Checks []Check
}

// Run gathers diagnostics for the given Codex home. It always returns a Report;
// problems are represented as warning checks rather than errors.
func Run(home codexhome.Home) Report {
	r := Report{Home: home}

	if home.RootExists() {
		r.ok(fmt.Sprintf("Codex home found: %s", home.Root))
	} else {
		r.warn(fmt.Sprintf("Codex home not found: %s", home.Root))
	}

	if home.SessionsDirExists() {
		r.ok(fmt.Sprintf("Sessions folder found: %s", home.SessionsDir))
	} else {
		r.warn(fmt.Sprintf("Sessions folder not found: %s", home.SessionsDir))
	}

	if home.ArchivedSessionsDirExists() {
		r.info(fmt.Sprintf("Archived sessions folder found: %s", home.ArchivedSessionsDir))
	} else {
		r.info("No archived sessions folder (this is normal)")
	}

	scan, _ := sessions.Scan(home, sessions.ScanOptions{})
	r.ok(fmt.Sprintf("%d rollout files detected", scan.Files))
	r.ok(fmt.Sprintf("%d valid sessions", scan.Valid))
	if scan.Compressed > 0 {
		r.info(fmt.Sprintf("%d compressed (.jsonl.zst) rollout files detected (export/list recover their metadata when the 'zstd' tool is installed)", scan.Compressed))
	}
	invalidPlain := scan.Invalid - scan.Compressed
	if invalidPlain > 0 {
		r.warn(fmt.Sprintf("%d rollout file(s) could not be parsed", invalidPlain))
	}

	if missing := countMissingCwd(scan.Sessions); missing > 0 {
		r.warn(fmt.Sprintf("%d session(s) have cwd paths that do not exist on this device", missing))
	}

	r.checkOptionalTools()

	r.ok("SQLite will not be modified")
	return r
}

// checkOptionalTools reports which optional external tools are installed. None of
// them is required for the core commands; each only enables a specific opt-in
// feature, so a missing tool is reported as info (not a warning).
func (r *Report) checkOptionalTools() {
	type tool struct {
		name      string
		available bool
		enables   string
	}
	tools := []tool{
		{"git", git.Available(), "export --with-git, import --clone"},
		{"age", crypt.Available(), "bundle encryption (--encrypt-to/--passphrase) and decrypting .age bundles"},
		{"zstd", zstdcli.Available(), "reading metadata of compressed .jsonl.zst sessions"},
	}
	for _, t := range tools {
		if t.available {
			r.ok(fmt.Sprintf("Optional tool '%s' found (enables %s)", t.name, t.enables))
		} else {
			r.info(fmt.Sprintf("Optional tool '%s' not found — %s unavailable until installed", t.name, t.enables))
		}
	}
}

func countMissingCwd(list []sessions.Session) int {
	count := 0
	for _, s := range list {
		if s.CWD != "" && !dirExists(s.CWD) {
			count++
		}
	}
	return count
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (r *Report) ok(msg string)   { r.Checks = append(r.Checks, Check{StatusOK, msg}) }
func (r *Report) warn(msg string) { r.Checks = append(r.Checks, Check{StatusWarn, msg}) }
func (r *Report) info(msg string) { r.Checks = append(r.Checks, Check{StatusInfo, msg}) }
