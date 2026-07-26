//go:build windows

package codexreconcile

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const windowsErrorInvalidParameter syscall.Errno = 87

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
	nativeErr := terminateWindowsProcessTree(uint32(process.Pid))
	if nativeErr == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	taskkill := exec.CommandContext(ctx, "taskkill.exe", "/PID", strconv.Itoa(process.Pid), "/T", "/F")
	taskkill.Stdout = io.Discard
	taskkill.Stderr = io.Discard
	taskkillErr := taskkill.Run()
	if taskkillErr == nil {
		return nil
	}
	killErr := process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		return errors.Join(nativeErr, taskkillErr)
	}
	return errors.Join(nativeErr, taskkillErr, killErr)
}

func terminateWindowsProcessTree(rootPID uint32) error {
	descendants, err := windowsDescendantPIDs(rootPID)
	if err != nil {
		return err
	}
	for _, pid := range descendants {
		if err := terminateWindowsPID(pid); err != nil {
			return err
		}
	}
	return terminateWindowsPID(rootPID)
}

func windowsDescendantPIDs(rootPID uint32) ([]uint32, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snapshot)

	children := make(map[uint32][]uint32)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	for {
		children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
		err = syscall.Process32Next(snapshot, &entry)
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	var descendants []uint32
	visited := map[uint32]bool{rootPID: true}
	var collect func(uint32)
	collect = func(parent uint32) {
		for _, child := range children[parent] {
			if visited[child] {
				continue
			}
			visited[child] = true
			collect(child)
			descendants = append(descendants, child)
		}
	}
	collect(rootPID)
	return descendants, nil
}

func terminateWindowsPID(pid uint32) error {
	handle, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, pid)
	if errors.Is(err, windowsErrorInvalidParameter) { // Process already exited.
		return nil
	}
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	return syscall.TerminateProcess(handle, 1)
}
