package lansync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
)

func TestDaemonRequiresConfirmation(t *testing.T) {
	home := fakeHome(t)
	err := Daemon(context.Background(), home, Options{Tool: agent.Codex, Out: io.Discard, ConfigDir: t.TempDir()}, DaemonOptions{Once: true})
	if err == nil || !strings.Contains(err.Error(), "EXPERIMENTAL") {
		t.Fatalf("daemon without --i-understand should refuse, got %v", err)
	}
}

func TestDaemonRequiresRememberedPeer(t *testing.T) {
	home := fakeHome(t)
	err := Daemon(context.Background(), home, Options{Tool: agent.Codex, Out: io.Discard, Confirmed: true, ConfigDir: t.TempDir()}, DaemonOptions{Once: true})
	if err == nil || !strings.Contains(err.Error(), "remembered peers") {
		t.Fatalf("daemon with no remembered peers should refuse, got %v", err)
	}
}

func TestDaemonSweepsAtStartupAndRetriesUnchangedSessions(t *testing.T) {
	started := time.Unix(1000, 0)
	ticks := make(chan time.Time, 4)
	for _, seconds := range []int{5, 30, 35, 60} {
		ticks <- started.Add(time.Duration(seconds) * time.Second)
	}
	close(ticks)
	attempts := 0
	watchAndSync(context.Background(), []string{t.TempDir()}, ticks, started, func() {
		// A sweep may find no peer or fail. Neither condition changes any local
		// session file; the next periodic attempt must still happen.
		attempts++
	})
	if attempts != 3 {
		t.Fatalf("got %d discovery attempts, want startup and retries at 30s/60s", attempts)
	}
}

func TestDaemonLocalChangeSweepsBeforeRetryDeadline(t *testing.T) {
	root := t.TempDir()
	started := time.Unix(1000, 0)
	ticks := make(chan time.Time, 1)
	ticks <- started.Add(5 * time.Second)
	close(ticks)
	attempts := 0
	watchAndSync(context.Background(), []string{root}, ticks, started, func() {
		attempts++
		if attempts == 1 {
			if err := os.WriteFile(filepath.Join(root, "session.jsonl"), []byte("new turn\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if attempts != 2 {
		t.Fatalf("got %d sweeps, want startup and local-change sweep", attempts)
	}
}

func TestDaemonCancelledBeforeStartupDoesNotSweep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	watchAndSync(ctx, []string{t.TempDir()}, nil, time.Now(), func() {
		t.Fatal("cancelled daemon performed a sweep")
	})
}

func TestDaemonReportsConflictsWarningsAndFailures(t *testing.T) {
	var out bytes.Buffer
	reportDaemonSync(&out, "laptop", Result{Received: bundle.ImportResult{
		Conflicts: 1, Warnings: []string{"session diverged; local file preserved"},
	}}, nil)
	if !strings.Contains(out.String(), "conflicts 1") || !strings.Contains(out.String(), "local file preserved") {
		t.Fatalf("conflict-only result was hidden: %s", out.String())
	}
	out.Reset()
	reportDaemonSync(&out, "laptop", Result{}, errors.New("transfer interrupted"))
	if !strings.Contains(out.String(), "failed: transfer interrupted") {
		t.Fatalf("failed transfer was hidden: %s", out.String())
	}
}

// Exercise the actual daemon accept path so reporting cannot become a helper
// that passes unit tests while inbound conflicts are silently discarded.
func TestDaemonInboundConflictIsReportedAndPreserved(t *testing.T) {
	clientHome, serverHome := fakeHome(t), fakeHome(t)
	const id = "aaaa1111-2222-3333-4444-555566667777"
	writeSession(t, clientHome, id, "/project", "client-only turn")
	writeSession(t, serverHome, id, "/project", "server-only turn")
	original := readSession(t, serverHome, id)
	clientConfig, serverConfig := t.TempDir(), t.TempDir()
	clientCert, err := loadOrCreateIdentity(clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := loadOrCreateIdentity(serverConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := rememberPeer(clientConfig, "server", fingerprint(serverCert.Certificate[0])); err != nil {
		t.Fatal(err)
	}
	if err := rememberPeer(serverConfig, "client", fingerprint(clientCert.Certificate[0])); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer listener.Close()
	out := &daemonCapture{changed: make(chan struct{}, 20)}
	go acceptLoop(ctx, listener, serverHome, Options{Tool: agent.Codex, Out: out, ConfigDir: serverConfig}, serverCert)
	result, err := Connect(clientHome, Options{Tool: agent.Codex, Out: io.Discard, ConfigDir: clientConfig, Confirmed: true}, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if result.Received.Conflicts != 1 {
		t.Fatalf("client conflicts = %d", result.Received.Conflicts)
	}
	for !strings.Contains(out.text(), "conflicts 1") {
		select {
		case <-out.changed:
		case <-ctx.Done():
			t.Fatalf("daemon hid inbound conflict: %s", out.text())
		}
	}
	if got := readSession(t, serverHome, id); got != original {
		t.Fatal("daemon changed the conflicting destination")
	}
}

type daemonCapture struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	changed chan struct{}
}

func (c *daemonCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	n, err := c.buf.Write(p)
	c.mu.Unlock()
	select {
	case c.changed <- struct{}{}:
	default:
	}
	return n, err
}
func (c *daemonCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func TestDaemonReportsPartialImportWhenTransferFails(t *testing.T) {
	var out bytes.Buffer
	reportDaemonSync(&out, "laptop", Result{Received: bundle.ImportResult{
		Imported: 2, Updated: 1, Conflicts: 1,
		Warnings: []string{"session diverged; local file preserved"},
	}}, errors.New("send offer interrupted"))
	for _, want := range []string{
		"failed: send offer interrupted",
		"received 2, updated 1, conflicts 1",
		"session diverged; local file preserved",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("partial sync omitted %q: %s", want, out.String())
		}
	}
}
