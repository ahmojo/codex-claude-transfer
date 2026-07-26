//go:build !windows

package codexreconcile

import (
	"os/exec"
	"time"
)

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 2 * time.Second
}
