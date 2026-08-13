// Package doctor performs read-only health checks on the local Codex setup so
// users can confirm cct can see their sessions before exporting or
// importing. It never writes anything and never touches Codex's SQLite state DB.
package doctor

import (
	"fmt"
	"os"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/repair"
	"github.com/ahmojo/codex-claude-transfer/internal/retention"
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

	if stale := countStaleMtimes([]string{home.SessionsDir}); stale > 0 {
		r.warn(fmt.Sprintf("%d session file(s) have a modification time ahead of their content (imported by an older cct); run `cct repair-times` so Codex stops re-parsing them on every open", stale))
	}

	r.checkOptionalTools()

	r.ok("SQLite will not be modified")
	return r
}

// RunClaude gathers diagnostics for a Claude Code home. It mirrors Run but for
// the ~/.claude/projects transcript store. The returned Report reuses the shared
// shape (its Home.Root/SessionsDir carry the Claude root and projects dir) so the
// same renderers apply. It never writes anything and never touches the cloud, the
// account, or ~/.claude.json.
func RunClaude(home claudehome.Home) Report {
	r := Report{Home: codexhome.Home{
		Root:        home.Root,
		SessionsDir: home.ProjectsDir,
		Source:      home.Source,
	}}

	if home.RootExists() {
		r.ok(fmt.Sprintf("Claude Code home found: %s", home.Root))
	} else {
		r.warn(fmt.Sprintf("Claude Code home not found: %s", home.Root))
	}

	if home.ProjectsDirExists() {
		r.ok(fmt.Sprintf("Projects folder found: %s", home.ProjectsDir))
	} else {
		r.warn(fmt.Sprintf("Projects folder not found: %s", home.ProjectsDir))
	}

	scan, _ := claudesessions.Scan(home, claudesessions.ScanOptions{})
	r.ok(fmt.Sprintf("%d transcript files detected", scan.Files))
	r.ok(fmt.Sprintf("%d valid sessions", scan.Valid))
	invalid := scan.Invalid
	if invalid > 0 {
		r.warn(fmt.Sprintf("%d transcript file(s) could not be parsed", invalid))
	}

	if missing := countMissingCwd(scan.Sessions); missing > 0 {
		r.warn(fmt.Sprintf("%d session(s) have cwd paths that do not exist on this device", missing))
	}

	if stale := countStaleMtimes([]string{home.ProjectsDir}); stale > 0 {
		r.warn(fmt.Sprintf("%d transcript file(s) have a modification time ahead of their content (imported by an older cct); run `cct repair-times --tool claude` to fix the open-lag", stale))
	}

	r.checkClaudeRetention(home, scan.Sessions)

	// Claude-relevant optional tools (git for handoff, age for encryption; zstd
	// is Codex-only — Claude Code does not compress transcripts).
	for _, t := range []struct {
		name      string
		available bool
		enables   string
	}{
		{"git", git.Available(), "export --with-git, import --clone"},
		{"age", crypt.Available(), "bundle encryption (--encrypt-to/--passphrase) and decrypting .age bundles"},
	} {
		if t.available {
			r.ok(fmt.Sprintf("Optional tool '%s' found (enables %s)", t.name, t.enables))
		} else {
			r.info(fmt.Sprintf("Optional tool '%s' not found — %s unavailable until installed", t.name, t.enables))
		}
	}

	r.ok("~/.claude.json and the Claude cloud are never modified")
	return r
}

// RetentionWarnWithin is how far ahead doctor looks for transcripts about to
// fall out of Claude Code's retention window. Two weeks is long enough to
// archive them without nagging about every session every day.
const RetentionWarnWithin = 14 * 24 * time.Hour

// checkClaudeRetention reports what Claude Code's own cleanup is about to
// delete. Claude removes transcripts older than `cleanupPeriodDays` silently,
// which is how people lose hundreds of sessions without noticing; cct cannot
// prevent that, so the least it can do is say which ones are next, while there
// is still time to export them.
func (r *Report) checkClaudeRetention(home claudehome.Home, list []sessions.Session) {
	policy, err := retention.LoadPolicy(home.Root)
	if err != nil {
		r.warn(fmt.Sprintf("cannot read Claude Code's cleanup setting: %v", err))
		return
	}
	rep := retention.Assess(list, policy, time.Now(), RetentionWarnWithin)

	source := fmt.Sprintf("its default of %d days", policy.Days)
	if policy.Configured {
		source = fmt.Sprintf("cleanupPeriodDays=%d from settings.json", policy.Days)
	}

	switch {
	case len(rep.Expired) > 0:
		r.warn(fmt.Sprintf("%d transcript(s) are already past Claude Code's cleanup window (%s) and can be deleted at any time; archive them with `cct export --all --tool claude -o claude-archive.codexbundle`",
			len(rep.Expired), source))
	case len(rep.Soon) > 0:
		r.warn(fmt.Sprintf("%d transcript(s) fall out of Claude Code's cleanup window (%s) within %d days, the first on %s; archive them with `cct export --all --tool claude -o claude-archive.codexbundle`",
			len(rep.Soon), source, int(RetentionWarnWithin.Hours()/24), rep.NextDeadline.Format("2006-01-02")))
	case !policy.Configured && len(list) > 0:
		r.info(fmt.Sprintf("Claude Code deletes transcripts older than %d days (its default; settings.json sets no cleanupPeriodDays) — nothing is due in the next %d days",
			policy.Days, int(RetentionWarnWithin.Hours()/24)))
	default:
		r.ok(fmt.Sprintf("No transcript is due for Claude Code's cleanup (%s)", source))
	}
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

// countStaleMtimes reports how many session files would be fixed by repair-times
// (mtime ahead of their content). It is a read-only dry run.
func countStaleMtimes(dirs []string) int {
	res, err := repair.RepairTimes(dirs, repair.Options{DryRun: true})
	if err != nil {
		return 0
	}
	return res.Fixed
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
