package agentstatus

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

func TestClaudeOfficialInstallerFallbackOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix official-script fallback chain")
	}
	home := t.TempDir()
	runtimeRoot := fakeManagedRuntimeRoot(t)
	service := probeTestService(home)
	service.ManagedRuntime = fakeManagedRuntimeResolver(t, runtimeRoot)
	service.Environ = func() []string {
		return []string{"PATH=/usr/bin:/bin", agentNPMRegistryEnv + "=https://registry.example.test"}
	}
	service.HTTPClient = &http.Client{Transport: installFallbackRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("#!/bin/sh\nexit 1\n")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	service.LookPath = func(name string) (string, error) {
		if name != "brew" {
			t.Fatalf("LookPath(%q), want brew", name)
		}
		return "/opt/homebrew/bin/brew", nil
	}

	var commands []InstallCommandInput
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		commands = append(commands, input)
		switch len(commands) {
		case 1, 2:
			return InstallCommandResult{ExitCode: 1}, nil
		case 3:
			return InstallCommandResult{ExitCode: 0}, nil
		default:
			t.Fatalf("unexpected command %#v", input)
			return InstallCommandResult{}, nil
		}
	}

	spec := InstallerSpec{
		Kind:            InstallerKindOfficialScript,
		ScriptURL:       "https://claude.example/install.sh",
		ScriptShell:     "bash",
		HomebrewFormula: "claude-code",
		ManagedNPM: &ManagedNPMPackageInstallerSpec{
			PackageName: "@anthropic-ai/claude-code",
			BinaryName:  "claude",
		},
	}
	command, result, err := service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("executeInstaller() = %#v, %v", result, err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want official, npm, brew", len(commands))
	}
	if !strings.Contains(command, " -> npm install -g @anthropic-ai/claude-code") {
		t.Fatalf("reported command = %q, want official and managed npm attempts", command)
	}
	if !strings.HasPrefix(commands[0].Command, "bash ") || len(commands[0].Args) != 2 || commands[0].Args[0] != "bash" {
		t.Fatalf("official command = %#v", commands[0])
	}
	if !strings.Contains(commands[1].Command, "npm") || !strings.Contains(strings.Join(commands[1].Args, " "), "@anthropic-ai/claude-code") {
		t.Fatalf("npm command = %#v", commands[1])
	}
	if commands[2].Command != "/opt/homebrew/bin/brew" || strings.Join(commands[2].Args, " ") != "install claude-code" {
		t.Fatalf("brew command = %#v", commands[2])
	}
	for _, command := range commands {
		if strings.Contains(command.Command+" "+strings.Join(command.Args, " "), "sudo") {
			t.Fatalf("installer invoked sudo: %#v", command)
		}
	}
}

func TestClaudeOfficialInstallerSkipsUndiscoverableHomebrew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix official-script fallback chain")
	}
	home := t.TempDir()
	runtimeRoot := fakeManagedRuntimeRoot(t)
	service := probeTestService(home)
	service.ManagedRuntime = fakeManagedRuntimeResolver(t, runtimeRoot)
	service.Environ = func() []string {
		return []string{"PATH=/usr/bin:/bin", agentNPMRegistryEnv + "=https://registry.example.test"}
	}
	service.HTTPClient = &http.Client{Transport: installFallbackRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("#!/bin/sh\nexit 1\n")), Header: make(http.Header), Request: request}, nil
	})}
	service.LookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	var commands []InstallCommandInput
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		commands = append(commands, input)
		if len(commands) == 1 {
			return InstallCommandResult{ExitCode: 1}, errors.New("official process failed to start")
		}
		return InstallCommandResult{ExitCode: 1}, errors.New("npm process failed to start")
	}
	spec := InstallerSpec{Kind: InstallerKindOfficialScript, ScriptURL: "https://claude.example/install.sh", ScriptShell: "bash", HomebrewFormula: "claude-code", ManagedNPM: &ManagedNPMPackageInstallerSpec{PackageName: "@anthropic-ai/claude-code", BinaryName: "claude"}}
	_, result, _ := service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want official and npm only", len(commands))
	}
	for _, marker := range []string{"[official]", "[managed-npm]"} {
		if !strings.Contains(result.Stderr, marker) {
			t.Fatalf("stderr = %q, want %q", result.Stderr, marker)
		}
	}
	for _, detail := range []string{"official process failed to start", "npm process failed to start"} {
		if !strings.Contains(result.Stderr, detail) {
			t.Fatalf("stderr = %q, want process error %q", result.Stderr, detail)
		}
	}
	if strings.Contains(result.Stderr, installFallbackFailedMarker) {
		t.Fatalf("stderr = %q, Unix Homebrew chain must not carry the Windows two-stage failure marker", result.Stderr)
	}
}

