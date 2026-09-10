//go:build windows

package agentstatus

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsClaudeOfficialSuccessSkipsManagedNPM(t *testing.T) {
	service, _ := windowsClaudeInstallTestService(t)
	calls := 0
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		calls++
		assertWindowsClaudeOfficialCommand(t, input)
		assertWindowsClaudeProxyEnv(t, input.Env)
		return InstallCommandResult{ExitCode: 0, Stdout: "official installed"}, nil
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	if err != nil || result.ExitCode != 0 || calls != 1 {
		t.Fatalf("executeInstaller() = %#v, %v; calls = %d, want official-only success", result, err, calls)
	}
}

func TestWindowsClaudeOfficialSourceFailureFallsBackToRealCmdProbe(t *testing.T) {
	service, home := windowsClaudeInstallTestService(t)
	calls := 0
	service.InstallCommand = func(ctx context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		if err := ctx.Err(); err != nil {
			t.Fatalf("installer stage started with canceled outer context: %v", err)
		}
		calls++
		if calls == 1 {
			assertWindowsClaudeOfficialCommand(t, input)
			assertWindowsClaudeProxyEnv(t, input.Env)
			return InstallCommandResult{ExitCode: 1, Stderr: "TUTTI_INSTALL_SOURCE_UNREACHABLE: timed out after 30 seconds"}, nil
		}
		assertWindowsClaudeProxyEnv(t, input.Env)
		writeWindowsBatch(t, filepath.Join(home, ".local", "bin", "claude.cmd"), "@echo off\r\npowershell.exe -NoLogo -NoProfile -NonInteractive -Command \"Start-Sleep -Seconds 6\"\r\necho 2.1.0 (Claude Code)\r\n")
		return InstallCommandResult{ExitCode: 0, Stdout: "managed npm installed"}, nil
	}

	started := time.Now()
	command, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	elapsed := time.Since(started)
	if err != nil || result.ExitCode != 0 || calls != 2 {
		t.Fatalf("executeInstaller() = %#v, %v; calls = %d, want npm fallback success", result, err, calls)
	}
	if elapsed < 5*time.Second || elapsed >= defaultInstallVerifyTimeout {
		t.Fatalf("real claude.cmd verification took %s, want >5s and <15s", elapsed)
	}
	if !strings.Contains(command, " -> npm install -g @anthropic-ai/claude-code") {
		t.Fatalf("reported command = %q, want official -> managed npm", command)
	}
}

func TestWindowsClaudeOfficialTimeoutLeavesBudgetForFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connectionDone := make(chan struct{})
	defer close(connectionDone)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		<-connectionDone
	}()

	service, home := windowsClaudeInstallTestService(t)
	service.InstallTimeout = 45 * time.Second
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	specs, err := DefaultRegistry().Select([]string{"claude-code"})
	if err != nil || len(specs) != 1 {
		t.Fatalf("select production Claude installer = %#v, %v", specs, err)
	}
	spec := specs[0].Install
	const productionURL = "https://claude.ai/install.ps1"
	loopbackURL := "http://" + listener.Addr().String() + "/install.ps1"
	if !strings.Contains(spec.WindowsPowerShellCommand, productionURL) {
		t.Fatalf("production PowerShell command omitted %s: %q", productionURL, spec.WindowsPowerShellCommand)
	}
	spec.WindowsPowerShellCommand = strings.Replace(
		spec.WindowsPowerShellCommand,
		productionURL,
		loopbackURL,
		1,
	)
	if !strings.Contains(spec.WindowsPowerShellCommand, loopbackURL) || strings.Contains(spec.WindowsPowerShellCommand, productionURL) {
		t.Fatalf("PowerShell test command did not target loopback: %q", spec.WindowsPowerShellCommand)
	}
	calls := 0
	officialElapsed := time.Duration(0)
	service.InstallCommand = func(ctx context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		calls++
		if calls == 1 {
			started := time.Now()
			result, runErr := runDefaultInstallCommand(ctx, input)
			officialElapsed = time.Since(started)
			return result, runErr
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("npm fallback inherited canceled outer context: %v", err)
		}
		writeWindowsBatch(t, filepath.Join(home, ".local", "bin", "claude.cmd"), "@echo off\r\necho 2.1.0 (Claude Code)\r\n")
		return InstallCommandResult{ExitCode: 0, Stdout: "managed npm installed"}, nil
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if err != nil || result.ExitCode != 0 || calls != 2 {
		t.Fatalf("executeInstaller() = %#v, %v; calls=%d, want timeout then fallback success", result, err, calls)
	}
	if officialElapsed < 25*time.Second || officialElapsed > 38*time.Second {
		t.Fatalf("real Invoke-RestMethod timeout took %s, want approximately 30s", officialElapsed)
	}
}

