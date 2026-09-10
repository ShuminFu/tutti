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
	_, result, err := service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("executeInstaller() = %#v, %v", result, err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want official, npm, brew", len(commands))
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
		return InstallCommandResult{ExitCode: 1}, errors.New("failed")
	}
	spec := InstallerSpec{Kind: InstallerKindOfficialScript, ScriptURL: "https://claude.example/install.sh", ScriptShell: "bash", HomebrewFormula: "claude-code", ManagedNPM: &ManagedNPMPackageInstallerSpec{PackageName: "@anthropic-ai/claude-code", BinaryName: "claude"}}
	_, _, _ = service.executeInstaller(context.Background(), "claude-code", spec, nil)
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want official and npm only", len(commands))
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
		Kind:            InstallerKindOfficialScript,
		ScriptURL:       "https://claude.example/install.sh",
		ScriptShell:     "bash",
		HomebrewFormula: "claude-code",
		ManagedNPM: &ManagedNPMPackageInstallerSpec{
			PackageName: "@anthropic-ai/claude-code",
			BinaryName:  "claude",
		},
	}
}

type installFallbackRoundTripFunc func(*http.Request) (*http.Response, error)

func (f installFallbackRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
