package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexreconcile"
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

// resolveBundle returns a plaintext bundle path for inspect/import. A plain
// bundle is returned unchanged with a no-op cleanup. An encrypted (.age) bundle
// is decrypted to a temporary file using an age identity (private-key) file and
// the returned cleanup removes it. Passphrase-based decryption is intentionally
// not supported here: the age CLI reads a passphrase only from an interactive
// terminal, which a loopback browser does not have; those bundles must be
// handled from the terminal (`cct import … --passphrase`). The string return is
// an error message for the UI (empty when ok).
func resolveBundle(path, identity string) (string, func(), string) {
	noop := func() {}
	if !strings.EqualFold(filepath.Ext(path), crypt.Extension) {
		return path, noop, ""
	}
	if identity == "" {
		return "", noop, "this bundle is encrypted; provide an age identity (key) file to decrypt it in the browser. Passphrase-encrypted bundles must be imported from the terminal."
	}
	if !crypt.Available() {
		return "", noop, "age is not installed or not on PATH; install age to read encrypted bundles"
	}
	tmpDir, err := os.MkdirTemp("", "cct-dec-")
	if err != nil {
		return "", noop, "cannot create temp dir: " + err.Error()
	}
	cleanup := func() { os.RemoveAll(tmpDir) }
	out := filepath.Join(tmpDir, "bundle.codexbundle")
	if err := crypt.Decrypt(path, out, crypt.DecryptOptions{IdentityFile: identity}); err != nil {
		cleanup()
		return "", noop, "decrypt failed: " + err.Error()
	}
	return out, cleanup, ""
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
	Mode            string   `json:"mode"` // "project" | "all" | "session"
	Tool            string   `json:"tool"` // "codex" | "claude"
	Project         string   `json:"project"`
	Session         string   `json:"session"` // thread id (or unique prefix) when mode == "session"
	Since           string   `json:"since"`   // date (YYYY-MM-DD) or duration (7d/48h/90m); applies to project/all
	Output          string   `json:"output"`
	IncludeArchived bool     `json:"include_archived"`
	WithGit         bool     `json:"with_git"`
	GitPush         bool     `json:"git_push"`
	StripImages     bool     `json:"strip_images"`
	Redact          bool     `json:"redact"`          // replace detected secrets with placeholders
	AllowSecrets    bool     `json:"allow_secrets"`   // bypass the pre-egress secret gate
	EncryptTo       []string `json:"encrypt_to"`      // age recipients; encrypts the bundle to <output>.age
	RecipientsFile  string   `json:"recipients_file"` // file of age recipients
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

	// A bundle covers exactly one of: one project folder, one session, or
	// everything. Resolve the project path only in "project" mode.
	var absProject string
	switch req.Mode {
	case "all", "session":
		// no project filter
	default: // "project"
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
	if req.Mode == "session" && strings.TrimSpace(req.Session) == "" {
		apiError(w, http.StatusBadRequest, "enter a session id (or a unique prefix) to export one session")
		return
	}

	var since time.Time
	if req.Since != "" {
		t, err := bundle.ParseSince(req.Since)
		if err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		since = t
	}

	// Encryption is opt-in and uses age recipients/identity files (non-interactive).
	// Passphrase mode is terminal-only and not offered here (age needs a TTY).
	encryptRequested := len(req.EncryptTo) > 0 || req.RecipientsFile != ""
	if encryptRequested && !crypt.Available() {
		apiError(w, http.StatusBadRequest, "age is not installed or not on PATH; install age to encrypt bundles")
		return
	}

	// --git-push is the only outbound action on export: it pushes YOUR code to
	// YOUR git remote (never sessions, never to any cct service). Capture what was
	// pushed so the UI can state it plainly.
	var pushedRemote, pushedBranch string
	if req.GitPush {
		if req.Mode != "project" {
			apiError(w, http.StatusBadRequest, "git push needs a single project, not 'everything' or a single session")
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
		remote, branch, err := git.Push(absProject)
		if err != nil {
			apiError(w, http.StatusBadGateway, "git push failed: "+err.Error())
			return
		}
		pushedRemote, pushedBranch = remote, branch
	}

	output := req.Output
	if output == "" {
		switch {
		case req.Mode == "session":
			output = "session.codexbundle"
		case req.Mode == "all" && kind == agent.Claude:
			output = "claude-sessions.codexbundle"
		case req.Mode == "all":
			output = "codex-sessions.codexbundle"
		default:
			output = filepath.Base(absProject) + ".codexbundle"
		}
		output, _ = filepath.Abs(output)
	}

	res, err := bundle.Export(s.home, bundle.ExportOptions{
		Tool:            kind,
		ClaudeHome:      s.claudeHome,
		ProjectPath:     absProject,
		SessionID:       req.Session,
		Since:           since,
		OutputPath:      output,
		IncludeArchived: req.IncludeArchived,
		WithGit:         req.WithGit,
		StripImages:     req.StripImages,
		Redact:          req.Redact,
	})
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Pre-egress secret gate (mirrors the CLI): refuse to leave a credential-laden
	// bundle on disk unless the user redacted or explicitly allowed it. The UI can
	// re-submit with redact or allow_secrets.
	if !req.Redact && !req.AllowSecrets {
		if sres, serr := bundle.ScanBundleSecrets(output); serr == nil && sres.Any() {
			os.Remove(output)
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "This bundle would contain a likely secret. Turn on \"Replace secrets with placeholders\", " +
					"or confirm \"Export anyway\".",
				"secrets_blocked":       true,
				"sessions_with_secrets": sres.SessionsWithSecrets,
				"secret_count":          sres.TotalFindings,
			})
			return
		}
	}

	bundlePath := res.BundlePath
	if encryptRequested {
		encPath := output + crypt.Extension
		err := crypt.Encrypt(output, encPath, crypt.EncryptOptions{
			Recipients:     req.EncryptTo,
			RecipientsFile: req.RecipientsFile,
		})
		// The plaintext bundle is intermediate; remove it whether or not
		// encryption succeeded so a clear bundle is never left behind.
		os.Remove(output)
		if err != nil {
			os.Remove(encPath)
			apiError(w, http.StatusUnprocessableEntity, "encrypt failed: "+err.Error())
			return
		}
		bundlePath = encPath
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bundle":           bundlePath,
		"included":         res.IncludedCount,
		"encrypted":        encryptRequested,
		"pushed_remote":    pushedRemote,
		"pushed_branch":    pushedBranch,
		"images_stripped":  res.ImagesStripped,
		"bytes_saved":      res.BytesSaved,
		"secrets_redacted": res.SecretsRedacted,
		"warnings":         res.Warnings,
	})
}

