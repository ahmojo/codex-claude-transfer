package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/doctor"
	"github.com/ahmojo/codex-claude-transfer/internal/lansync"
	"github.com/ahmojo/codex-claude-transfer/internal/repair"
	"github.com/ahmojo/codex-claude-transfer/internal/search"
	"github.com/ahmojo/codex-claude-transfer/internal/secrets"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// secretHit pairs a session with the secrets found inside it.
type secretHit struct {
	Session  sessions.Session
	Findings []secrets.Finding
}

// printScan renders the secret-scan results.
func printScan(w io.Writer, kind agent.Kind, hits []secretHit) {
	if len(hits) == 0 {
		fmt.Fprintf(w, "No likely secrets found in your %s sessions.\n", kind.Label())
		return
	}
	total := 0
	for _, h := range hits {
		total += len(h.Findings)
	}
	fmt.Fprintf(w, "Found %s across %s:\n\n", plural(total, "possible secret"), plural(len(hits), "session"))
	for _, h := range hits {
		fmt.Fprintf(w, "• %s\n", safeTerminal(title(h.Session)))
		if h.Session.ThreadID != "" {
			fmt.Fprintf(w, "  Thread: %s\n", h.Session.ThreadID)
		}
		for _, fnd := range h.Findings {
			fmt.Fprintf(w, "    %s: %s\n", fnd.Type, safeTerminal(fnd.Masked))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "These are heuristic matches — review before trusting them. To share/sync without")
	fmt.Fprintln(w, "the secrets, export with --redact (replaces detected values with placeholders).")
}

// printSync renders the outcome of a `cct sync` run (the apply summary). Dry-run
// previews are printed inside the sync layer, so this only reports a real sync.
func printSync(w io.Writer, kind agent.Kind, res lansync.Result) {
	if res.DryRun {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Sync complete with %s.\n", safeTerminal(res.PeerHost))
	fmt.Fprintf(w, "  Sent to peer:       %d session(s)\n", res.Sent)
	r := res.Received
	fmt.Fprintf(w, "  Received & added:   %d\n", r.Imported)
	if r.Updated > 0 {
		fmt.Fprintf(w, "  Updated (merged):   %d (+%s)\n", r.Updated, plural(r.LinesAdded, "line"))
	}
	if r.AlreadyAhead > 0 {
		fmt.Fprintf(w, "  Already up to date: %d\n", r.AlreadyAhead)
	}
	if r.Conflicts > 0 {
		fmt.Fprintf(w, "  Conflicts (kept local, not overwritten): %d\n", r.Conflicts)
	}
	if r.Mapped > 0 {
		fmt.Fprintf(w, "  Remapped to a local folder: %d\n", r.Mapped)
	}
	// Warn when a received session's project folder doesn't exist here — the same
	// "hidden session" gotcha import flags, with a ready-to-paste fix.
	if summary := bundle.SummarizeCWDs(r.Manifest.Sessions, bundle.DirExists); summary.MissingCount > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Heads up: %s landed under a project folder that doesn't exist on this\n", plural(summary.MissingCount, "received session"))
		fmt.Fprintf(w, "machine, so they may be hidden in %s's view. Re-sync with --map-cwd-here to\n", kind.Label())
		fmt.Fprintln(w, "place them under the folder you're in, or --map-cwd \"<old>=<local path>\".")
	}
	if r.Imported > 0 || r.Updated > 0 {
		fmt.Fprintf(w, "\nRestart %s so it picks up the synced sessions.\n", kind.Label())
	}
}

// printSearch renders full-text search results, most relevant first.
func printSearch(w io.Writer, kind agent.Kind, query string, matches []search.Match) {
	if len(matches) == 0 {
		fmt.Fprintf(w, "No %s sessions matched %q.\n", kind.Label(), query)
		return
	}
	fmt.Fprintf(w, "%s matched %q:\n\n", plural(len(matches), kind.Label()+" session"), query)
	for i, m := range matches {
		s := m.Session
		fmt.Fprintf(w, "%d. %s\n", i+1, safeTerminal(title(s)))
		if s.ThreadID != "" {
			fmt.Fprintf(w, "   Thread: %s\n", s.ThreadID)
		}
		if s.CWD != "" {
			fmt.Fprintf(w, "   Project: %s\n", safeTerminal(s.CWD))
		}
		fmt.Fprintf(w, "   Updated: %s   Hits: %d\n", s.UpdatedAt().Format("2006-01-02 15:04"), m.Hits)
		if m.Snippet != "" {
			fmt.Fprintf(w, "   … %s\n", safeTerminal(m.Snippet))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Tip: export one with  cct export --session <thread-id>\n")
}

// countCompressedSessions returns how many sessions in the list are compressed
// (.jsonl.zst). Full-text search reads plain text only, so these are skipped —
// callers surface the count so the gap is visible, not silent.
func countCompressedSessions(list []sessions.Session) int {
	n := 0
	for _, s := range list {
		if s.Compressed {
			n++
		}
	}
	return n
}

// printCompressedSkipNote prints a visible line when full-text search skipped
// compressed sessions, so users know their result set was not exhaustive.
func printCompressedSkipNote(w io.Writer, n int) {
	if n > 0 {
		fmt.Fprintf(w, "\n%s skipped because searchable text was unavailable (compressed .jsonl.zst; needs the 'zstd' tool to read).\n", plural(n, "compressed session"))
	}
}

// printRepair renders the outcome of a `repair-times` run.
func printRepair(w io.Writer, kind agent.Kind, res repair.Result) {
	if res.Scanned == 0 {
		fmt.Fprintf(w, "No %s session files found to check.\n", kind.Label())
		return
	}
	fmt.Fprintf(w, "Checked %s.\n", plural(res.Scanned, kind.Label()+" session file"))
	verb := "Fixed"
	if res.DryRun {
		verb = "Would fix"
	}
	fmt.Fprintf(w, "%s %s whose modification time ran ahead of their content (imported with the wrong mtime):\n",
		verb, plural(res.Fixed, "session"))
	const maxShown = 20
	for i, fr := range res.Files {
		if i >= maxShown {
			fmt.Fprintf(w, "  ... and %d more\n", len(res.Files)-maxShown)
			break
		}
		fmt.Fprintf(w, "  %s\n      %s  ->  %s\n", safeTerminal(filepath.Base(fr.Path)),
			fr.OldMtime.Format("2006-01-02 15:04"), fr.ContentTime.Format("2006-01-02 15:04"))
	}
	if res.Fixed == 0 && !res.DryRun {
		fmt.Fprintln(w, "Everything already looks correct — nothing to change.")
	}
	for _, warn := range res.Warnings {
		fmt.Fprintf(w, "warning: %s\n", safeTerminal(warn))
	}
	if res.DryRun {
		fmt.Fprintln(w, "\nNo files were changed because --dry-run was used.")
	} else if res.Fixed > 0 {
		fmt.Fprintf(w, "\nRestart %s so it re-reads them once; after that they'll open without the delay.\n", kind.Label())
	}
}

func printReport(w io.Writer, report doctor.Report) {
	for _, c := range report.Checks {
		fmt.Fprintf(w, "%s %s\n", symbol(c.Status), c.Message)
	}
}

func symbol(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "[ok]"
	case doctor.StatusWarn:
		return "[warn]"
	default:
		return "[info]"
	}
}

func printList(w io.Writer, kind agent.Kind, scan sessions.ScanResult, flat bool) {
	label := kind.Label()
	if len(scan.Sessions) == 0 {
		fmt.Fprintf(w, "No %s sessions found.\n", label)
		return
	}

	fmt.Fprintf(w, "Found %d %s session(s)\n\n", len(scan.Sessions), label)

	if flat {
		// Flat list, newest first.
		ss := make([]sessions.Session, len(scan.Sessions))
		copy(ss, scan.Sessions)
		sort.Slice(ss, func(i, j int) bool {
			return ss[i].UpdatedAt().After(ss[j].UpdatedAt())
		})
		for i, s := range ss {
			printSession(w, i+1, s)
		}
	} else {
		printListGrouped(w, scan.Sessions)
	}

	unparsed := 0
	for _, s := range scan.Sessions {
		if !s.Parsed && !s.Compressed {
			unparsed++
		}
	}
	fmt.Fprintf(w, "Files: %d  Valid: %d  Compressed: %d  Unparsed/invalid: %d\n",
		scan.Files, scan.Valid, scan.Compressed, unparsed)
}

func printListGrouped(w io.Writer, ss []sessions.Session) {
	type group struct {
		cwd      string
		sessions []sessions.Session
		newest   time.Time
	}

	keyOf := func(cwd string) string {
		return strings.ToLower(strings.TrimRight(cwd, `/\`))
	}

	order := []string{}
	groups := map[string]*group{}
	for _, s := range ss {
		k := keyOf(s.CWD)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
			groups[k] = &group{cwd: s.CWD}
		}
		g := groups[k]
		g.sessions = append(g.sessions, s)
		if t := s.UpdatedAt(); t.After(g.newest) {
			g.newest = t
		}
	}

	// Sort groups by most-recent session descending.
	sort.Slice(order, func(i, j int) bool {
		return groups[order[i]].newest.After(groups[order[j]].newest)
	})

	n := 1
	for _, k := range order {
		g := groups[k]
		cwd := g.cwd
		if cwd == "" {
			cwd = "(no project)"
		}
		fmt.Fprintf(w, "── %s  (%s)\n\n", cwd, plural(len(g.sessions), "session"))

		// Sort sessions within group newest first.
		sort.Slice(g.sessions, func(i, j int) bool {
			return g.sessions[i].UpdatedAt().After(g.sessions[j].UpdatedAt())
		})
		for _, s := range g.sessions {
			printSession(w, n, s)
			n++
		}
	}
}

func printSession(w io.Writer, n int, s sessions.Session) {
	fmt.Fprintf(w, "%d. %s\n", n, safeTerminal(title(s)))
	if s.ThreadID != "" {
		fmt.Fprintf(w, "   Thread: %s\n", s.ThreadID)
	}
	fmt.Fprintf(w, "   Updated: %s\n", s.UpdatedAt().Format("2006-01-02 15:04"))
	if s.Source != "" {
		fmt.Fprintf(w, "   Source: %s\n", safeTerminal(s.Source))
	}
	labels := flags(s)
	if labels != "" {
		fmt.Fprintf(w, "   %s\n", labels)
	}
	fmt.Fprintln(w)
}

// safeTerminal makes an untrusted string safe to print to a terminal. A malicious
// bundle's metadata (preview, cwd, git remote, warnings) could otherwise contain
// ANSI/OSC escape sequences that spoof the screen or write the clipboard (OSC 52)
// during the very inspect/preview step the user relies on to judge a bundle. It
// drops all C0/C1 control characters (including ESC, CR, and LF, so a value cannot
// forge extra lines) and turns tabs into spaces. See SEC-10 in docs/security/audit.md.
func safeTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		default:
			return r
		}
	}, s)
}

func printExport(w io.Writer, kind agent.Kind, project, session string, result bundle.ExportResult) {
	label := kind.Label()
	switch {
	case session != "":
		fmt.Fprintf(w, "Exporting one %s session (thread id %s)\n\n", label, session)
	case project == "":
		fmt.Fprintf(w, "Exporting all %s sessions\n\n", label)
	default:
		fmt.Fprintf(w, "Exporting %s sessions for project:\n%s\n\n", label, project)
	}
	for _, warn := range result.Warnings {
		fmt.Fprintf(w, "warning: %s\n", safeTerminal(warn))
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Included sessions: %d\n", result.IncludedCount)
	if result.MatchCompressedSkipped > 0 {
		fmt.Fprintf(w, "%s skipped by --match because searchable text was unavailable (compressed .jsonl.zst).\n",
			plural(result.MatchCompressedSkipped, "compressed session"))
	}
	if result.ImagesStripped > 0 {
		fmt.Fprintf(w, "Images stripped: %d (saved ~%s)\n", result.ImagesStripped, humanBytes(result.BytesSaved))
		fmt.Fprintln(w, "Note: a stripped bundle isn't merge-friendly — import --merge sees it as diverged")
		fmt.Fprintln(w, "  from an unstripped copy. Import it fresh; don't use it for incremental sync.")
	}
	if result.SecretsRedacted > 0 {
		fmt.Fprintf(w, "Secrets redacted: %d (replaced with placeholders)\n", result.SecretsRedacted)
		fmt.Fprintln(w, "Note: redacted content differs from the original, so this bundle isn't merge-friendly.")
	}
	fmt.Fprintf(w, "Bundle written:\n%s\n", result.BundlePath)
}

// humanBytes renders a byte count in the largest sensible unit for a one-line
// summary (e.g. "1.4 MB"). It is approximate and for human output only.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func printInspect(w io.Writer, path string, res bundle.InspectResult) {
	m := res.Manifest
	kind := agent.Normalize(agent.Kind(m.Tool))
	fmt.Fprintf(w, "Bundle: %s\n", path)
	fmt.Fprintf(w, "Tool: %s\n", kind.Label())
	fmt.Fprintf(w, "Format: %s\n", m.FormatVersion)
	if m.CreatedAt != "" {
		fmt.Fprintf(w, "Created: %s", m.CreatedAt)
		if m.CreatedByDevice != "" {
			fmt.Fprintf(w, " by %s", safeTerminal(m.CreatedByDevice))
		}
		fmt.Fprintln(w)
	}
	if m.SourceProjectPath != "" {
		fmt.Fprintf(w, "Source project: %s\n", safeTerminal(m.SourceProjectPath))
	}
	fmt.Fprintf(w, "Files in bundle: %d  (checksummed: %d)\n", len(res.Entries), len(res.Checksums))
	fmt.Fprintf(w, "Sessions: %d\n\n", len(m.Sessions))
	for i, s := range m.Sessions {
		name := s.Preview
		if name == "" {
			name = s.BundlePath
		}
		fmt.Fprintf(w, "%d. %s\n", i+1, safeTerminal(name))
		if s.ThreadID != "" {
			fmt.Fprintf(w, "   Thread: %s\n", s.ThreadID)
		}
		if s.OriginalCWD != "" {
			fmt.Fprintf(w, "   CWD: %s\n", safeTerminal(s.OriginalCWD))
		}
		if s.Source != "" {
			fmt.Fprintf(w, "   Source: %s\n", safeTerminal(s.Source))
		}
		if s.Compressed {
			fmt.Fprintf(w, "   [compressed]\n")
		}
		fmt.Fprintln(w)
	}
	printCWDSummary(w, kind, bundle.SummarizeCWDs(m.Sessions, bundle.DirExists), path, false)
}

// printCWDSummary renders the distinct recorded working directories in a bundle
// and flags the ones that do not exist on this machine — the #1 reason imported
// sessions appear "missing" in Codex (they are hidden from a project's sidebar
// unless a folder at that exact cwd exists). When onlyIfMissing is true the
// whole block is suppressed unless at least one folder is missing, so it does
// not add noise to a clean import.
func printCWDSummary(w io.Writer, kind agent.Kind, summary bundle.CWDSummary, bundlePath string, onlyIfMissing bool) {
	if onlyIfMissing && summary.MissingCount == 0 {
		return
	}
	if len(summary.Dirs) == 0 && summary.UnknownCWD == 0 {
		return
	}
	// Claude Code groups its sidebar by the project folder (projects/<encoded-cwd>/),
	// so for Claude this list IS the set of sidebar groups the sessions land in.
	if kind == agent.Claude {
		fmt.Fprintln(w, "Project groups (recorded cwd):")
	} else {
		fmt.Fprintln(w, "Project folders (recorded cwd):")
	}
	for _, d := range summary.Dirs {
		mark := "[ok]     "
		if !d.ExistsLocal {
			mark = "[missing]"
		}
		fmt.Fprintf(w, "  %s %s  (%s)\n", mark, safeTerminal(d.Path), plural(d.Count, "session"))
	}
	if summary.UnknownCWD > 0 {
		fmt.Fprintf(w, "  (%s have no recorded cwd — compressed or unknown)\n", plural(summary.UnknownCWD, "session"))
	}
	if summary.MissingCount > 0 {
		fmt.Fprintln(w)
		if kind == agent.Claude {
			// For Claude the symptom is grouping, not invisibility: the sessions
			// keep their original group folder, so they show under the source
			// machine's path rather than the project's location here.
			fmt.Fprintf(w, "Some of these paths do not exist on this machine, so those sessions will\n")
			fmt.Fprintf(w, "stay grouped under their original project, not its location here. Remap the\n")
			fmt.Fprintf(w, "cwd on import to move them into the right group, e.g.:\n")
		} else {
			fmt.Fprintf(w, "Some of these folders do not exist on this machine, so those sessions\n")
			fmt.Fprintf(w, "will be hidden in %s's project view until you create the folder (then\n", kind.Label())
			fmt.Fprintf(w, "restart %s) or remap the cwd on import, e.g.:\n", kind.Label())
		}
		fmt.Fprintf(w, "  cct import %s --map-cwd \"<old-cwd>=<new-local-path>\"\n", bundlePath)
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func printImport(w io.Writer, kind agent.Kind, path string, res bundle.ImportResult) {
	fmt.Fprintf(w, "Bundle: %s\n", path)
	fmt.Fprintf(w, "Sessions in bundle: %d\n", len(res.Manifest.Sessions))
	fmt.Fprintf(w, "New sessions: %d\n", res.Imported)
	fmt.Fprintf(w, "Already existing: %d\n", res.SkippedIdentical)
	fmt.Fprintf(w, "Conflicts: %d\n", res.Conflicts)
	if res.Updated > 0 {
		if res.LinesAdded > 0 {
			fmt.Fprintf(w, "Updated (new messages appended): %d (+%s)\n", res.Updated, plural(res.LinesAdded, "line"))
		} else {
			fmt.Fprintf(w, "Updated (new messages appended): %d\n", res.Updated)
		}
	}
	if res.AlreadyAhead > 0 {
		fmt.Fprintf(w, "Already up to date (local is ahead): %d\n", res.AlreadyAhead)
	}
	if res.Replaced > 0 {
		fmt.Fprintf(w, "Replaced (backup kept): %d\n", res.Replaced)
	}
	if res.ImportedCopies > 0 {
		fmt.Fprintf(w, "Imported as new copies: %d\n", res.ImportedCopies)
	}
	if res.SkippedDeselected > 0 {
		fmt.Fprintf(w, "Skipped (excluded by filter): %d\n", res.SkippedDeselected)
	}
	if res.SkippedOther > 0 {
		fmt.Fprintf(w, "Other skipped (archived/non-session): %d\n", res.SkippedOther)
	}
	if res.Mapped > 0 {
		fmt.Fprintf(w, "Remapped cwd: %d\n", res.Mapped)
	}
	if res.MappedCompressedSkipped > 0 {
		fmt.Fprintf(w, "Compressed (cwd not remapped): %d\n", res.MappedCompressedSkipped)
	}
	if res.ProjectProvided {
		fmt.Fprintf(w, "CWD mismatch warnings: %d\n", res.CWDMismatchCount)
	}

	if len(res.Warnings) > 0 {
		fmt.Fprintln(w)
		for _, warn := range res.Warnings {
			fmt.Fprintf(w, "warning: %s\n", safeTerminal(warn))
		}
	}

	if gi := res.Manifest.Git; gi != nil && !gi.Empty() {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "This bundle records the project's git state:")
		if gi.RemoteURL != "" {
			fmt.Fprintf(w, "  remote: %s\n", safeTerminal(gi.RemoteURL))
		}
		if gi.Branch != "" {
			fmt.Fprintf(w, "  branch: %s\n", safeTerminal(gi.Branch))
		}
		if gi.CommitSHA != "" {
			fmt.Fprintf(w, "  commit: %s\n", safeTerminal(gi.CommitSHA))
		}
		if gi.Dirty {
			fmt.Fprintln(w, "  note: the working tree was dirty at export; the commit is not the exact state.")
		}
		if gi.Unpushed {
			fmt.Fprintln(w, "  note: the commit was not pushed to any remote, so it may not be fetchable.")
		}
		if gi.RemoteURL != "" {
			fmt.Fprintln(w, "To get the code on this machine:")
			if gi.CommitSHA != "" {
				fmt.Fprintf(w, "  git clone %s <dir> && (cd <dir> && git checkout %s)\n", safeTerminal(gi.RemoteURL), safeTerminal(gi.CommitSHA))
			} else {
				fmt.Fprintf(w, "  git clone %s <dir>\n", safeTerminal(gi.RemoteURL))
			}
			fmt.Fprintln(w, "  or re-run import with: --clone <dir>")
		}
	}

	// For Claude, always show the project groups the sessions land in (it mirrors
	// the grouped sidebar). For Codex, only surface the folder list when something
	// is missing, so a clean import stays quiet.
	summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)
	alwaysShow := kind == agent.Claude
	if alwaysShow || summary.MissingCount > 0 {
		fmt.Fprintln(w)
		printCWDSummary(w, kind, summary, path, !alwaysShow)
	}

	fmt.Fprintln(w)
	if res.DryRun {
		fmt.Fprintln(w, "No files were changed because --dry-run was used.")
		return
	}
	if res.Imported > 0 || res.Replaced > 0 || res.ImportedCopies > 0 || res.Updated > 0 {
		fmt.Fprintln(w, "Import complete.")
		if kind == agent.Claude {
			fmt.Fprintln(w, "Next: run Claude Code again so it discovers the imported transcripts.")
			fmt.Fprintln(w, "cct does not modify ~/.claude.json or the Claude cloud.")
		} else {
			fmt.Fprintln(w, "Next: restart the Codex App (or run Codex again) so it scans and")
			fmt.Fprintln(w, "reconciles the imported rollout files. cct does not modify Codex's SQLite.")
		}
	} else {
		fmt.Fprintln(w, "Nothing to import (all sessions already present or skipped).")
	}
}

func printTranslate(w io.Writer, path string, res bundle.TranslateResult) {
	fmt.Fprintf(w, "Cross-agent handoff: %s → %s\n", res.SourceTool.Label(), res.TargetTool.Label())
	fmt.Fprintf(w, "Bundle: %s\n\n", path)
	fmt.Fprintf(w, "Translated sessions: %d\n", res.Translated)
	if res.SkippedExisting > 0 {
		fmt.Fprintf(w, "Already translated (skipped): %d\n", res.SkippedExisting)
	}
	if res.Skipped > 0 {
		fmt.Fprintf(w, "Could not translate (skipped): %d\n", res.Skipped)
	}
	for _, it := range res.Items {
		if it.Action == "translate" {
			fmt.Fprintf(w, "  + %s  (%s)\n", it.SessionID, plural(it.Turns, "turn"))
		}
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintln(w)
		for _, wn := range res.Warnings {
			fmt.Fprintf(w, "warning: %s\n", safeTerminal(wn))
		}
	}

	fmt.Fprintln(w)
	if res.DryRun {
		fmt.Fprintln(w, "No files were written because --dry-run was used.")
		return
	}
	if res.Translated > 0 {
		fmt.Fprintln(w, "Handoff complete. These are best-effort translated sessions: each opens")
		fmt.Fprintf(w, "with a handoff note and the prior conversation as text. Run %s again to\n", res.TargetTool.Label())
		fmt.Fprintln(w, "see them, then continue from there.")
	} else {
		fmt.Fprintln(w, "Nothing was translated (all sessions already present or unconvertible).")
	}
}

func title(s sessions.Session) string {
	switch {
	case s.Preview != "":
		return s.Preview
	case s.Compressed:
		return "(compressed session — metadata not recovered; install zstd)"
	case !s.Parsed:
		return "(unparsed session)"
	default:
		return s.FileName
	}
}

func flags(s sessions.Session) string {
	var parts []string
	if s.Compressed {
		parts = append(parts, "compressed")
	}
	if s.Archived {
		parts = append(parts, "archived")
	}
	if len(parts) == 0 {
		return ""
	}
	out := "["
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out + "]"
}
