package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/doctor"
	"github.com/ahmojo/codex-claude-transfer/internal/lansync"
	"github.com/ahmojo/codex-claude-transfer/internal/search"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/stats"
)

// This file holds the --json renderers. They emit a single, stable JSON object
// to stdout so cct can be scripted (jq, automation, other tools). Human
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
	Bundle                 string   `json:"bundle"`
	Included               int      `json:"included"`
	TotalScanned           int      `json:"total_scanned"`
	CompressedSkipped      int      `json:"compressed_skipped"`
	MatchCompressedSkipped int      `json:"match_compressed_skipped,omitempty"`
	ImagesStripped         int      `json:"images_stripped"`
	BytesSaved             int64    `json:"bytes_saved"`
	Warnings               []string `json:"warnings,omitempty"`
}

func printExportJSON(w io.Writer, res bundle.ExportResult) {
	writeJSON(w, exportJSON{
		Bundle:                 res.BundlePath,
		Included:               res.IncludedCount,
		TotalScanned:           res.TotalScanned,
		CompressedSkipped:      res.CompressedSkipped,
		MatchCompressedSkipped: res.MatchCompressedSkipped,
		ImagesStripped:         res.ImagesStripped,
		BytesSaved:             res.BytesSaved,
		Warnings:               res.Warnings,
	})
}

type importJSON struct {
	Bundle                  string               `json:"bundle"`
	SessionsInBundle        int                  `json:"sessions_in_bundle"`
	Imported                int                  `json:"imported"`
	SkippedIdentical        int                  `json:"skipped_identical"`
	Conflicts               int                  `json:"conflicts"`
	Updated                 int                  `json:"updated"`
	LinesAdded              int                  `json:"lines_added"`
	AlreadyAhead            int                  `json:"already_ahead"`
	Replaced                int                  `json:"replaced"`
	ImportedCopies          int                  `json:"imported_copies"`
	SkippedDeselected       int                  `json:"skipped_deselected"`
	SkippedOther            int                  `json:"skipped_other"`
	Mapped                  int                  `json:"mapped"`
	MappedCompressedSkipped int                  `json:"mapped_compressed_skipped"`
	CWDMismatchCount        int                  `json:"cwd_mismatch_count"`
	DryRun                  bool                 `json:"dry_run"`
	Warnings                []string             `json:"warnings,omitempty"`
	Reconcile               *importReconcileJSON `json:"reconcile,omitempty"`
}

