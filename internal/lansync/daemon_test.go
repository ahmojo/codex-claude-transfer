package lansync

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
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
