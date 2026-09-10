//go:build windows

package agentstatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNewInstallExecCommandUsesNoConsole(t *testing.T) {
	command := newInstallExecCommand(context.Background(), `C:\\Users\\tester\\codex.cmd`, "--version")
	assertHiddenConsoleCommand(t, command)
	if command.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", command.SysProcAttr.CreationFlags)
	}
}

func TestNewHiddenConsoleCommandHidesTaskkill(t *testing.T) {
	command := newHiddenConsoleCommand("taskkill.exe", "/PID", "42", "/T", "/F")
	assertHiddenConsoleCommand(t, command)
	want := []string{"taskkill.exe", "/PID", "42", "/T", "/F"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("command args = %#v, want %#v", command.Args, want)
	}
}

func assertHiddenConsoleCommand(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false")
	}
	if command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", command.SysProcAttr.CreationFlags)
	}
	if command.SysProcAttr.CreationFlags&windows.CREATE_NEW_CONSOLE != 0 {
		t.Fatalf("CreationFlags = %#x, must not include CREATE_NEW_CONSOLE", command.SysProcAttr.CreationFlags)
	}
}

func TestNewInstallShellCommandUsesCmdInterpreter(t *testing.T) {
	t.Setenv("ComSpec", `C:\Windows\System32\cmd.exe`)
	command := newInstallShellCommand(context.Background(), "echo tutti")
	want := []string{`C:\Windows\System32\cmd.exe`, "/D", "/S", "/C", "echo tutti"}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("command args = %#v, want %#v", command.Args, want)
	}
}

func TestLoginCommandDoesNotUsePOSIXSingleQuotesOnWindows(t *testing.T) {
	command := joinLoginShellCommand([]string{`C:\\Users\\tester\\AppData\\Local\\opencode.CMD`, "auth", "login"})
	want := `C:\\Users\\tester\\AppData\\Local\\opencode.CMD auth login`
	if command != want {
		t.Fatalf("login command = %q, want %q", command, want)
	}
}

func TestLoginCommandQuotesWindowsPathsWithSpaces(t *testing.T) {
	command := joinLoginShellCommand([]string{`C:\\Program Files\\opencode\\opencode.CMD`, "auth", "login"})
	want := `"C:\\Program Files\\opencode\\opencode.CMD" auth login`
	if command != want {
		t.Fatalf("login command = %q, want %q", command, want)
	}
}

func TestPlatformExecutableFileAcceptsPlainWindowsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command.exe")
	if err := os.WriteFile(path, []byte("command"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !platformExecutableFile(info) {
		t.Fatal("platformExecutableFile() = false")
	}
}

func TestOfficialScriptInvocationUsesManagedPosixShell(t *testing.T) {
	env := []string{
		"PATH=C:\\Windows\\System32",
		managedPosixShellInstallerEnv + `=C:\\Work\\managed-posix-shell\\usr\\bin\\bash.exe`,
	}
	command, args, gotEnv := officialScriptInvocation("bash", `C:\\Users\\tester\\AppData\\Local\\Temp\\install.sh`, env)
	wantArgs := []string{
		`C:\\Work\\managed-posix-shell\\usr\\bin\\bash.exe`,
		"--noprofile",
		"--norc",
		`C:\\Users\\tester\\AppData\\Local\\Temp\\install.sh`,
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	if command != joinShellCommand(wantArgs) {
		t.Fatalf("command = %q, want %q", command, joinShellCommand(wantArgs))
	}
	if got := installerEnvValue(gotEnv, "MSYS2_PATH_TYPE"); got != "inherit" {
		t.Fatalf("MSYS2_PATH_TYPE = %q, want inherit", got)
	}
}

func TestRunDefaultInstallCommandExecutesCmdPathWithSpaces(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "path with spaces", "fake npm.cmd")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		scriptPath,
		[]byte("@echo off\r\necho marker=%1\r\nexit /b 0\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result, err := runDefaultInstallCommand(context.Background(), InstallCommandInput{
		Args: []string{scriptPath, "hello world"},
		Env:  os.Environ(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "marker=\"hello world\"") {
		t.Fatalf("stdout = %q, want command argument to survive cmd.exe", result.Stdout)
	}
}