type importReconcileJSON struct {
	CodexHome          string   `json:"codex_home"`
	Requested          int      `json:"requested"`
	Verified           int      `json:"verified"`
	AlreadyDiscovered  int      `json:"already_discovered"`
	ReadForRepair      int      `json:"read_for_repair"`
	UnknownThreadIDs   int      `json:"unknown_thread_ids,omitempty"`
	Version            string   `json:"codex_version,omitempty"`
	VerificationMethod string   `json:"verification_method,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	Error              string   `json:"error,omitempty"`
}

func printImportJSON(w io.Writer, path string, res bundle.ImportResult, report postImportReconcile) {
	var reconcile *importReconcileJSON
	if report.Requested {
		reconcile = &importReconcileJSON{
			CodexHome:          report.CodexHome,
			Requested:          len(report.ThreadIDs) + report.UnknownThreadIDs,
			Verified:           len(report.Result.Verified),
			AlreadyDiscovered:  len(report.Result.AlreadyDiscovered),
			ReadForRepair:      len(report.Result.ReadForRepair),
			UnknownThreadIDs:   report.UnknownThreadIDs,
			Version:            report.Result.Version,
			VerificationMethod: string(report.Result.VerificationMethod),
			Warnings:           report.Result.Warnings,
		}
		if report.Err != nil {
			reconcile.Error = report.Err.Error()
		}
	}
	writeJSON(w, importJSON{
		Bundle:                  path,
		SessionsInBundle:        len(res.Manifest.Sessions),
		Imported:                res.Imported,
		SkippedIdentical:        res.SkippedIdentical,
		Conflicts:               res.Conflicts,
		Updated:                 res.Updated,
		LinesAdded:              res.LinesAdded,
		AlreadyAhead:            res.AlreadyAhead,
		Replaced:                res.Replaced,
		ImportedCopies:          res.ImportedCopies,
		SkippedDeselected:       res.SkippedDeselected,
		SkippedOther:            res.SkippedOther,
		Mapped:                  res.Mapped,
		MappedCompressedSkipped: res.MappedCompressedSkipped,
		CWDMismatchCount:        res.CWDMismatchCount,
		DryRun:                  res.DryRun,
		Warnings:                res.Warnings,
		Reconcile:               reconcile,
	})
}

type diffChangeJSON struct {
	ThreadID   string `json:"thread_id,omitempty"`
	Preview    string `json:"preview,omitempty"`
	BundlePath string `json:"bundle_path"`
	Change     string `json:"change"` // "new", "grow", "conflict"
	LinesAdded int    `json:"lines_added,omitempty"`
}

type diffJSON struct {
	Bundle           string           `json:"bundle"`
	SessionsInBundle int              `json:"sessions_in_bundle"`
	New              int              `json:"new"`
	Grow             int              `json:"grow"`
	Identical        int              `json:"identical"`
	Ahead            int              `json:"ahead"`
	Conflicts        int              `json:"conflicts"`
	Filtered         int              `json:"filtered,omitempty"`
	Changes          []diffChangeJSON `json:"changes"`
	Warnings         []string         `json:"warnings,omitempty"`
}

func printDiffJSON(w io.Writer, path string, res bundle.ImportResult) {
	byPath := map[string]bundle.ManifestSession{}
	for _, ms := range res.Manifest.Sessions {
		byPath[ms.BundlePath] = ms
	}
	changes := make([]diffChangeJSON, 0)
	for _, it := range res.Items {
		var change string
		switch it.Action {
		case bundle.ActionImport:
			change = "new"
		case bundle.ActionUpdate:
			change = "grow"
		case bundle.ActionConflict:
			change = "conflict"
		default:
			continue
		}
		ms := byPath[it.BundlePath]
		changes = append(changes, diffChangeJSON{
			ThreadID:   ms.ThreadID,
			Preview:    ms.Preview,
			BundlePath: it.BundlePath,
			Change:     change,
			LinesAdded: it.LinesAdded,
		})
	}
	writeJSON(w, diffJSON{
		Bundle:           path,
		SessionsInBundle: len(res.Manifest.Sessions),
		New:              res.Imported,
		Grow:             res.Updated,
		Identical:        res.SkippedIdentical,
		Ahead:            res.AlreadyAhead,
		Conflicts:        res.Conflicts,
		Filtered:         res.SkippedDeselected,
		Changes:          changes,
		Warnings:         res.Warnings,
	})
}

type syncJSON struct {
	PeerHost     string `json:"peer_host"`
	DryRun       bool   `json:"dry_run"`
	Sent         int    `json:"sent"`
	PreviewSend  int    `json:"preview_send,omitempty"`
	PreviewRecv  int    `json:"preview_receive,omitempty"`
	Received     int    `json:"received"`
	Updated      int    `json:"updated"`
	LinesAdded   int    `json:"lines_added"`
	AlreadyAhead int    `json:"already_ahead"`
	Conflicts    int    `json:"conflicts"`
	Remapped     int    `json:"remapped"`
}

type searchMatchJSON struct {
	ThreadID string `json:"thread_id"`
	CWD      string `json:"cwd,omitempty"`
	Preview  string `json:"preview,omitempty"`
	Updated  string `json:"updated_at"`
	Hits     int    `json:"hits"`
	Snippet  string `json:"snippet,omitempty"`
	Path     string `json:"path"`
}

func printSearchJSON(w io.Writer, matches []search.Match, compressedSkipped int) {
	out := make([]searchMatchJSON, 0, len(matches))
	for _, m := range matches {
		s := m.Session
		out = append(out, searchMatchJSON{
			ThreadID: s.ThreadID,
			CWD:      s.CWD,
			Preview:  s.Preview,
			Updated:  s.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
			Hits:     m.Hits,
			Snippet:  m.Snippet,
			Path:     s.Path,
		})
	}
	writeJSON(w, map[string]any{"matches": out, "count": len(out), "compressed_skipped": compressedSkipped})
}

func printScanJSON(w io.Writer, hits []secretHit, compressedSkipped int) {
	type findingJSON struct {
		Type   string `json:"type"`
		Masked string `json:"masked"`
	}
	type hitJSON struct {
		ThreadID string        `json:"thread_id"`
		CWD      string        `json:"cwd,omitempty"`
		Path     string        `json:"path"`
		Findings []findingJSON `json:"findings"`
	}
	out := make([]hitJSON, 0, len(hits))
	total := 0
	for _, h := range hits {
		fs := make([]findingJSON, 0, len(h.Findings))
		for _, f := range h.Findings {
			fs = append(fs, findingJSON{Type: f.Type, Masked: f.Masked})
		}
		total += len(fs)
		out = append(out, hitJSON{ThreadID: h.Session.ThreadID, CWD: h.Session.CWD, Path: h.Session.Path, Findings: fs})
	}
	writeJSON(w, map[string]any{"sessions": out, "session_count": len(out), "secret_count": total, "compressed_skipped": compressedSkipped})
}

func printStatsJSON(w io.Writer, kind agent.Kind, s stats.Stats) {
	type projJSON struct {
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	type dayJSON struct {
		Day   string `json:"day"`
		Count int    `json:"count"`
	}
	projects := make([]projJSON, 0, len(s.Projects))
	for _, p := range s.Projects {
		projects = append(projects, projJSON{Path: p.Path, Count: p.Count})
	}
	days := make([]dayJSON, 0, len(s.Days))
	for _, d := range s.Days {
		days = append(days, dayJSON{Day: d.Day, Count: d.Count})
	}
	first, last := "", ""
	if !s.First.IsZero() {
		first = s.First.Format("2006-01-02")
		last = s.Last.Format("2006-01-02")
	}
	writeJSON(w, map[string]any{
		"tool":        kind.String(),
		"total":       s.Total,
		"parsed":      s.Parsed,
		"compressed":  s.Compressed,
		"archived":    s.Archived,
		"no_cwd":      s.NoCWD,
		"total_bytes": s.TotalBytes,
		"first_day":   first,
		"last_day":    last,
		"projects":    projects,
		"days":        days,
	})
}

func printSyncJSON(w io.Writer, res lansync.Result) {
	r := res.Received
	writeJSON(w, syncJSON{
		PeerHost:     res.PeerHost,
		DryRun:       res.DryRun,
		Sent:         res.Sent,
		PreviewSend:  res.PreviewSend,
		PreviewRecv:  res.PreviewRecv,
		Received:     r.Imported,
		Updated:      r.Updated,
		LinesAdded:   r.LinesAdded,
		AlreadyAhead: r.AlreadyAhead,
		Conflicts:    r.Conflicts,
		Remapped:     r.Mapped,
	})
}
