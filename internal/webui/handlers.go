package webui

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/crypt"
	"github.com/ahmojo/codex-claude-transfer/internal/doctor"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// kindFromRequest reads the selected agent from a ?tool= query parameter (or a
// "tool" form value), defaulting to Codex. An unrecognized value falls back to
// Codex rather than erroring, so the UI can never wedge the server.
func (s *Server) kindFromRequest(r *http.Request) agent.Kind {
	if k, err := agent.Parse(r.URL.Query().Get("tool")); err == nil {
		return k
	}
	return agent.Codex
}

// importHomeFor returns the destination home (as a codexhome.Home carrier) for
// the given agent: the Codex home, or the Claude home rooted at ~/.claude with
// sessions under projects/.
func (s *Server) importHomeFor(kind agent.Kind) codexhome.Home {
	if kind == agent.Claude {
		return codexhome.Home{Root: s.claudeHome.Root, SessionsDir: s.claudeHome.ProjectsDir, Source: s.claudeHome.Source}
	}
	return s.home
}

// writeJSON encodes v as the response body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError reports a problem the UI can show to the user.
func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---- doctor ----

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	kind := s.kindFromRequest(r)
	var report doctor.Report
	if kind == agent.Claude {
		report = doctor.RunClaude(s.claudeHome)
	} else {
		report = doctor.Run(s.home)
	}
	type check struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	out := struct {
		CodexHome   string  `json:"codex_home"`
		SessionsDir string  `json:"sessions_dir"`
		Checks      []check `json:"checks"`
		Tools       struct {
			Git  bool `json:"git"`
			Age  bool `json:"age"`
			Zstd bool `json:"zstd"`
		} `json:"tools"`
	}{CodexHome: report.Home.Root, SessionsDir: report.Home.SessionsDir}
	for _, c := range report.Checks {
		out.Checks = append(out.Checks, check{Status: statusString(c.Status), Message: c.Message})
	}
	out.Tools.Git = git.Available()
	out.Tools.Age = crypt.Available()
	out.Tools.Zstd = zstdcli.Available()
	writeJSON(w, http.StatusOK, out)
}

func statusString(s doctor.Status) string {
	switch s {
	case doctor.StatusOK:
		return "ok"
	case doctor.StatusWarn:
		return "warn"
	default:
		return "info"
	}
}

// ---- sessions ----

type sessionDTO struct {
	ThreadID   string `json:"thread_id"`
	CWD        string `json:"cwd"`
	Preview    string `json:"preview"`
	Source     string `json:"source"`
	Compressed bool   `json:"compressed"`
	Archived   bool   `json:"archived"`
	UpdatedAt  string `json:"updated_at"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	kind := s.kindFromRequest(r)
	var scan sessions.ScanResult
	var err error
	if kind == agent.Claude {
		scan, err = claudesessions.Scan(s.claudeHome, claudesessions.ScanOptions{})
	} else {
		scan, err = sessions.Scan(s.home, sessions.ScanOptions{DecompressCompressed: true})
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	out := struct {
		Count    int          `json:"count"`
		Sessions []sessionDTO `json:"sessions"`
		Projects []projectDTO `json:"projects"`
	}{Count: len(scan.Sessions), Sessions: make([]sessionDTO, 0, len(scan.Sessions))}
	ms := make([]bundle.ManifestSession, 0, len(scan.Sessions))
	for _, sn := range scan.Sessions {
		out.Sessions = append(out.Sessions, sessionDTO{
			ThreadID:   sn.ThreadID,
			CWD:        sn.CWD,
			Preview:    sn.Preview,
			Source:     sn.Source,
			Compressed: sn.Compressed,
			Archived:   sn.Archived,
			UpdatedAt:  sn.UpdatedAt().Format("2006-01-02 15:04"),
		})
		ms = append(ms, bundle.ManifestSession{OriginalCWD: sn.CWD})
	}
	summary := bundle.SummarizeCWDs(ms, bundle.DirExists)
	for _, d := range summary.Dirs {
		out.Projects = append(out.Projects, projectDTO{Path: d.Path, Count: d.Count, ExistsLocal: d.ExistsLocal})
	}
	writeJSON(w, http.StatusOK, out)
}

type projectDTO struct {
	Path        string `json:"path"`
	Count       int    `json:"count"`
	ExistsLocal bool   `json:"exists_local"`
}

// ---- export ----

type exportReq struct {
	Mode            string `json:"mode"` // "project" | "all"
	Tool            string `json:"tool"` // "codex" | "claude"
	Project         string `json:"project"`
	Output          string `json:"output"`
	IncludeArchived bool   `json:"include_archived"`
	WithGit         bool   `json:"with_git"`
	GitPush         bool   `json:"git_push"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req exportReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	kind := agent.Codex
	if k, err := agent.Parse(req.Tool); err == nil {
		kind = k
	}

	var absProject string
	if req.Mode != "all" {
		p := req.Project
		if p == "" {
			apiError(w, http.StatusBadRequest, "choose a project folder, or export everything")
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid project path: "+err.Error())
			return
		}
		absProject = abs
	}

	if req.GitPush {
		if req.Mode == "all" {
			apiError(w, http.StatusBadRequest, "git push needs a single project, not 'export everything'")
			return
		}
		if !git.Available() {
			apiError(w, http.StatusBadRequest, "git is not installed; cannot push")
			return
		}
		if !git.IsRepo(absProject) {
			apiError(w, http.StatusBadRequest, absProject+" is not a git repository")
			return
		}
		if _, _, err := git.Push(absProject); err != nil {
			apiError(w, http.StatusBadGateway, "git push failed: "+err.Error())
			return
		}
	}

	output := req.Output
	if output == "" {
		if req.Mode == "all" {
			if kind == agent.Claude {
				output = "claude-sessions.codexbundle"
			} else {
				output = "codex-sessions.codexbundle"
			}
		} else {
			output = filepath.Base(absProject) + ".codexbundle"
		}
		output, _ = filepath.Abs(output)
	}

	res, err := bundle.Export(s.home, bundle.ExportOptions{
		Tool:            kind,
		ClaudeHome:      s.claudeHome,
		ProjectPath:     absProject,
		OutputPath:      output,
		IncludeArchived: req.IncludeArchived,
		WithGit:         req.WithGit,
	})
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bundle":   res.BundlePath,
		"included": res.IncludedCount,
		"warnings": res.Warnings,
	})
}