// ---- inspect ----

type pathReq struct {
	Path     string `json:"path"`
	Identity string `json:"identity"` // age key file, for an encrypted (.age) bundle
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
	path, cleanup, derr := resolveBundle(req.Path, req.Identity)
	if derr != "" {
		apiError(w, http.StatusBadRequest, derr)
		return
	}
	defer cleanup()
	res, err := bundle.Inspect(path)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)
	projects := make([]projectDTO, 0, len(summary.Dirs))
	for _, d := range summary.Dirs {
		projects = append(projects, projectDTO{Path: d.Path, Count: d.Count, ExistsLocal: d.ExistsLocal})
	}
	// here_dir is the folder cct app was launched in; the UI offers "map to current
	// folder" (--map-cwd-here) labelled with it, valid for a single-project bundle.
	hereDir, _ := os.Getwd()
	writeJSON(w, http.StatusOK, map[string]any{
		"format":   res.Manifest.FormatVersion,
		"created":  res.Manifest.CreatedAt,
		"device":   res.Manifest.CreatedByDevice,
		"sessions": len(res.Manifest.Sessions),
		"projects": projects,
		"tool":     res.Manifest.Tool,
		"here_dir": hereDir,
		"git":      res.Manifest.Git,
	})
}

// ---- import ----

type importReq struct {
	Path              string      `json:"path"`
	Identity          string      `json:"identity"` // age key file, for an encrypted (.age) bundle
	DryRun            bool        `json:"dry_run"`
	Merge             bool        `json:"merge"`
	Reconcile         bool        `json:"reconcile"`
	ReplaceWithBackup bool        `json:"replace_with_backup"`
	ImportAsCopy      bool        `json:"import_as_copy"`
	Project           string      `json:"project"`  // warn on cwd mismatch against this path
	Sessions          []string    `json:"sessions"` // only import these thread ids (unique prefixes)
	TranslateTo       string      `json:"translate_to"`
	CloneDir          string      `json:"clone_dir"` // after import, clone the recorded git remote here
	MapCWD            []cwdMapDTO `json:"map_cwd"`
	MapCWDHere        bool        `json:"map_cwd_here"` // map the bundle's single project to this app's launch dir
}

