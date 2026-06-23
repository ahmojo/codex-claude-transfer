package webui

import (
	"net/http"
	"os"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/cctpaths"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/meta"
	"github.com/ahmojo/codex-claude-transfer/internal/search"
	"github.com/ahmojo/codex-claude-transfer/internal/secrets"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/stats"
)

// scanSessions scans the selected agent's home, matching the read endpoints.
func (s *Server) scanSessions(kind agent.Kind) (sessions.ScanResult, error) {
	if kind == agent.Claude {
		return claudesessions.Scan(s.claudeHome, claudesessions.ScanOptions{})
	}
	return sessions.Scan(s.home, sessions.ScanOptions{DecompressCompressed: true})
}

// metaStore loads cct's tag/name sidecar (never inside an agent home).
func metaStore() (*meta.Store, string) {
	dir, _ := cctpaths.ConfigDir("")
	st, _ := meta.Load(dir)
	return st, dir
}

// ---- stats ----

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	kind := s.kindFromRequest(r)
	scan, err := s.scanSessions(kind)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	st := stats.Compute(scan.Sessions)
	type proj struct {
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	type day struct {
		Day   string `json:"day"`
		Count int    `json:"count"`
	}
	projects := make([]proj, 0, len(st.Projects))
	for _, p := range st.Projects {
		projects = append(projects, proj{Path: p.Path, Count: p.Count})
	}
	days := make([]day, 0, len(st.Days))
	for _, d := range st.Days {
		days = append(days, day{Day: d.Day, Count: d.Count})
	}
	first, last := "", ""
	if !st.First.IsZero() {
		first, last = st.First.Format("2006-01-02"), st.Last.Format("2006-01-02")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":       st.Total,
		"compressed":  st.Compressed,
		"archived":    st.Archived,
		"no_cwd":      st.NoCWD,
		"total_bytes": st.TotalBytes,
		"first_day":   first,
		"last_day":    last,
		"projects":    projects,
		"days":        days,
	})
}

// ---- search ----

type searchReq struct {
	Query         string `json:"query"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"case_sensitive"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req searchReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		apiError(w, http.StatusBadRequest, "enter something to search for")
		return
	}
	kind := s.kindFromRequest(r)
	scan, err := s.scanSessions(kind)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	matches, err := search.Search(scan.Sessions, search.Query{Text: req.Query, Regex: req.Regex, CaseSensitive: req.CaseSensitive})
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	store, _ := metaStore()
	type matchDTO struct {
		ThreadID string   `json:"thread_id"`
		CWD      string   `json:"cwd"`
		Preview  string   `json:"preview"`
		Updated  string   `json:"updated_at"`
		Hits     int      `json:"hits"`
		Snippet  string   `json:"snippet"`
		Name     string   `json:"name"`
		Tags     []string `json:"tags"`
	}
	out := make([]matchDTO, 0, len(matches))
	for _, m := range matches {
		e := store.Get(m.Session.ThreadID)
		out = append(out, matchDTO{
			ThreadID: m.Session.ThreadID,
			CWD:      m.Session.CWD,
			Preview:  m.Session.Preview,
			Updated:  m.Session.UpdatedAt().Format("2006-01-02 15:04"),
			Hits:     m.Hits,
			Snippet:  m.Snippet,
			Name:     e.Name,
			Tags:     e.Tags,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "matches": out})
}

// ---- scan (secrets) ----

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	kind := s.kindFromRequest(r)
	scan, err := s.scanSessions(kind)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	type finding struct {
		Type   string `json:"type"`
		Masked string `json:"masked"`
	}
	type hit struct {
		ThreadID string    `json:"thread_id"`
		CWD      string    `json:"cwd"`
		Preview  string    `json:"preview"`
		Findings []finding `json:"findings"`
	}
	var hits []hit
	total := 0
	for _, sn := range scan.Sessions {
		if sn.Compressed {
			continue
		}
		data, rerr := os.ReadFile(sn.Path)
		if rerr != nil {
			continue
		}
		found := secrets.Scan(data)
		if len(found) == 0 {
			continue
		}
		fs := make([]finding, 0, len(found))
		for _, f := range found {
			fs = append(fs, finding{Type: f.Type, Masked: f.Masked})
		}
		total += len(fs)
		hits = append(hits, hit{ThreadID: sn.ThreadID, CWD: sn.CWD, Preview: sn.Preview, Findings: fs})
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_count": len(hits), "secret_count": total, "sessions": hits})
}

// ---- resume ----

type resumeReq struct {
	Session string `json:"session"` // thread id or unique prefix
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	var req resumeReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	q := strings.TrimSpace(req.Session)
	if q == "" {
		apiError(w, http.StatusBadRequest, "choose a session to resume")
		return
	}
	kind := s.kindFromRequest(r)
	scan, err := s.scanSessions(kind)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	var match *sessions.Session
	var prefix []sessions.Session
	for i := range scan.Sessions {
		sn := scan.Sessions[i]
		if sn.ThreadID == "" {
			continue
		}
		if sn.ThreadID == q {
			match = &scan.Sessions[i]
			break
		}
		if strings.HasPrefix(sn.ThreadID, q) {
			prefix = append(prefix, sn)
		}
	}
	if match == nil {
		switch len(prefix) {
		case 1:
			match = &prefix[0]
		case 0:
			apiError(w, http.StatusNotFound, "no session matches "+q)
			return
		default:
			apiError(w, http.StatusBadRequest, "that prefix matches several sessions; use a longer id")
			return
		}
	}
	name, args := resumeCommand(kind, match.ThreadID)
	writeJSON(w, http.StatusOK, map[string]any{
		"thread_id": match.ThreadID,
		"cwd":       match.CWD,
		"preview":   match.Preview,
		"command":   name + " " + strings.Join(args, " "),
	})
}

// resumeCommand returns the agent command that continues a session by thread id.
// (Kept here so the WebUI does not depend on the cli package.)
func resumeCommand(kind agent.Kind, threadID string) (string, []string) {
	if agent.Normalize(kind) == agent.Claude {
		return "claude", []string{"--resume", threadID}
	}
	return "codex", []string{"resume", threadID}
}

// ---- tags & names ----

type tagsReq struct {
	Session    string   `json:"session"` // thread id (exact)
	AddTags    []string `json:"add_tags"`
	RemoveTags []string `json:"remove_tags"`
	SetName    *string  `json:"set_name"` // nil = leave unchanged; "" = clear
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	var req tagsReq
	if err := decodeBody(r, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Session) == "" {
		apiError(w, http.StatusBadRequest, "missing session id")
		return
	}
	store, dir := metaStore()
	for _, t := range req.AddTags {
		store.AddTag(req.Session, t)
	}
	for _, t := range req.RemoveTags {
		store.RemoveTag(req.Session, t)
	}
	if req.SetName != nil {
		store.SetName(req.Session, *req.SetName)
	}
	if err := store.Save(dir); err != nil {
		apiError(w, http.StatusInternalServerError, "cannot save: "+err.Error())
		return
	}
	e := store.Get(req.Session)
	writeJSON(w, http.StatusOK, map[string]any{"thread_id": req.Session, "name": e.Name, "tags": e.Tags})
}
