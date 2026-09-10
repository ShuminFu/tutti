package agentstatus

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProbeCodexAppServerClassifiesMissingOptionalDependencyFromStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	platform, ok := codexNpmPlatformDir(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skip("unsupported Codex platform")
	}
	platformPackage := "@openai/" + platform
	command := filepath.Join(t.TempDir(), "codex")
	writeExecutable(t, command, "#!/bin/sh\n"+
		"echo 'Error: Missing optional dependency "+platformPackage+". Reinstall @openai/codex with optional dependencies enabled.' >&2\n"+
		"exit 1\n")

	service := Service{
		ProbeTimeout:    3 * time.Second,
		ProbeReadyAfter: 100 * time.Millisecond,
	}
	evidence := service.probeCodexAppServer(
		context.Background(),
		[]string{command, "app-server"},
		[]string{"PATH=" + filepath.Dir(command)},
	)

	if evidence.CommandStarted != true || evidence.ProtocolReady {
		t.Fatalf("evidence = %#v, want a started command with failed protocol", evidence)
	}
	if evidence.Category != "platform_package_enoent" || evidence.PlatformPackageName != platformPackage {
		t.Fatalf("evidence = %#v, want missing platform package %q classified from stderr", evidence, platformPackage)
	}
}

func TestProbeCodexAppServerClassifiesUnsupportedSubcommandFromStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	command := filepath.Join(t.TempDir(), "codex")
	writeExecutable(t, command, "#!/bin/sh\n"+
		"echo \"error: unrecognized subcommand 'app-server'\" >&2\n"+
		"exit 2\n")

	service := Service{
		ProbeTimeout:    3 * time.Second,
		ProbeReadyAfter: 100 * time.Millisecond,
	}
	evidence := service.probeCodexAppServer(
		context.Background(),
		[]string{command, "app-server"},
		[]string{"PATH=" + filepath.Dir(command)},
	)

	if evidence.Category != "app_server_unsupported" {
		t.Fatalf("evidence = %#v, want app_server_unsupported classified from stderr", evidence)
	}
	logOutput := output.String()
	for _, field := range []string{
		"event=tutti.agent_provider.codex.app_server_probe.failed",
		"launcherPath=" + command,
		"failureStage=protocol",
		"commandStarted=true",
		"protocolReady=false",
		"category=app_server_unsupported",
		"durationMs=",
		"unrecognized subcommand",
	} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("Codex probe failure log missing %q:\n%s", field, logOutput)
		}
	}
}

func TestUseCodexWindowsCLIProbeMatchesGOOS(t *testing.T) {
	if useCodexWindowsCLIProbe() != (runtime.GOOS == "windows") {
		t.Fatalf("useCodexWindowsCLIProbe() = %v, want GOOS==windows", useCodexWindowsCLIProbe())
	}
}

func TestProbeCodexWindowsCLITreatsVersionAsReadyWithoutAppServer(t *testing.T) {
	command := writeCodexWindowsCLIProbeFixture(t, "0.145.0", true)
	service := Service{
		ProbeTimeout:    3 * time.Second,
		ProbeReadyAfter: 100 * time.Millisecond,
	}
	evidence := service.probeCodexWindowsCLI(
		context.Background(),
		[]string{command, "app-server"},
		[]string{"PATH=" + filepath.Dir(command)},
	)
	if !evidence.CommandStarted || !evidence.ProtocolReady || evidence.Category != "" {
		t.Fatalf("evidence = %#v, want CLI --version readiness without an app-server handshake", evidence)
	}
}

func TestProbeCodexWindowsCLIFailsWhenVersionMissing(t *testing.T) {
	command := writeCodexWindowsCLIProbeFixture(t, "", false)
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	service := Service{
		ProbeTimeout:    3 * time.Second,
		ProbeReadyAfter: 100 * time.Millisecond,
	}
	evidence := service.probeCodexWindowsCLI(context.Background(), []string{command, "app-server"}, nil)
	if !evidence.CommandStarted || evidence.ProtocolReady || evidence.Category != "cli_version_failed" {
		t.Fatalf("evidence = %#v, want a started CLI with no version", evidence)
	}
	if !strings.Contains(output.String(), "event=tutti.agent_provider.codex.windows_cli_probe.failed") {
		t.Fatalf("Windows CLI probe failure log missing:\n%s", output.String())
	}
}

func TestProbeCodexWindowsCLIFailsWhenCommandMissing(t *testing.T) {
	evidence := Service{}.probeCodexWindowsCLI(context.Background(), nil, nil)
	if evidence.CommandStarted || evidence.ProtocolReady || evidence.Category != "spawn_failed" {
		t.Fatalf("evidence = %#v, want spawn_failed for a missing launcher", evidence)
	}
}

func TestProbeCodexAppServerUsesWindowsCLIInsteadOfDaemon(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows CLI probe is the production path only on Windows")
	}
	command := writeCodexWindowsCLIProbeFixture(t, "0.145.0", true)
	service := Service{
		ProbeTimeout:    3 * time.Second,
		ProbeReadyAfter: 100 * time.Millisecond,
	}
	evidence := service.probeCodexAppServer(
		context.Background(),
		[]string{command, "app-server"},
		[]string{"PATH=" + filepath.Dir(command)},
	)
	if !evidence.ProtocolReady {
		t.Fatalf("evidence = %#v, want Windows detection to accept codex --version", evidence)
	}
}

func writeCodexWindowsCLIProbeFixture(t *testing.T, version string, failAppServer bool) string {
	t.Helper()
	command := filepath.Join(t.TempDir(), "codex")
	if runtime.GOOS == "windows" {
		command += ".cmd"
		script := "@echo off\r\n"
		if version != "" {
			script += "if \"%~1\"==\"--version\" (\r\n  echo codex " + version + "\r\n  exit /b 0\r\n)\r\n"
		} else {
			script += "if \"%~1\"==\"--version\" (\r\n  echo not-a-version\r\n  exit /b 0\r\n)\r\n"
		}
		if failAppServer {
			script += "if \"%~1\"==\"app-server\" (\r\n  echo error: unrecognized subcommand app-server >&2\r\n  exit /b 2\r\n)\r\n"
		}
		script += "exit /b 1\r\n"
		writeExecutable(t, command, script)
		return command
	}
	script := "#!/bin/sh\n"
	if version != "" {
		script += "if [ \"$1\" = \"--version\" ]; then echo 'codex " + version + "'; exit 0; fi\n"
	} else {
		script += "if [ \"$1\" = \"--version\" ]; then echo 'not-a-version'; exit 0; fi\n"
	}
	if failAppServer {
		script += "if [ \"$1\" = \"app-server\" ]; then echo \"error: unrecognized subcommand 'app-server'\" >&2; exit 2; fi\n"
	}
	script += "exit 1\n"
	writeExecutable(t, command, script)
	return command
}