type cwdMapDTO struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type importReconcileDTO struct {
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
	FallbackCommands   []string `json:"fallback_commands,omitempty"`
}

// These seams keep the HTTP handler testable without starting a real Codex
// process. Production always uses the native codexreconcile implementation.
var (
	importThreadsChanged = codexreconcile.ThreadsChangedByImport
	reconcileCodexImport = codexreconcile.Reconcile
)

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
	if req.Reconcile && req.DryRun {
		apiError(w, http.StatusBadRequest, "reconcile cannot be combined with a dry run")
		return
	}
	if req.Reconcile && req.TranslateTo != "" {
		apiError(w, http.StatusBadRequest, "reconcile is only available for native Codex imports")
		return
	}

	path, cleanup, derr := resolveBundle(req.Path, req.Identity)
	if derr != "" {
		apiError(w, http.StatusBadRequest, derr)
		return
	}
	defer cleanup()

	// Cross-agent handoff: translate the bundle's sessions into the other agent's
	// format and write them into that agent's home, instead of importing natively.
	if req.TranslateTo != "" {
		s.handleTranslateImport(w, req, path)
		return
	}

	if req.MapCWDHere && len(req.MapCWD) > 0 {
		apiError(w, http.StatusBadRequest, "choose either map-to-current-folder or explicit cwd mappings, not both")
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
	// "Map to current folder" resolves to the directory cct app was launched in.
	var hereDir string
	if req.MapCWDHere {
		hereDir, err = os.Getwd()
		if err != nil {
			apiError(w, http.StatusUnprocessableEntity, "cannot determine the current directory: "+err.Error())
			return
		}
	}

	var absProject string
	if req.Project != "" {
		absProject, err = filepath.Abs(req.Project)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid project path: "+err.Error())
			return
		}
	}

	// The bundle records its own tool; that decides the destination home.
	insp, err := bundle.Inspect(path)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	kind := agent.Normalize(agent.Kind(insp.Manifest.Tool))
	if req.Reconcile && kind != agent.Codex {
		apiError(w, http.StatusBadRequest, "reconcile applies only to Codex bundles")
		return
	}
	home := s.importHomeFor(kind)

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:        path,
		DryRun:            req.DryRun,
		Merge:             req.Merge,
		MapCWD:            mappings,
		MapCWDHere:        req.MapCWDHere,
		HereDir:           hereDir,
		ReplaceWithBackup: req.ReplaceWithBackup,
		ImportAsCopy:      req.ImportAsCopy,
		ProjectPath:       absProject,
		SessionIDs:        req.Sessions,
	})
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	var reconcile *importReconcileDTO
	if req.Reconcile {
		reconcile = reconcileImportedSessions(r.Context(), home, res)
	}

	// Opt-in git clone, mirroring the CLI: only on a real import, only when the
	// bundle records a remote and the user gave a target directory.
	var cloned string
	var cloneErr string
	if req.CloneDir != "" && !res.DryRun {
		gi := res.Manifest.Git
		switch {
		case gi == nil || gi.RemoteURL == "":
			cloneErr = "the bundle records no git remote URL to clone"
		default:
			if err := git.Clone(gi.RemoteURL, req.CloneDir, gi.CommitSHA); err != nil {
				cloneErr = "clone failed: " + err.Error()
			} else {
				cloned = req.CloneDir
			}
		}
	}

	// Project summary + launch dir so the preview can offer "map to current folder"
	// (--map-cwd-here), which is valid only for a single-project bundle.
	summary := bundle.SummarizeCWDs(res.Manifest.Sessions, bundle.DirExists)
	projects := make([]projectDTO, 0, len(summary.Dirs))
	for _, d := range summary.Dirs {
		projects = append(projects, projectDTO{Path: d.Path, Count: d.Count, ExistsLocal: d.ExistsLocal})
	}
	here, _ := os.Getwd()
	response := map[string]any{
		"dry_run":            res.DryRun,
		"imported":           res.Imported,
		"updated":            res.Updated,
		"lines_added":        res.LinesAdded,
		"already_ahead":      res.AlreadyAhead,
		"skipped_identical":  res.SkippedIdentical,
		"conflicts":          res.Conflicts,
		"replaced":           res.Replaced,
		"imported_copies":    res.ImportedCopies,
		"remapped":           res.Mapped,
		"cwd_mismatch":       res.CWDMismatchCount,
		"sessions_in_bundle": len(res.Manifest.Sessions),
		"projects":           projects,
		"here_dir":           here,
		"tool":               res.Manifest.Tool,
		"cloned":             cloned,
		"clone_error":        cloneErr,
		"warnings":           res.Warnings,
	}
	if reconcile != nil {
		response["reconcile"] = reconcile
	}
	writeJSON(w, http.StatusOK, response)
}

