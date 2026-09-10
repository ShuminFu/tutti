package agentstatus

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestExternalAdapterProbeExecutesCommandNotPackageDirectory(t *testing.T) {
	root := t.TempDir()
	command := filepath.Join(root, "adapter")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(root, "node_modules", "adapter-package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	service := Service{ProbeReadyAfter: 10 * time.Millisecond, ProbeTimeout: time.Second}
	result := service.probeAdapterRuntimeCommand(context.Background(), ProviderSpec{
		Provider: "claude-code",
	}, providerRuntimeResolution{
		AdapterPath:    packageDir,
		AdapterCommand: []string{command},
		Env:            os.Environ(),
	}, time.Now())
	if result.Status != ProbeReady {
		t.Fatalf("result = %#v, want command probe ready", result)
	}
	if result.Command[0] != command {
		t.Fatalf("command = %#v, package directory must not replace launcher", result.Command)
	}
}

func TestExternalClaudeRuntimeUsesHostSelectedCLIPath(t *testing.T) {
	preferred := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(preferred, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_EXECUTABLE", preferred)
	service := Service{}
	resolved := service.resolveExternalProviderRuntime(context.Background(), ProviderSpec{Provider: "claude-code"}, service.commandResolver(), os.Environ())
	if resolved.CLIPath != preferred {
		t.Fatalf("CLIPath = %q, want host-selected %q", resolved.CLIPath, preferred)
	}
}

func TestClaudeACPProbeGetsColdStartBudget(t *testing.T) {
	service := Service{ProbeTimeout: 3 * time.Second}
	timeout := service.probeTimeoutForSpec(ProviderSpec{Provider: "claude-code", ExternalRegistryID: "claude-acp"})
	if timeout != 15*time.Second {
		t.Fatalf("timeout = %s, want 15s", timeout)
	}
}

func TestGatewayFailureOverridesOnlySuccessfulACPProbe(t *testing.T) {
	status := ProviderStatus{Availability: Availability{ReasonCode: "gateway_auth_unavailable"}}
	failed := applyHostGatewayProbeStatus(ProbeResult{Status: ProbeReady}, status)
	if failed.Status != ProbeFailed || failed.ReasonCode != "gateway_auth_unavailable" {
		t.Fatalf("failed = %#v", failed)
	}
	original := ProbeResult{Status: ProbeFailed, ReasonCode: "acp_adapter_launch_failed"}
	if got := applyHostGatewayProbeStatus(original, status); got.Status != original.Status || got.ReasonCode != original.ReasonCode {
		t.Fatalf("runtime failure was overwritten: %#v", got)
	}
}

func TestCodexProbeUsesDetectionCommandLimiter(t *testing.T) {
	limiter := NewDetectionCommandLimiter(1)
	release, acquired := limiter.acquire(context.Background())
	if !acquired {
		t.Fatal("failed to occupy detection command limiter")
	}
	defer release()

	var calls atomic.Int32
	service := Service{
		DetectionCommands: limiter,
		CodexProtocolProbe: func(context.Context, []string, []string) CodexProbeEvidence {
			calls.Add(1)
			return CodexProbeEvidence{CommandStarted: true, ProtocolReady: true}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := service.probeAdapterRuntimeCommand(ctx, ProviderSpec{
		Provider:       "codex",
		BinaryNames:    []string{"codex"},
		AdapterCommand: []string{"codex", "app-server"},
	}, providerRuntimeResolution{
		CLIPath:        "/tmp/codex",
		AdapterPath:    "/tmp/codex",
		AdapterCommand: []string{"codex", "app-server"},
	}, time.Now())

	if result.Status != ProbeFailed || result.ReasonCode != "probe_canceled" {
		t.Fatalf("result = %#v, want probe_canceled while limiter is occupied", result)
	}
	if calls.Load() != 0 {
		t.Fatalf("CodexProtocolProbe calls = %d, want 0 before limiter acquisition", calls.Load())
	}
}
