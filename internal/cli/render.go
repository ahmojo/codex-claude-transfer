package cli

import (
	"fmt"
	"io"

	"github.com/ahmojo/Codex_Sync/internal/bundle"
	"github.com/ahmojo/Codex_Sync/internal/doctor"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
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

func printList(w io.Writer, scan sessions.ScanResult) {
	if len(scan.Sessions) == 0 {
		fmt.Fprintln(w, "No Codex sessions found.")
		return
	}

	fmt.Fprintf(w, "Found %d Codex session(s)\n\n", len(scan.Sessions))
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

	fmt.Fprintf(w, "Files: %d  Valid: %d  Compressed: %d  Unparsed/invalid: %d\n",
		scan.Files, scan.Valid, scan.Compressed, scan.Invalid-scan.Compressed)
}

func printExport(w io.Writer, project string, result bundle.ExportResult) {
	if project == "" {
		fmt.Fprintf(w, "Exporting all Codex sessions\n\n")
	} else {
		fmt.Fprintf(w, "Exporting Codex sessions for project:\n%s\n\n", project)
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
	fmt.Fprintf(w, "Bundle: %s\n", path)
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
}

func printImport(w io.Writer, path string, res bundle.ImportResult) {
	fmt.Fprintf(w, "Bundle: %s\n", path)
	fmt.Fprintf(w, "Sessions in bundle: %d\n", len(res.Manifest.Sessions))
	fmt.Fprintf(w, "New sessions: %d\n", res.Imported)
	fmt.Fprintf(w, "Already existing: %d\n", res.SkippedIdentical)
	fmt.Fprintf(w, "Conflicts: %d\n", res.Conflicts)
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

	fmt.Fprintln(w)
	if res.DryRun {
		fmt.Fprintln(w, "No files were changed because --dry-run was used.")
		return
	}
	if res.Imported > 0 {
		fmt.Fprintln(w, "Import complete.")
		fmt.Fprintln(w, "Next: restart the Codex App (or run Codex again) so it scans and")
		fmt.Fprintln(w, "reconciles the imported rollout files. codex-sync does not modify Codex's SQLite.")
	} else {
		fmt.Fprintln(w, "Nothing to import (all sessions already present or skipped).")
	}
}

func title(s sessions.Session) string {
	switch {
	case s.Preview != "":
		return s.Preview
	case s.Compressed:
		return "(compressed session — not parsed in v0.1)"
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