// reconcileImportedSessions asks Codex itself to discover the exact rollout
// IDs changed by this import. Any failure is reported in-band: the file import
// has already succeeded and remains intact.
func reconcileImportedSessions(ctx context.Context, home codexhome.Home, result bundle.ImportResult) *importReconcileDTO {
	changed, err := importThreadsChanged(home, result)
	report := &importReconcileDTO{
		CodexHome:        home.Root,
		Requested:        len(changed.IDs) + changed.Unknown,
		UnknownThreadIDs: changed.Unknown,
	}
	var native codexreconcile.Result
	if err == nil {
		native, err = reconcileCodexImport(ctx, codexreconcile.Options{
			CodexHome: home.Root,
			ThreadIDs: changed.IDs,
		})
	}
	report.Verified = len(native.Verified)
	report.AlreadyDiscovered = len(native.AlreadyDiscovered)
	report.ReadForRepair = len(native.ReadForRepair)
	report.Version = native.Version
	report.VerificationMethod = string(native.VerificationMethod)
	report.Warnings = native.Warnings
	if err == nil && changed.Unknown > 0 {
		err = fmt.Errorf("could not determine an exact thread ID for %d affected rollout(s)", changed.Unknown)
	}
	if err != nil {
		report.Error = err.Error()
		report.FallbackCommands = importReconcileFallbacks(changed.IDs, home.Root)
	}
	return report
}

func importReconcileFallbacks(threadIDs []string, codexHome string) []string {
	const limit = 5
	ids := threadIDs
	if len(ids) > limit {
		ids = ids[:limit]
	}
	commands := make([]string, 0, len(ids))
	for _, id := range ids {
		commands = append(commands, fmt.Sprintf(`cct resume %s --run --codex-home "%s"`, id, codexHome))
	}
	if len(commands) == 0 {
		commands = append(commands, fmt.Sprintf(`cct resume <thread-id> --run --codex-home "%s"`, codexHome))
	}
	return commands
}

// handleTranslateImport performs a cross-agent handoff (import --to): it reads the
// bundle in its own agent's format, translates each session into the target
// agent's format, and writes the results into that agent's home.
func (s *Server) handleTranslateImport(w http.ResponseWriter, req importReq, path string) {
	target, err := agent.Parse(req.TranslateTo)
	if err != nil {
		apiError(w, http.StatusBadRequest, "translate target: "+err.Error())
		return
	}
	home := s.importHomeFor(target)
	res, err := bundle.TranslateImport(home, bundle.TranslateOptions{
		BundlePath: path,
		TargetTool: target,
		DryRun:     req.DryRun,
	})
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dry_run":           res.DryRun,
		"translated":        true,
		"source_tool":       res.SourceTool.Label(),
		"target_tool":       res.TargetTool.Label(),
		"written":           res.Translated,
		"skipped_identical": res.SkippedExisting,
		"skipped":           res.Skipped,
		"warnings":          res.Warnings,
	})
}
