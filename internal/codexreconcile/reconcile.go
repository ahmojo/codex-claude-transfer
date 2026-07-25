// Package codexreconcile asks Codex's native app-server to discover rollout
// files that cct has just imported. It never opens or writes Codex's SQLite
// database or session_index.jsonl itself.
package codexreconcile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout = 20 * time.Second
	maxMessageSize = 16 << 20
)

var (
	lookPath       = exec.LookPath
	commandContext = exec.CommandContext
	versionRE      = regexp.MustCompile(`(?i)codex(?: desktop|-cli)?/([0-9]+\.[0-9]+\.[0-9]+(?:[-+][^\s;()]+)?)`)
)

// Options configures a post-import reconciliation attempt.
type Options struct {
	CodexHome string
	// ThreadIDs must contain exact canonical UUID-shaped IDs. Reconcile rejects
	// the entire request before starting Codex if any value is invalid.
	ThreadIDs []string
	// CodexPath overrides executable discovery. It is primarily useful for
	// controlled integration tests; normal callers should leave it empty.
	CodexPath string
	Timeout   time.Duration
}

// Result describes which native Codex operations completed.
type Result struct {
	Version            string
	Requested          []string
	AlreadyDiscovered  []string
	ReadForRepair      []string
	Verified           []string
	VerificationMethod VerificationMethod
	Warnings           []string
}

// VerificationMethod identifies the native Codex list behavior used to prove
// that an imported thread is discoverable.
type VerificationMethod string

const (
	VerificationStateDB       VerificationMethod = "thread/list state DB"
	VerificationScanAndRepair VerificationMethod = "thread/list scan-and-repair"
)

// Reconcile starts a short-lived Codex app-server scoped to CodexHome, probes
// the protocol, asks Codex to read any missing thread IDs, then verifies them
// through thread/list. A failure never changes the imported rollout files.
// Thread IDs are validated again at this public boundary even when callers
// obtained them from an already-validated import scan.
func Reconcile(parent context.Context, opts Options) (Result, error) {
	requested, err := validatedUniqueSorted(opts.ThreadIDs)
	if err != nil {
		return Result{}, err
	}
	result := Result{Requested: requested}
	if len(result.Requested) == 0 {
		return result, nil
	}
	if strings.TrimSpace(opts.CodexHome) == "" {
		return result, errors.New("Codex home is empty")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	codexPath := opts.CodexPath
	if codexPath == "" {
		var err error
		codexPath, err = lookPath("codex")
		if err != nil {
			return result, fmt.Errorf("find Codex executable: %w", err)
		}
	}

	server, err := startServer(ctx, codexPath, opts.CodexHome)
	if err != nil {
		return result, err
	}
	defer server.close()

	serverInfo, err := server.initialize()
	if err != nil {
		return result, fmt.Errorf("Codex app-server initialize: %w", err)
	}
	result.Version = versionFromUserAgent(serverInfo.UserAgent)
	if !samePath(serverInfo.CodexHome, opts.CodexHome) {
		return result, fmt.Errorf("Codex app-server opened home %q, expected %q", serverInfo.CodexHome, opts.CodexHome)
	}

	stateOnly := true
	discovered, err := server.listThreadIDs(stateOnly)
	if err != nil {
		// Older app-server builds may reject useStateDbOnly. Falling back to
		// the default thread/list is safe: Codex itself performs any scan and
		// repair, and cct still never opens the index.
		if !isUnsupportedStateOnlyList(err) {
			return result, fmt.Errorf("probe Codex state-only thread/list: %w", err)
		}
		stateOnly = false
		discovered, err = server.listThreadIDs(stateOnly)
		if err != nil {
			return result, fmt.Errorf("Codex app-server lacks compatible thread/list capability: %w", err)
		}
		result.Warnings = append(result.Warnings, "Codex does not support state-only list verification; used its scan-and-repair thread/list fallback")
		result.VerificationMethod = VerificationScanAndRepair
	} else {
		result.VerificationMethod = VerificationStateDB
	}

	for _, id := range result.Requested {
		if discovered[id] {
			result.AlreadyDiscovered = append(result.AlreadyDiscovered, id)
			continue
		}
		if err := server.readThread(id); err != nil {
			return result, fmt.Errorf("Codex thread/read %s: %w", id, err)
		}
		result.ReadForRepair = append(result.ReadForRepair, id)
	}

	verified, err := server.listThreadIDs(stateOnly)
	if err != nil {
		return result, fmt.Errorf("verify Codex discovery with %s: %w", result.VerificationMethod, err)
	}
	var missing []string
	for _, id := range result.Requested {
		if verified[id] {
			result.Verified = append(result.Verified, id)
		} else {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return result, fmt.Errorf("Codex did not expose %d thread(s) after native read: %s", len(missing), strings.Join(missing, ", "))
	}
	return result, nil
}

func isUnsupportedStateOnlyList(err error) bool {
	var rpcErr *rpcError
	return errors.As(err, &rpcErr) && rpcErr.Code == -32602
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("RPC %d: %s (%s)", e.Code, e.Message, compactJSON(e.Data))
	}
	return fmt.Sprintf("RPC %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type appServer struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *lockedBuffer
	nextID  int
}

func startServer(ctx context.Context, codexPath, codexHome string) (*appServer, error) {
	cmd := codexCommand(ctx, codexPath, "app-server", "--stdio")
	cmd.Env = withEnv(os.Environ(), "CODEX_HOME", codexHome)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex app-server stdout: %w", err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxMessageSize)
	return &appServer{cmd: cmd, stdin: stdin, scanner: scanner, stderr: stderr}, nil
}

func codexCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat"):
		cmdArgs := []string{"/d", "/c", path}
		cmdArgs = append(cmdArgs, args...)
		return commandContext(ctx, "cmd.exe", cmdArgs...)
	case runtime.GOOS == "windows" && ext == ".ps1":
		cmdArgs := []string{"-NoProfile", "-NonInteractive", "-File", path}
		cmdArgs = append(cmdArgs, args...)
		return commandContext(ctx, "powershell.exe", cmdArgs...)
	default:
		return commandContext(ctx, path, args...)
	}
}