// ---- inspect ----

type pathReq struct {
	Path string `json:"path"`
}

func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req pathReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if req.Path == "" {
		apiError(w, http.StatusBadRequest, "choose a .codexbundle file")
		return
	}
	res, err := bundle.Inspect(req.Path)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)
	projects := make([]projectDTO, 0, len(summary.Dirs))
	for _, d := range summary.Dirs {
		projects = append(projects, projectDTO{Path: d.Path, Count: d.Count, ExistsLocal: d.ExistsLocal})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"format":   res.Manifest.FormatVersion,
		"created":  res.Manifest.CreatedAt,
		"device":   res.Manifest.CreatedByDevice,
		"sessions": len(res.Manifest.Sessions),
		"projects": projects,
		"git":      res.Manifest.Git,
	})
}

// ---- import ----

type importReq struct {
	Path              string      `json:"path"`
	DryRun            bool        `json:"dry_run"`
	ReplaceWithBackup bool        `json:"replace_with_backup"`
	ImportAsCopy      bool        `json:"import_as_copy"`
	MapCWD            []cwdMapDTO `json:"map_cwd"`
}

type cwdMapDTO struct {
	Old string `json:"old"`
	New string `json:"new"`
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if req.Path == "" {
		apiError(w, http.StatusBadRequest, "choose a .codexbundle file")
		return
	}
	if req.ReplaceWithBackup && req.ImportAsCopy {
		apiError(w, http.StatusBadRequest, "choose either replace-with-backup or import-as-copy, not both")
		return
	}
	var specs []string
	for _, m := range req.MapCWD {
		if m.Old != "" && m.New != "" {
			specs = append(specs, m.Old+"="+m.New)
		}
	}
	mappings, err := bundle.ParseCWDMappings(specs)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The bundle records its own tool; that decides the destination home.
	insp, err := bundle.Inspect(req.Path)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	home := s.importHomeFor(agent.Normalize(agent.Kind(insp.Manifest.Tool)))

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:        req.Path,
		DryRun:            req.DryRun,
		MapCWD:            mappings,
		ReplaceWithBackup: req.ReplaceWithBackup,
		ImportAsCopy:      req.ImportAsCopy,
	})
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dry_run":            res.DryRun,
		"imported":           res.Imported,
		"skipped_identical":  res.SkippedIdentical,
		"conflicts":          res.Conflicts,
		"replaced":           res.Replaced,
		"imported_copies":    res.ImportedCopies,
		"remapped":           res.Mapped,
		"sessions_in_bundle": len(res.Manifest.Sessions),
		"warnings":           res.Warnings,
	})
}
