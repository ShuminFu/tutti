package agentstatus

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
)

func (s Service) probeCodexAppServer(ctx context.Context, command, env []string) CodexProbeEvidence {
	if s.CodexProtocolProbe != nil {
		return s.CodexProtocolProbe(ctx, append([]string(nil), command...), append([]string(nil), env...))
	}
	// Official Codex on Windows rejects the app-server daemon. Availability
	// there is the CLI itself (`codex --version`); macOS/Linux keep the
	// formal initialize handshake.
	if useCodexWindowsCLIProbe() {
		return s.probeCodexWindowsCLI(ctx, command, env)
	}
	return s.probeCodexAppServerHandshake(ctx, command, env)
}

func useCodexWindowsCLIProbe() bool {
	return runtime.GOOS == "windows"
}

func (s Service) probeCodexWindowsCLI(ctx context.Context, command, env []string) CodexProbeEvidence {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return CodexProbeEvidence{
			Category: "spawn_failed",
			Message:  "codex command is unavailable",
		}
	}
	launcher := command[0]
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := s.probeTimeout()
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	startedAt := time.Now()
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := newInstallExecCommand(cmdCtx, launcher, "--version")
	if len(env) > 0 {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	diagnostic := truncateCodexProbeMessage(joinCodexProbeMessages(string(output), execErrorMessage(err)))
	if err != nil {
		evidence := CodexProbeEvidence{
			CommandStarted: commandStartedFromExecError(err),
			Category:       "spawn_failed",
			Message:        firstNonBlank(diagnostic, err.Error()),
		}
		if evidence.CommandStarted {
			evidence.Category = "cli_version_failed"
		}
		logCodexWindowsCLIProbeFailure(ctx, launcher, evidence, time.Since(startedAt))
		return evidence
	}
	if parseCLIVersion(string(output)) == "" {
		evidence := CodexProbeEvidence{
			CommandStarted: true,
			Category:       "cli_version_failed",
			Message:        firstNonBlank(diagnostic, "codex --version produced no version"),
		}
		logCodexWindowsCLIProbeFailure(ctx, launcher, evidence, time.Since(startedAt))
		return evidence
	}
	return CodexProbeEvidence{
		CommandStarted: true,
		ProtocolReady:  true,
	}
}

func commandStartedFromExecError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return false
	}
	return true
}

func execErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func logCodexWindowsCLIProbeFailure(ctx context.Context, launcher string, evidence CodexProbeEvidence, duration time.Duration) {
	failureStage := "version"
	if !evidence.CommandStarted {
		failureStage = "command"
	}
	slog.WarnContext(ctx,
		"codex windows CLI probe failed",
		"event", "tutti.agent_provider.codex.windows_cli_probe.failed",
		"launcherPath", launcher,
		"failureStage", failureStage,
		"commandStarted", evidence.CommandStarted,
		"protocolReady", evidence.ProtocolReady,
		"category", evidence.Category,
		"durationMs", duration.Milliseconds(),
		"diagnostic", evidence.Message,
	)
}

func (s Service) probeCodexAppServerHandshake(ctx context.Context, command, env []string) CodexProbeEvidence {
	result := agentruntime.ProbeCodexAppServer(ctx, agentruntime.CodexAppServerProbeInput{
		Command: command,
		Env:     env,
		Host: agentruntime.HostMetadata{ClientInfo: agentruntime.ClientInfo{
			Name: "tutti-desktop", Title: "Tutti", Version: "0.1.0",
		}},
		StartupTimeout: s.probeTimeout(), HandshakeTimeout: s.probeTimeout(), ShutdownTimeout: s.probeReadyAfter(),
	})
	diagnosticMessage := joinCodexProbeMessages(result.Message, result.StderrTail)
	commandCategory, commandPackage := codexProbeClassification(result.CommandCategory, diagnosticMessage)
	protocolCategory, protocolPackage := codexProbeClassification(result.ProtocolCategory, diagnosticMessage)
	category, platformPackage := protocolCategory, protocolPackage
	if !result.CommandStarted {
		category, platformPackage = commandCategory, commandPackage
	}
	if category == "" {
		category, platformPackage = codexProbeClassification(result.Category, diagnosticMessage)
	}
	evidence := CodexProbeEvidence{
		CommandStarted:      result.CommandStarted,
		ProtocolReady:       result.ProtocolReady,
		Category:            category,
		PlatformPackageName: platformPackage,
		Message:             truncateCodexProbeMessage(diagnosticMessage),
	}
	if !evidence.ProtocolReady {
		failureStage := "protocol"
		if !evidence.CommandStarted {
			failureStage = "command"
		}
		launcherPath := ""
		if len(command) > 0 {
			launcherPath = command[0]
		}
		slog.WarnContext(ctx,
			"codex app-server probe failed",
			"event", "tutti.agent_provider.codex.app_server_probe.failed",
			"launcherPath", launcherPath,
			"failureStage", failureStage,
			"commandStarted", evidence.CommandStarted,
			"protocolReady", evidence.ProtocolReady,
			"commandCategory", result.CommandCategory,
			"protocolCategory", result.ProtocolCategory,
			"category", evidence.Category,
			"durationMs", result.Duration.Milliseconds(),
			"diagnostic", evidence.Message,
		)
	}
	return evidence
}

func codexProbeClassification(category, message string) (string, string) {
	lower := strings.ToLower(message)
	if (strings.Contains(lower, "unknown command") || strings.Contains(lower, "unrecognized subcommand")) &&
		strings.Contains(lower, "app-server") {
		return "app_server_unsupported", ""
	}
	platform, ok := codexNpmPlatformDir(runtime.GOOS, runtime.GOARCH)
	if ok {
		needle := "@openai/" + platform
		missingPlatformDependency := strings.Contains(lower, "enoent") ||
			strings.Contains(lower, "missing optional dependency") ||
			strings.Contains(lower, "cannot find module") ||
			strings.Contains(lower, "could not find module")
		if missingPlatformDependency && strings.Contains(lower, strings.ToLower(needle)) {
			return "platform_package_enoent", needle
		}
	}
	return category, ""
}

func joinCodexProbeMessages(messages ...string) string {
	joined := make([]string, 0, len(messages))
	seen := map[string]struct{}{}
	for _, message := range messages {
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		if _, ok := seen[message]; ok {
			continue
		}
		seen[message] = struct{}{}
		joined = append(joined, message)
	}
	return strings.Join(joined, "\n")
}

func truncateCodexProbeMessage(message string) string {
	const limit = 1024
	message = strings.TrimSpace(message)
	if len(message) <= limit {
		return message
	}
	for len(message) > limit {
		message = message[:limit]
		for len(message) > 0 && !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	return message + " [truncated]"
}
