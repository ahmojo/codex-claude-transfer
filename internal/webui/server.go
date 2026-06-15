// Package webui serves codex-sync's local desktop GUI: a small single-page app
// served over a loopback-only HTTP server, backed by the same core packages as
// the CLI. It is pure standard library (no third-party web framework, no build
// step), so it cross-compiles to every target like the rest of the binary.
//
// Safety model: the server binds to 127.0.0.1 only (never a routable address),
// requires a per-launch random token on every /api call (so other local
// processes and malicious web pages cannot drive it), and checks the Host header
// to mitigate DNS-rebinding. It never uploads anything; it is just a local face
// over the existing export/import/inspect operations.
package webui

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ahmojo/Codex_Sync/internal/codexhome"
)

//go:embed static/*
var staticFiles embed.FS

// Options configures the desktop server.
type Options struct {
	CodexHome string // optional --codex-home override
	Port      int    // 0 = pick a free port
	NoBrowser bool   // do not auto-open the browser
}

// Server holds the running desktop UI state.
type Server struct {
	home  codexhome.Home
	token string
	out   io.Writer
}

// Run starts the desktop UI: it binds a loopback listener, prints (and opens) the
// URL, and serves until the process is interrupted. It blocks.
func Run(opts Options, stdout, stderr io.Writer) int {
	home, err := codexhome.Detect(opts.CodexHome)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot determine Codex home: %v\n", err)
		return 1
	}

	token, err := randomToken()
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot generate session token: %v\n", err)
		return 1
	}

	s := &Server{home: home, token: token, out: stdout}

	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot start local server on %s: %v\n", addr, err)
		return 1
	}

	url := fmt.Sprintf("http://%s/?token=%s", ln.Addr().String(), token)
	fmt.Fprintln(stdout, "codex-sync desktop is running locally (nothing is uploaded).")
	fmt.Fprintf(stdout, "Open this URL in your browser if it does not open automatically:\n  %s\n", url)
	fmt.Fprintln(stdout, "Press Ctrl-C to stop.")

	if !opts.NoBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}

	srv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "error: server stopped: %v\n", err)
		return 1
	}
	return 0
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static SPA. The HTML/JS/CSS contain no token; the token only ever arrives
	// via the launch URL's query string, which the page forwards on API calls.
	sub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(staticFiles, "static/index.html")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Serve index.html directly for "/" (FileServer would 301 index.html→/,
		// causing a redirect loop). Other paths go to the embedded file server.
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// API: token-gated, loopback-only.
	mux.HandleFunc("/api/doctor", s.guard(s.handleDoctor))
	mux.HandleFunc("/api/sessions", s.guard(s.handleSessions))
	mux.HandleFunc("/api/export", s.guard(s.handleExport))
	mux.HandleFunc("/api/inspect", s.guard(s.handleInspect))
	mux.HandleFunc("/api/import", s.guard(s.handleImport))
	return mux
}

// guard enforces the loopback Host check and the per-launch token on /api calls.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if subtleCompare(r.Header.Get("X-Codex-Sync-Token"), s.token) {
			h(w, r)
			return
		}
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
	}
}

// localHost reports whether the request targets a loopback host, mitigating
// DNS-rebinding (a remote page resolving to 127.0.0.1 would carry its own Host).
func localHost(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// subtleCompare is a length-aware constant-time-ish token comparison.
func subtleCompare(a, b string) bool {
	if len(a) != len(b) || a == "" {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// openBrowser opens url in the user's default browser, best-effort per OS.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
