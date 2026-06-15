package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ahmojo/Codex_Sync/internal/bundle"
	"github.com/ahmojo/Codex_Sync/internal/doctor"
	"github.com/ahmojo/Codex_Sync/internal/sessions"
)

// This file holds the --json renderers. They emit a single, stable JSON object
// to stdout so codex-sync can be scripted (jq, automation, other tools). Human
// status text and warnings still go to stderr; stdout stays pure JSON.

// writeJSON marshals v as indented JSON followed by a newline. A marshal error
// is written as an error code; it should not happen for these plain structs.
func writeJSON(w io.Writer, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "{\"error\":%q}\n", err.Error())
		return
	}
	w.Write(data)
	fmt.Fprintln(w)
}

type checkJSON struct {
	Status  string `json:"status"` // "ok" | "warn" | "info"
	Message string `json:"message"`
}

type doctorJSON struct {
	CodexHome   string      `json:"codex_home"`
	SessionsDir string      `json:"sessions_dir"`
	Checks      []checkJSON `json:"checks"`
}

func printDoctorJSON(w io.Writer, report doctor.Report) {
	out := doctorJSON{
		CodexHome:   report.Home.Root,
		SessionsDir: report.Home.SessionsDir,
		Checks:      make([]checkJSON, 0, len(report.Checks)),
	}
	for _, c := range report.Checks {
		out.Checks = append(out.Checks, checkJSON{Status: doctorStatusString(c.Status), Message: c.Message})
	}
	writeJSON(w, out)
}

func doctorStatusString(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "ok"
	case doctor.StatusWarn:
		return "warn"
	default:
		return "info"
	}
}

type sessionJSON struct {
	ThreadID      string `json:"thread_id,omitempty"`
	CWD           string `json:"cwd,omitempty"`
	Preview       string `json:"preview,omitempty"`
	Source        string `json:"source,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	Compressed    bool   `json:"compressed"`
	Archived      bool   `json:"archived"`
	Parsed        bool   `json:"parsed"`
	UpdatedAt     string `json:"updated_at"`
	RelPath       string `json:"rel_path"`
	Path          string `json:"path"`
}

type listJSON struct {
	Files      int           `json:"files"`
	Valid      int           `json:"valid"`
	Compressed int           `json:"compressed"`
	Sessions   []sessionJSON `json:"sessions"`
}

func printListJSON(w io.Writer, scan sessions.ScanResult) {
	out := listJSON{
		Files:      scan.Files,
		Valid:      scan.Valid,
		Compressed: scan.Compressed,
		Sessions:   make([]sessionJSON, 0, len(scan.Sessions)),
	}
	for _, s := range scan.Sessions {
		out.Sessions = append(out.Sessions, sessionJSON{
			ThreadID:      s.ThreadID,
			CWD:           s.CWD,
			Preview:       s.Preview,
			Source:        s.Source,
			ModelProvider: s.ModelProvider,
			Compressed:    s.Compressed,
			Archived:      s.Archived,
			Parsed:        s.Parsed,
			UpdatedAt:     s.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
			RelPath:       s.RelPath,
			Path:          s.Path,
		})
	}
	writeJSON(w, out)
}

type cwdDirJSON struct {
	Path        string `json:"path"`
	Count       int    `json:"count"`
	ExistsLocal bool   `json:"exists_local"`
}

type cwdSummaryJSON struct {
	Dirs         []cwdDirJSON `json:"dirs"`
	UnknownCWD   int          `json:"unknown_cwd"`
	MissingCount int          `json:"missing_count"`
}

func toCWDSummaryJSON(s bundle.CWDSummary) cwdSummaryJSON {
	out := cwdSummaryJSON{UnknownCWD: s.UnknownCWD, MissingCount: s.MissingCount, Dirs: make([]cwdDirJSON, 0, len(s.Dirs))}
	for _, d := range s.Dirs {
		out.Dirs = append(out.Dirs, cwdDirJSON{Path: d.Path, Count: d.Count, ExistsLocal: d.ExistsLocal})
	}
	return out
}

type inspectJSON struct {
	Bundle        string          `json:"bundle"`
	Manifest      bundle.Manifest `json:"manifest"`
	FilesInBundle int             `json:"files_in_bundle"`
	Checksummed   int             `json:"checksummed"`
	CWDSummary    cwdSummaryJSON  `json:"cwd_summary"`
}

func printInspectJSON(w io.Writer, path string, res bundle.InspectResult) {
	writeJSON(w, inspectJSON{
		Bundle:        path,
		Manifest:      res.Manifest,
		FilesInBundle: len(res.Entries),
		Checksummed:   len(res.Checksums),
		CWDSummary:    toCWDSummaryJSON(bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)),
	})
}

type exportJSON struct {
	Bundle            string   `json:"bundle"`
	Included          int      `json:"included"`
	TotalScanned      int      `json:"total_scanned"`
	CompressedSkipped int      `json:"compressed_skipped"`
	Warnings          []string `json:"warnings,omitempty"`
}

func printExportJSON(w io.Writer, res bundle.ExportResult) {
	writeJSON(w, exportJSON{
		Bundle:            res.BundlePath,
		Included:          res.IncludedCount,
		TotalScanned:      res.TotalScanned,
		CompressedSkipped: res.CompressedSkipped,
		Warnings:          res.Warnings,
	})
}

type importJSON struct {
	Bundle                  string   `json:"bundle"`
	SessionsInBundle        int      `json:"sessions_in_bundle"`
	Imported                int      `json:"imported"`
	SkippedIdentical        int      `json:"skipped_identical"`
	Conflicts               int      `json:"conflicts"`
	Replaced                int      `json:"replaced"`
	ImportedCopies          int      `json:"imported_copies"`
	SkippedDeselected       int      `json:"skipped_deselected"`
	SkippedOther            int      `json:"skipped_other"`
	Mapped                  int      `json:"mapped"`
	MappedCompressedSkipped int      `json:"mapped_compressed_skipped"`
	CWDMismatchCount        int      `json:"cwd_mismatch_count"`
	DryRun                  bool     `json:"dry_run"`
	Warnings                []string `json:"warnings,omitempty"`
}

func printImportJSON(w io.Writer, path string, res bundle.ImportResult) {
	writeJSON(w, importJSON{
		Bundle:                  path,
		SessionsInBundle:        len(res.Manifest.Sessions),
		Imported:                res.Imported,
		SkippedIdentical:        res.SkippedIdentical,
		Conflicts:               res.Conflicts,
		Replaced:                res.Replaced,
		ImportedCopies:          res.ImportedCopies,
		SkippedDeselected:       res.SkippedDeselected,
		SkippedOther:            res.SkippedOther,
		Mapped:                  res.Mapped,
		MappedCompressedSkipped: res.MappedCompressedSkipped,
		CWDMismatchCount:        res.CWDMismatchCount,
		DryRun:                  res.DryRun,
		Warnings:                res.Warnings,
	})
}
