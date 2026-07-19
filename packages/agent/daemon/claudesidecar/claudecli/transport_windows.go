//go:build windows

package claudecli

import (
	"os"
	"os/exec"
)

func terminateProcess(process *os.Process) error {
	return process.Kill()
}

func exitSignalName(*exec.ExitError) string {
	return ""
}