func TestWindowsClaudeFallbackFailureKeepsReasonAndBothStageSummaries(t *testing.T) {
	service, _ := windowsClaudeInstallTestService(t)
	calls := 0
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		calls++
		if calls == 1 {
			assertWindowsClaudeOfficialCommand(t, input)
			return InstallCommandResult{ExitCode: 1, Stderr: strings.Repeat("official failure ", 500)}, nil
		}
		return InstallCommandResult{ExitCode: 1, Stderr: strings.Repeat("npm failure ", 500)}, nil
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	if err != nil || result.ExitCode == 0 || calls != 2 {
		t.Fatalf("executeInstaller() = %#v, %v; calls = %d, want two-stage failure", result, err, calls)
	}
	message := trimActionOutput(result.Stderr)
	if got := installerFailureReasonCode(InstallerSpec{}, message, "install_command_failed"); got != "install_fallback_failed" {
		t.Fatalf("reason = %q, want install_fallback_failed; stderr=%q", got, message)
	}
	for _, marker := range []string{"[official]", "[managed-npm]"} {
		if !strings.Contains(message, marker) {
			t.Fatalf("stderr omitted %q after trim: %q", marker, message)
		}
	}
}

func TestWindowsClaudeFallbackOuterBudgetReportsInstallTimeout(t *testing.T) {
	service, _ := windowsClaudeInstallTestService(t)
	service.InstallTimeout = 100 * time.Millisecond
	calls := 0
	service.InstallCommand = func(ctx context.Context, _ InstallCommandInput) (InstallCommandResult, error) {
		calls++
		if calls == 1 {
			return InstallCommandResult{ExitCode: 1, Stderr: "official failed"}, nil
		}
		<-ctx.Done()
		return InstallCommandResult{ExitCode: 1, Stderr: "npm still running"}, ctx.Err()
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	actionResult := installActionErrorResult(RunActionResult{Stderr: result.Stderr}, err, service.installTimeout(), InstallerSpec{})
	if calls != 2 || !errors.Is(err, context.DeadlineExceeded) || actionResult.ReasonCode != "install_timed_out" {
		t.Fatalf("calls=%d err=%v action=%#v, want fallback outer timeout", calls, err, actionResult)
	}
}

func TestWindowsClaudeInstallVerificationCancelsWholeCmdProcessTree(t *testing.T) {
	service, home := windowsClaudeInstallTestService(t)
	installPrefix := filepath.Join(home, ".local", "bin")
	marker := filepath.Join(home, "child-survived.txt")
	child := filepath.Join(home, "slow child.cmd")
	writeWindowsBatch(t, child, "@echo off\r\nping.exe -n 21 127.0.0.1 >nul\r\n> \""+marker+"\" echo survived\r\n")
	writeWindowsBatch(t, filepath.Join(installPrefix, "claude.cmd"), "@echo off\r\ncall \""+child+"\"\r\necho 2.1.0 (Claude Code)\r\n")

	started := time.Now()
	ready := service.managedNPMPackageInstallReady(context.Background(), "claude-code", ManagedNPMPackageInstallerSpec{
		BinaryName:   "claude",
		VerifyBinary: true,
	}, installPrefix, os.Environ(), "")
	elapsed := time.Since(started)
	if ready || elapsed < 14*time.Second || elapsed > 18*time.Second {
		t.Fatalf("verification ready=%v elapsed=%s, want cancellation at 15s", ready, elapsed)
	}
	time.Sleep(6 * time.Second)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("canceled claude.cmd left a child process running; marker stat error=%v", err)
	}
}

func windowsClaudeInstallTestService(t *testing.T) (Service, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "User Profile")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	service := probeTestService(home)
	service.ManagedRuntime = fakeManagedRuntimeResolver(t, fakeManagedRuntimeRoot(t))
	t.Setenv(agentNPMRegistryEnv, "https://registry.example.test")
	t.Setenv("HTTP_PROXY", "http://explicit-http-proxy.example.test:8080")
	t.Setenv("HTTPS_PROXY", "http://explicit-https-proxy.example.test:8443")
	service.Environ = os.Environ
	service.IsExecutableFile = isTestExecutableUnderHome(home)
	return service, home
}

func assertWindowsClaudeProxyEnv(t *testing.T, env []string) {
	t.Helper()
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[strings.ToUpper(key)] = value
		}
	}
	if values["HTTP_PROXY"] != "http://explicit-http-proxy.example.test:8080" || values["HTTPS_PROXY"] != "http://explicit-https-proxy.example.test:8443" {
		t.Fatalf("proxy env = %#v, want explicit HTTP_PROXY/HTTPS_PROXY preserved", values)
	}
}

func assertWindowsClaudeOfficialCommand(t *testing.T, input InstallCommandInput) {
	t.Helper()
	joined := strings.Join(input.Args, " ")
	if len(input.Args) == 0 || !strings.EqualFold(input.Args[0], "powershell.exe") || !strings.Contains(joined, "-TimeoutSec 30") {
		t.Fatalf("official input = %#v, want PowerShell source timeout", input)
	}
}

func writeWindowsBatch(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
