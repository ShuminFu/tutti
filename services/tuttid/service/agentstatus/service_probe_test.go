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