func TestOfficialInstallerDoesNotAddUnixManagedNPMFallbackWithoutHomebrew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix official-script behavior")
	}
	service := claudeFallbackTestService(t)
	var commands []InstallCommandInput
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		commands = append(commands, input)
		if len(commands) == 1 {
			return InstallCommandResult{ExitCode: 1, Stderr: "official failed"}, nil
		}
		return InstallCommandResult{ExitCode: 0, Stdout: "installed"}, nil
	}
	spec := claudeFallbackInstallerSpec()
	spec.HomebrewFormula = ""
	_, result, err := service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if err != nil || result.ExitCode == 0 || len(commands) != 1 {
		t.Fatalf("executeInstaller() = %#v, %v; commands = %d, want original Unix official-only failure", result, err, len(commands))
	}
}

func TestOfficialScriptManagedNPMFallbackScope(t *testing.T) {
	spec := claudeFallbackInstallerSpec()
	if !officialScriptUsesManagedNPMFallback("windows", spec) {
		t.Fatal("PowerShell-backed Windows installer should have managed npm recovery")
	}
	managedPrimary := spec
	managedPrimary.WindowsFallback = providerregistry.InstallerWindowsFallbackManagedNPM
	if officialScriptUsesManagedNPMFallback("windows", managedPrimary) {
		t.Fatal("managed npm Windows primary must not run the same installer twice")
	}
	withoutHomebrew := spec
	withoutHomebrew.HomebrewFormula = ""
	if officialScriptUsesManagedNPMFallback("darwin", withoutHomebrew) {
		t.Fatal("Unix provider without the historical Homebrew chain must not gain a new npm fallback")
	}
}

func TestInstallerFailureReasonPrefersFailedFallback(t *testing.T) {
	spec := InstallerSpec{FailureReasonMarkers: map[string][]string{
		"install_source_unreachable": {"tutti_install_source_unreachable"},
	}}
	message := "TUTTI_INSTALL_SOURCE_UNREACHABLE\n" + installFallbackFailedMarker
	if got := installerFailureReasonCode(spec, message, "install_command_failed"); got != "install_fallback_failed" {
		t.Fatalf("reason = %q, want install_fallback_failed", got)
	}
}

func TestInstallerFallbackMarkerSurvivesActionOutputTrim(t *testing.T) {
	official := labelInstallCommandResult("official", InstallCommandResult{Stderr: strings.Repeat("o", 5000)})
	npm := labelInstallCommandResult("managed-npm", InstallCommandResult{Stderr: strings.Repeat("n", 5000)})
	result := markInstallFallbackFailure("windows", combineInstallCommandResults(official, npm), true)
	message := trimActionOutput(result.Stderr)
	if got := installerFailureReasonCode(InstallerSpec{}, message, "install_command_failed"); got != "install_fallback_failed" {
		t.Fatalf("reason = %q, want install_fallback_failed after output trim", got)
	}
	for _, label := range []string{"[official]", "[managed-npm]"} {
		if !strings.Contains(message, label) {
			t.Fatalf("trimmed stderr omitted stage label %q: %q", label, message)
		}
	}
	unixResult := markInstallFallbackFailure("darwin", InstallCommandResult{Stderr: "npm failed"}, true)
	if strings.Contains(unixResult.Stderr, installFallbackFailedMarker) {
		t.Fatalf("Unix stderr = %q, must not gain the Windows two-stage failure marker", unixResult.Stderr)
	}
}

