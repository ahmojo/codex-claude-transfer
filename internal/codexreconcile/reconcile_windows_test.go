//go:build windows

package codexreconcile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReconcileWindowsWrapperTimeoutKillsChild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	pidPath := filepath.Join(temp, "child.pid")
	t.Cleanup(func() { killPIDFileProcess(pidPath) })
	wrapperPath := filepath.Join(temp, "codex.cmd")
	wrapper := fmt.Sprintf("@echo off\r\n\"%s\" -test.run=^TestReconcileWindowsNeverReplyChild$ --\r\n", executable)
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCT_RECONCILE_NEVER_REPLY", "1")
	t.Setenv("CCT_RECONCILE_CHILD_PID", pidPath)
	home := t.TempDir()

	type outcome struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		started := time.Now()
		_, err := Reconcile(context.Background(), Options{
			CodexHome: home,
			ThreadIDs: []string{testThreadA},
			CodexPath: wrapperPath,
			Timeout:   2 * time.Second,
		})
		done <- outcome{err: err, elapsed: time.Since(started)}
	}()

	pid := waitForPID(t, pidPath)
	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("Reconcile unexpectedly succeeded")
		}
		if got.elapsed > 4*time.Second {
			t.Fatalf("Reconcile returned after %s, want bounded cancellation", got.elapsed)
		}
	case <-time.After(5 * time.Second):
		killPIDFileProcess(pidPath)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("Reconcile remained blocked after its timeout")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if err := process.Kill(); err == nil {
		t.Fatalf("wrapper child process %d survived reconciliation timeout", pid)
	}
}

func TestReconcileWindowsNeverReplyChild(t *testing.T) {
	if os.Getenv("CCT_RECONCILE_NEVER_REPLY") != "1" {
		return
	}
	pidPath := os.Getenv("CCT_RECONCILE_CHILD_PID")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(2)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("wrapper child did not write PID file %s", path)
	return 0
}

func killPIDFileProcess(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	_ = exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}