func (s *appServer) initialize() (struct {
	UserAgent string `json:"userAgent"`
	CodexHome string `json:"codexHome"`
}, error) {
	var result struct {
		UserAgent string `json:"userAgent"`
		CodexHome string `json:"codexHome"`
	}
	err := s.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "cct", "version": "post-import-reconcile"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &result)
	if err != nil {
		return result, err
	}
	if err := s.notify("initialized", nil); err != nil {
		return result, err
	}
	return result, nil
}

func (s *appServer) readThread(id string) error {
	var ignored json.RawMessage
	return s.call("thread/read", map[string]any{"threadId": id, "includeTurns": false}, &ignored)
}

func (s *appServer) listThreadIDs(stateOnly bool) (map[string]bool, error) {
	ids := make(map[string]bool)
	var cursor any
	seenCursors := make(map[string]bool)
	for page := 0; page < 10000; page++ {
		params := map[string]any{"limit": 100}
		if stateOnly {
			params["useStateDbOnly"] = true
		}
		if cursor != nil {
			params["cursor"] = cursor
		}
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			NextCursor any `json:"nextCursor"`
		}
		if err := s.call("thread/list", params, &result); err != nil {
			return nil, err
		}
		for _, thread := range result.Data {
			if thread.ID != "" {
				ids[thread.ID] = true
			}
		}
		if result.NextCursor == nil {
			return ids, nil
		}
		key := fmt.Sprint(result.NextCursor)
		if key == "" || seenCursors[key] {
			return nil, errors.New("thread/list returned a repeated pagination cursor")
		}
		seenCursors[key] = true
		cursor = result.NextCursor
	}
	return nil, errors.New("thread/list exceeded pagination safety limit")
}

func (s *appServer) call(method string, params any, out any) error {
	s.nextID++
	id := s.nextID
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	if err := s.write(request); err != nil {
		return err
	}
	for s.scanner.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(s.scanner.Bytes(), &response); err != nil {
			continue // Ignore non-JSON diagnostics and unrelated notifications.
		}
		if response.ID != id {
			continue
		}
		if response.Error != nil {
			return response.Error
		}
		if out == nil || len(response.Result) == 0 || string(response.Result) == "null" {
			return nil
		}
		if raw, ok := out.(*json.RawMessage); ok {
			*raw = append((*raw)[:0], response.Result...)
			return nil
		}
		if err := json.Unmarshal(response.Result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
	if err := s.scanner.Err(); err != nil {
		return fmt.Errorf("read Codex app-server response: %w%s", err, s.stderrSuffix())
	}
	return fmt.Errorf("Codex app-server closed before replying to %s%s", method, s.stderrSuffix())
}

func (s *appServer) notify(method string, params any) error {
	notification := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		notification["params"] = params
	}
	return s.write(notification)
}

func (s *appServer) write(message any) error {
	b, err := json.Marshal(message)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := s.stdin.Write(b); err != nil {
		return fmt.Errorf("write Codex app-server request: %w%s", err, s.stderrSuffix())
	}
	return nil
}

func (s *appServer) close() {
	_ = s.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

func (s *appServer) stderrSuffix() string {
	text := strings.TrimSpace(s.stderr.String())
	if text == "" {
		return ""
	}
	if len(text) > 2048 {
		text = text[len(text)-2048:]
	}
	return ": " + text
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func validatedUniqueSorted(ids []string) ([]string, error) {
	seen := make(map[string]bool)
	result := make([]string, 0, len(ids))
	for i, id := range ids {
		if !ValidThreadID(id) {
			return nil, fmt.Errorf("invalid Codex thread ID at position %d: expected UUID in 8-4-4-4-12 hexadecimal form", i+1)
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result, nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func withEnv(env []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

func versionFromUserAgent(userAgent string) string {
	match := versionRE.FindStringSubmatch(userAgent)
	if len(match) == 2 {
		return match[1]
	}
	return "unknown"
}

func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
