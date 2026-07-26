//go:build windows

package codexreconcile

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return killWindowsProcessTree(cmd.Process)
	}
	cmd.WaitDelay = 2 * time.Second
}

func killWindowsProcessTree(process *os.Process) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	taskkill := exec.CommandContext(ctx, "taskkill.exe", "/PID", strconv.Itoa(process.Pid), "/T", "/F")
	taskkill.Stdout = io.Discard
	taskkill.Stderr = io.Discard
	if err := taskkill.Run(); err == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	return err
}