func TestInstallActionErrorDistinguishesMissingRuntimeTargets(t *testing.T) {
	for _, test := range []struct {
		err    error
		reason string
	}{
		{err: errInstalledCLINotDetected, reason: "installed_cli_not_detected"},
		{err: errInstalledAdapterNotDetected, reason: "installed_adapter_not_detected"},
	} {
		result := installActionErrorResult(RunActionResult{}, test.err, time.Minute, InstallerSpec{})
		if result.ReasonCode != test.reason {
			t.Fatalf("error %v produced reason %q, want %q", test.err, result.ReasonCode, test.reason)
		}
	}
}

func TestWindowsPowerShellCommandEnablesUTF8Output(t *testing.T) {
	got := windowsPowerShellUTF8Command("Write-Error '无法连接'")
	if !strings.Contains(got, "[System.Text.UTF8Encoding]::new($false)") || !strings.HasSuffix(got, "Write-Error '无法连接'") {
		t.Fatalf("PowerShell command = %q", got)
	}
}

func TestClaudeOfficialInstallerStopsAfterSuccessfulManagedNPMFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix official-script fallback chain")
	}
	service := claudeFallbackTestService(t)
	service.LookPath = func(string) (string, error) {
		t.Fatal("LookPath(brew) called after successful npm fallback")
		return "", os.ErrNotExist
	}
	var commands []InstallCommandInput
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		commands = append(commands, input)
		if len(commands) == 1 {
			return InstallCommandResult{ExitCode: 1}, nil
		}
		return InstallCommandResult{ExitCode: 0}, nil
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("executeInstaller() = %#v, %v", result, err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want official and npm only", len(commands))
	}
}

func TestClaudeOfficialInstallerStopsAfterOfficialSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix official-script fallback chain")
	}
	service := claudeFallbackTestService(t)
	service.LookPath = func(string) (string, error) {
		t.Fatal("LookPath(brew) called after successful official installer")
		return "", os.ErrNotExist
	}
	var commands []InstallCommandInput
	service.InstallCommand = func(_ context.Context, input InstallCommandInput) (InstallCommandResult, error) {
		commands = append(commands, input)
		return InstallCommandResult{ExitCode: 0}, nil
	}

	_, result, err := service.executeInstaller(context.Background(), "claude-code", claudeFallbackInstallerSpec(), nil)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("executeInstaller() = %#v, %v", result, err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want official only", len(commands))
	}
}

func claudeFallbackTestService(t *testing.T) Service {
	t.Helper()
	home := t.TempDir()
	runtimeRoot := fakeManagedRuntimeRoot(t)
	service := probeTestService(home)
	service.ManagedRuntime = fakeManagedRuntimeResolver(t, runtimeRoot)
	service.Environ = func() []string {
		return []string{"PATH=/usr/bin:/bin", agentNPMRegistryEnv + "=https://registry.example.test"}
	}
	service.HTTPClient = &http.Client{Transport: installFallbackRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("#!/bin/sh\nexit 1\n")), Header: make(http.Header), Request: request}, nil
	})}
	return service
}

func claudeFallbackInstallerSpec() InstallerSpec {
	return InstallerSpec{
		Kind:                     InstallerKindOfficialScript,
		ScriptURL:                "https://claude.example/install.sh",
		ScriptShell:              "bash",
		WindowsFallback:          providerregistry.InstallerWindowsFallbackPowerShell,
		WindowsPowerShellCommand: "irm https://claude.example/install.ps1 -TimeoutSec 30 | iex",
		HomebrewFormula:          "claude-code",
		ManagedNPM: &ManagedNPMPackageInstallerSpec{
			PackageName:     "@anthropic-ai/claude-code",
			BinaryName:      "claude",
			IncludeOptional: true,
			VerifyBinary:    runtime.GOOS == "windows",
		},
	}
}

type installFallbackRoundTripFunc func(*http.Request) (*http.Response, error)

func (f installFallbackRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
