package agentstatus

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestServiceListDetectsProvidersConcurrently proves provider detection fans
// out instead of running serially: each provider's auth status command blocks
// until every requested provider has entered its own command. Serial detection
// would never let the second provider start, so the rendezvous would time out.
func TestServiceListLogsProviderDetectionTimings(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	service := testService(func(string) (string, error) {
		return "", errors.New("not found")
	}, map[string]bool{})
	if _, err := service.List(t.Context(), ListInput{Providers: []string{"codex"}}); err != nil {
		t.Fatalf("List() error = %v", err)
	}

	logOutput := output.String()
	for _, field := range []string{
		"event=tutti.agent_provider.status_detection.completed",
		"provider=codex",
		"durationMs=",
		"runtimeResolutionMs=",
		"adapterProbeMs=",
		"authMs=",
		"cliVersionMs=",
		"postChecksMs=",
		"event=tutti.agent_provider.status_list.completed",
		"requestedProviderCount=1",
		"providerCount=1",
		"success=true",
	} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("status timing log missing %q:\n%s", field, logOutput)
		}
	}
}
