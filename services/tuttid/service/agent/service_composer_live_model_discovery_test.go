package agent

import (
	"context"
	"slices"
	"testing"
)

func TestServiceGetsComposerOptionsLeavesUnresolvedProviderModelUnset(t *testing.T) {
	runtime := newFakeRuntime()
	service := newIsolatedAgentService(runtime)

	options, err := service.GetComposerOptions(context.Background(), ComposerOptionsInput{
		Provider: "openclaw",
	})
	if err != nil {
		t.Fatalf("GetComposerOptions returned error: %v", err)
	}
	if options.EffectiveSettings.Model != "" {
		t.Fatalf("effectiveSettings.model = %q, want empty", options.EffectiveSettings.Model)
	}
	if options.EffectiveSettings.ReasoningEffort != "" {
		t.Fatalf("effectiveSettings.reasoningEffort = %q, want empty", options.EffectiveSettings.ReasoningEffort)
	}
	if capabilities, ok := options.RuntimeContext["capabilities"].([]string); ok &&
		slices.Contains(capabilities, "imageInput") {
		t.Fatalf("capabilities = %#v, want no imageInput", options.RuntimeContext["capabilities"])
	}
}

func TestServiceUpdateSettingsPreservesCodexModelCatalogReasoningEffort(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.sessions["ws-1:session-1"] = ProviderRuntimeSession{
		ID:          "session-1",
		Provider:    "codex",
		WorkspaceID: "ws-1",
		Status:      "working",
		Settings: &ComposerSettings{
			ReasoningEffort: "high",
		},
	}
	service := newIsolatedAgentService(runtime)
	seedPersistedLiveSettingsSession(service, runtime.sessions["ws-1:session-1"])
	reasoningEffort := "minimal"

	session, err := service.UpdateSettings(context.Background(), "ws-1", "session-1", ComposerSettingsPatch{
		ReasoningEffort: &reasoningEffort,
	})
	if err != nil {
		t.Fatalf("UpdateSettings returned error: %v", err)
	}
	if session.Settings == nil || session.Settings.ReasoningEffort != "minimal" {
		t.Fatalf("session settings = %#v, want reasoningEffort minimal", session.Settings)
	}
}

func TestServiceUpdateSettingsPreservesAdvertisedReasoningEffort(t *testing.T) {
	for _, effort := range []string{"minimal", "none"} {
		t.Run(effort, func(t *testing.T) {
			runtime := newFakeRuntime()
			runtime.sessions["ws-1:session-1"] = ProviderRuntimeSession{
				ID:          "session-1",
				Provider:    "codex",
				WorkspaceID: "ws-1",
				Status:      "working",
				Settings: &ComposerSettings{
					Model:           "gpt-catalog",
					ReasoningEffort: "high",
				},
			}
			service := NewService(runtime)
			seedPersistedLiveSettingsSession(service, runtime.sessions["ws-1:session-1"])
			service.ModelCatalog = fakeModelCatalog{
				result: AgentModelCatalogResult{
					Provider: "codex",
					Source:   "codex-cli",
					Models: []AgentModelOption{{
						ID:                         "gpt-catalog",
						DefaultReasoningEffort:     "high",
						ReasoningEffortsAdvertised: true,
						SupportedReasoningEfforts: []AgentModelReasoningEffortOption{
							{Value: "minimal"}, {Value: "none"}, {Value: "high"},
						},
					}},
				},
			}

			configureTestApplicationHost(service)
			session, err := service.UpdateSettings(context.Background(), "ws-1", "session-1", ComposerSettingsPatch{
				ReasoningEffort: stringRef(effort),
			})
			if err != nil {
				t.Fatalf("UpdateSettings returned error: %v", err)
			}
			if session.Settings == nil || session.Settings.ReasoningEffort != effort {
				t.Fatalf("session settings = %#v, want reasoning %q", session.Settings, effort)
			}
		})
	}
}

func TestServiceUpdateSettingsDefersModelChangeReasoningClampToLiveRuntime(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.sessions["ws-1:session-1"] = ProviderRuntimeSession{
		ID:          "session-1",
		Provider:    "codex",
		WorkspaceID: "ws-1",
		Status:      "ready",
		Settings: &ComposerSettings{
			Model:           "gpt-5.6-sol",
			ReasoningEffort: "ultra",
		},
	}
	service := NewService(runtime)
	seedPersistedLiveSettingsSession(service, runtime.sessions["ws-1:session-1"])
	service.ModelCatalog = fakeModelCatalog{
		result: AgentModelCatalogResult{
			Provider: "codex",
			Source:   "codex-cli",
			Models: []AgentModelOption{
				{
					ID:                         "gpt-5.6-sol",
					DefaultReasoningEffort:     "high",
					ReasoningEffortsAdvertised: true,
					SupportedReasoningEfforts: []AgentModelReasoningEffortOption{
						{Value: "low"}, {Value: "medium"}, {Value: "high"},
						{Value: "xhigh"}, {Value: "max"}, {Value: "ultra"},
					},
				},
				{
					ID:                         "gpt-5.6-luna",
					DefaultReasoningEffort:     "high",
					ReasoningEffortsAdvertised: true,
					SupportedReasoningEfforts: []AgentModelReasoningEffortOption{
						{Value: "low"}, {Value: "medium"}, {Value: "high"},
						{Value: "xhigh"}, {Value: "max"},
					},
				},
			},
		},
	}
	configureTestApplicationHost(service)
	model := "gpt-5.6-luna"

	session, err := service.UpdateSettings(context.Background(), "ws-1", "session-1", ComposerSettingsPatch{
		Model: &model,
	})
	if err != nil {
		t.Fatalf("UpdateSettings returned error: %v", err)
	}
	// The daemon-side catalog intentionally lacks Luna/ultra, while a live
	// target may advertise it. The service must not downgrade before the live
	// adapter gets a chance to resolve its fresher model/list snapshot.
	if session.Settings == nil || session.Settings.Model != model || session.Settings.ReasoningEffort != "ultra" {
		t.Fatalf("session settings = %#v, want unclamped Luna/ultra runtime input", session.Settings)
	}
}

func TestServiceUpdateSettingsNormalizesClaudeMinimalReasoningEffort(t *testing.T) {
	runtime := newFakeRuntime()
	runtime.sessions["ws-1:session-1"] = ProviderRuntimeSession{
		ID:          "session-1",
		Provider:    "claude-code",
		WorkspaceID: "ws-1",
		Status:      "working",
		Settings: &ComposerSettings{
			ReasoningEffort: "high",
		},
	}
	service := newIsolatedAgentService(runtime)
	seedPersistedLiveSettingsSession(service, runtime.sessions["ws-1:session-1"])
	reasoningEffort := "minimal"

	session, err := service.UpdateSettings(context.Background(), "ws-1", "session-1", ComposerSettingsPatch{
		ReasoningEffort: &reasoningEffort,
	})
	if err != nil {
		t.Fatalf("UpdateSettings returned error: %v", err)
	}
	if session.Settings == nil || session.Settings.ReasoningEffort != "high" {
		t.Fatalf("session settings = %#v, want reasoningEffort high", session.Settings)
	}
}
