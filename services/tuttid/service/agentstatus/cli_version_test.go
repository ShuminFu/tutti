package agentstatus

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCLIVersion(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{name: "codex format", output: "codex-cli 0.142.1", want: "0.142.1"},
		{name: "codex test fake", output: "codex 0.100.0", want: "0.100.0"},
		{
			name:   "claude parenthesized suffix",
			output: "2.1.191 (Claude Code)",
			want:   "2.1.191",
		},
		{name: "leading v tolerated", output: "v1.2.3", want: "1.2.3"},
		{name: "prerelease suffix", output: "tool 1.2.3-beta.1", want: "1.2.3-beta.1"},
		{name: "two component", output: "1.4", want: "1.4"},
		{name: "trailing newline", output: "0.142.1\n", want: "0.142.1"},
		{name: "no version token", output: "Claude Code", want: ""},
		{name: "empty", output: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCLIVersion(tc.output); got != tc.want {
				t.Fatalf("parseCLIVersion(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func TestCLIVersionHonorsContextCancellation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "slow-cli")
	writeExecutable(t, binary, "#!/bin/sh\nexec sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	if got := (Service{}).cliVersion(ctx, binary, nil); got != "" {
		t.Fatalf("cliVersion() = %q, want unknown version", got)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("cliVersion() ignored context cancellation; elapsed = %s", elapsed)
	}
}

func TestProviderCLIVersionLogsFailedCommandEvidence(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "claude")
	writeExecutable(t, binary, "#!/bin/sh\necho '2.1.231 (Claude Code)'\nexit 7\n")
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	got := (Service{}).providerCLIVersion(context.Background(), ProviderSpec{Provider: "claude-code"}, binary, nil)
	if got != "" {
		t.Fatalf("providerCLIVersion() = %q, want rejected output", got)
	}
	logLine := output.String()
	for _, evidence := range []string{
		"event=tutti.agent_provider.cli_version_probe.failed",
		"provider=claude-code",
		"binaryPath=" + binary,
		"exitCode=7",
		`output="2.1.231 (Claude Code)"`,
	} {
		if !strings.Contains(logLine, evidence) {
			t.Fatalf("version failure log missing %q:\n%s", evidence, logLine)
		}
	}
}
