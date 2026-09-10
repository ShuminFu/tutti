//go:build windows

package agentstatus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Provider status and authentication probes are background work. A fresh
// CREATE_NEW_CONSOLE still allocates a conhost window before HideWindow is
// observed and therefore flashes during short-lived probes. CREATE_NO_WINDOW
// prevents Windows from allocating that console in the first place.
// Keep this rule on cancellation helpers such as taskkill as well.

func newInstallExecCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(executable))
	var command *exec.Cmd
	switch extension {
	case ".cmd", ".bat":
		// Batch files cannot be passed directly to CreateProcess. Pass the argv
		// pieces to cmd.exe separately so Go performs the Windows quoting exactly
		// once. `call` keeps nested batch-file semantics explicit.
		command = exec.CommandContext(ctx, installCommandInterpreter(), append([]string{"/D", "/S", "/C", "call", executable}, args...)...)
	case ".ps1":
		// Cursor's official Windows installer exposes agent.ps1. PowerShell
		// scripts are not CreateProcess entry points, so invoke them explicitly.
		command = exec.CommandContext(ctx, "powershell.exe", append([]string{
			"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", executable,
		}, args...)...)
	default:
		command = exec.CommandContext(ctx, executable, args...)
	}
	configureInstallProcessCommand(command, ctx)
	return command
}

func newInstallShellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, resolveInstallerShell(), "/D", "/S", "/C", command)
	configureInstallProcessCommand(cmd, ctx)
	return cmd
}

// configureInstallProcessCommand makes context cancellation terminate the
// complete Windows process tree. npm/opencode and PowerShell launchers are
// wrappers; killing only the wrapper leaves the real child alive, which then
// makes the next ACP probe hang or contend for the provider's database.
func configureInstallProcessCommand(command *exec.Cmd, ctx context.Context) {
	configureHiddenConsoleCommand(command)
	command.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
	command.WaitDelay = 500 * time.Millisecond
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := newHiddenConsoleCommand("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
		if err == nil {
			return nil
		}
		if killErr := command.Process.Kill(); killErr != nil {
			return errors.Join(err, killErr)
		}
		return nil
	}
}

func newHiddenConsoleCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	configureHiddenConsoleCommand(command)
	return command
}

func configureHiddenConsoleCommand(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags &^= windows.CREATE_NEW_CONSOLE
	command.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

func platformExecutableFile(os.FileInfo) bool {
	return true
}

func resolveInstallerShell() string {
	if value := strings.TrimSpace(os.Getenv("ComSpec")); value != "" {
		return value
	}
	return "cmd.exe"
}

func installCommandInterpreter() string {
	return resolveInstallerShell()
}

func structuredInstallRunner() string {
	return "cmd.exe /D /S /C call"
}
