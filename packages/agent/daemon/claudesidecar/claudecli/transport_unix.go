//go:build !windows

package claudecli

import (
	"os"
	"os/exec"
	"syscall"
)

func terminateProcess(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

func exitSignalName(err *exec.ExitError) string {
	status, ok := err.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
