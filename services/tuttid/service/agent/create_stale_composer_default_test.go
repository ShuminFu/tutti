package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
)

// A default saved while claude-code ran through the company gateway
// (gpt-5.6-luna) outlives the switch back to a personal subscription. The
// client echoes that default back as the create model, so every new
// claude-code session failed with `invalid model "gpt-5.6-luna"`. A stale
// stored default must fall back to the account's default model; an explicit
// invalid pick that is not the stored default must still be refused.
func newStaleDefaultClaudeService(t *testing.T, storedModel string) (*fakeRuntime, *Service) {
	t.Helper()
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, "")
	t.Setenv(runtimeprep.HostModelEndpointsEnv, "")
	runtime := newFakeRuntime()
	service := newTestService(runtime)
	personal := []ComposerConfigOptionValue{
		{ID: "default", Label: "Default", Value: "default"},
		{ID: "opus", Label: "Opus", Value: "opus"},
	}
	now := time.Now().UTC()
	for _, target := range []string{"", agenttargetbiz.IDLocalClaudeCode} {
		service.setLiveComposerModelOptionsForScope(
			newComposerLiveModelScope("claude-code", "workspace-stale", "", target), now, personal)
	}
	service.AgentComposerDefaultsReader = fakeAgentComposerDefaultsReader{
		agenttargetbiz.IDLocalClaudeCode: {Model: storedModel},
	}
	return runtime, service
}

func TestServiceCreateFallsBackFromStaleStoredDefaultModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model *string
	}{
		{name: "server fills stored default", model: nil},
		{name: "client echoes stored default", model: stringPointer("gpt-5.6-luna")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, service := newStaleDefaultClaudeService(t, "gpt-5.6-luna")
			if _, err := service.Create(context.Background(), "workspace-stale", CreateSessionInput{
				AgentTargetID: agenttargetbiz.IDLocalClaudeCode,
				Cwd:           stringPointer(t.TempDir()),
				Model:         tc.model,
			}); err != nil {
				t.Fatalf("Create() error = %v, want fallback from stale default", err)
			}
			for _, start := range visibleRuntimeStarts(runtime.startCalls) {
				if start.Model == "gpt-5.6-luna" {
					t.Fatalf("runtime started with stale default %q", start.Model)
				}
			}
		})
	}
}

func TestServiceCreateStillRefusesExplicitInvalidModel(t *testing.T) {
	_, service := newStaleDefaultClaudeService(t, "opus")
	_, err := service.Create(context.Background(), "workspace-stale", CreateSessionInput{
		AgentTargetID: agenttargetbiz.IDLocalClaudeCode,
		Cwd:           stringPointer(t.TempDir()),
		Model:         stringPointer("gpt-5.6-luna"),
	})
	var invalid *InvalidModelError
	if !errors.As(err, &invalid) {
		t.Fatalf("Create() error = %v, want InvalidModelError", err)
	}
}

// The other direction: a personal-subscription default (opus) after switching
// the target to a company gateway that does not list it.
func TestServiceCreateFallsBackFromStaleDefaultUnderGatewayOverlay(t *testing.T) {
	runtime, service := newStaleDefaultClaudeService(t, "opus")
	payload := `{"version":1,"providers":{"claude-code":{"planName":"DinTal Runtime LLM Proxy","protocol":"anthropic","baseURL":"http://127.0.0.1:18799/llmproxy/anthropic","apiKey":"loopback","model":"gpt-5.6-luna","models":[{"id":"gpt-5.6-luna","name":"gpt-5.6-luna"},{"id":"deepseek-v4-pro","name":"deepseek-v4-pro"}]}}}`
	path := filepath.Join(t.TempDir(), "host-model-endpoints.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeprep.HostModelEndpointsFileEnv, path)
	if _, err := service.Create(context.Background(), "workspace-stale", CreateSessionInput{
		AgentTargetID: agenttargetbiz.IDLocalClaudeCode,
		Cwd:           stringPointer(t.TempDir()),
		Model:         stringPointer("opus"),
	}); err != nil {
		t.Fatalf("Create() error = %v, want fallback to gateway default", err)
	}
	for _, start := range visibleRuntimeStarts(runtime.startCalls) {
		if start.Model == "opus" {
			t.Fatalf("runtime started with stale default %q", start.Model)
		}
	}
}
