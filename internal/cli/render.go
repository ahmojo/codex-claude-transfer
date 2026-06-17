package cli

import (
	"fmt"
	"io"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/doctor"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

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

func printList(w io.Writer, kind agent.Kind, scan sessions.ScanResult) {
	label := kind.Label()
	if len(scan.Sessions) == 0 {
		fmt.Fprintf(w, "No %s sessions found.\n", label)
		return
	}

	fmt.Fprintf(w, "Found %d %s session(s)\n\n", len(scan.Sessions), label)
	for i, s := range scan.Sessions {
		fmt.Fprintf(w, "%d. %s\n", i+1, title(s))
		if s.ThreadID != "" {
			fmt.Fprintf(w, "   Thread: %s\n", s.ThreadID)
		}
		if s.CWD != "" {
			fmt.Fprintf(w, "   CWD: %s\n", s.CWD)
		}
		fmt.Fprintf(w, "   Updated: %s\n", s.UpdatedAt().Format("2006-01-02 15:04"))
		if s.Source != "" {
			fmt.Fprintf(w, "   Source: %s\n", s.Source)
		}
		labels := flags(s)
		if labels != "" {
			fmt.Fprintf(w, "   %s\n", labels)
		}
		fmt.Fprintln(w)
	}

	// Compressed sessions may now be parsed (via zstd), so they can be Valid and
	// Compressed at once. Count the genuinely unparsed (non-compressed) files
	// directly from the sessions to avoid a misleading or negative number.
	unparsed := 0
	for _, s := range scan.Sessions {
		if !s.Parsed && !s.Compressed {
			unparsed++
		}
	}
	fmt.Fprintf(w, "Files: %d  Valid: %d  Compressed: %d  Unparsed/invalid: %d\n",
		scan.Files, scan.Valid, scan.Compressed, unparsed)
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
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Included sessions: %d\n", result.IncludedCount)
	fmt.Fprintf(w, "Bundle written:\n%s\n", result.BundlePath)
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
			fmt.Fprintf(w, " by %s", m.CreatedByDevice)
		}
		fmt.Fprintln(w)
	}
	if m.SourceProjectPath != "" {
		fmt.Fprintf(w, "Source project: %s\n", m.SourceProjectPath)
	}
	fmt.Fprintf(w, "Files in bundle: %d  (checksummed: %d)\n", len(res.Entries), len(res.Checksums))
	fmt.Fprintf(w, "Sessions: %d\n\n", len(m.Sessions))
	for i, s := range m.Sessions {
		name := s.Preview
		if name == "" {
			name = s.BundlePath
		}
		fmt.Fprintf(w, "%d. %s\n", i+1, name)
		if s.ThreadID != "" {
			fmt.Fprintf(w, "   Thread: %s\n", s.ThreadID)
		}
		if s.OriginalCWD != "" {
			fmt.Fprintf(w, "   CWD: %s\n", s.OriginalCWD)
		}
		if s.Source != "" {
			fmt.Fprintf(w, "   Source: %s\n", s.Source)
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
	fmt.Fprintln(w, "Project folders (recorded cwd):")
	for _, d := range summary.Dirs {
		mark := "[ok]     "
		if !d.ExistsLocal {
			mark = "[missing]"
		}
		fmt.Fprintf(w, "  %s %s  (%s)\n", mark, d.Path, plural(d.Count, "session"))
	}
	if summary.UnknownCWD > 0 {
		fmt.Fprintf(w, "  (%s have no recorded cwd — compressed or unknown)\n", plural(summary.UnknownCWD, "session"))
	}
	if summary.MissingCount > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Some of these folders do not exist on this machine, so those sessions\n")
		fmt.Fprintf(w, "will be hidden in %s's project view until you create the folder (then\n", kind.Label())
		fmt.Fprintf(w, "restart %s) or remap the cwd on import, e.g.:\n", kind.Label())
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
		fmt.Fprintf(w, "Skipped (not selected by --session): %d\n", res.SkippedDeselected)
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
			fmt.Fprintf(w, "warning: %s\n", warn)
		}
	}

	if gi := res.Manifest.Git; gi != nil && !gi.Empty() {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "This bundle records the project's git state:")
		if gi.RemoteURL != "" {
			fmt.Fprintf(w, "  remote: %s\n", gi.RemoteURL)
		}
		if gi.Branch != "" {
			fmt.Fprintf(w, "  branch: %s\n", gi.Branch)
		}
		if gi.CommitSHA != "" {
			fmt.Fprintf(w, "  commit: %s\n", gi.CommitSHA)
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
				fmt.Fprintf(w, "  git clone %s <dir> && (cd <dir> && git checkout %s)\n", gi.RemoteURL, gi.CommitSHA)
			} else {
				fmt.Fprintf(w, "  git clone %s <dir>\n", gi.RemoteURL)
			}
			fmt.Fprintln(w, "  or re-run import with: --clone <dir>")
		}
	}

	if summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists); summary.MissingCount > 0 {
		fmt.Fprintln(w)
		printCWDSummary(w, kind, summary, path, true)
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
			fmt.Fprintf(w, "warning: %s\n", wn)
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
