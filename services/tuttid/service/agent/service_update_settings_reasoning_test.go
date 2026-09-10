package agent

import (
	"context"
	"testing"
)

// TestServiceUpdateSettingsKeepsAdvertisedReasoningForUnprofiledProvider 钉住
// resume 的 502：扩展自带的 provider（acp:grok）不在 providerregistry 里，查静态
// composer 表得到全零 profile，旧代码据此把 "high" 压成 ""，下游 ACP 校验器再拒
// 掉 "" → `agent session ACP reasoning value "" is not advertised`，该会话永久
// 发不出去。这里断言送到 runtime 的补丁里 reasoningEffort 仍是 "high"。
// 去掉修复（clampSkipsOpenProviderIdentities = false）即红。
func TestServiceUpdateSettingsKeepsAdvertisedReasoningForUnprofiledProvider(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		want     string
	}{
		// 扩展 provider：本层不该有意见，原样放行给 ACP 校验器判。
		{name: "extension acp provider passes through", provider: "acp:grok", want: "high"},
		{name: "bare extension provider passes through", provider: "grok", want: "high"},
		// 表里认识的 provider 走原逻辑，行为不变。
		{name: "profiled provider keeps clamping", provider: "claude-code", want: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newFakeRuntime()
			settings := ComposerSettings{Model: "grok-4.6", ReasoningEffort: "medium"}
			runtime.sessions["ws-1:session-1"] = ProviderRuntimeSession{
				ID: "session-1", WorkspaceID: "ws-1", Provider: tc.provider, Settings: &settings,
			}
			service := newTestService(runtime)
			service.SessionReader = &settingsUpdateSessionReader{
				fakeSessionReader: &fakeSessionReader{sessions: map[string]PersistedSession{
					"ws-1:session-1": {
						ID: "session-1", WorkspaceID: "ws-1", Provider: tc.provider,
						ProviderSessionID: "provider-session-1", Cwd: "/workspace",
						RailSectionKey:    "conversations",
						Settings: settings, CreatedAtUnixMS: 100, UpdatedAtUnixMS: 200,
						LastEventUnixMS: 200,
					},
				}},
				updatedAtUnixMS: 300,
			}

			reasoning := "high"
			if _, err := service.UpdateSettings(context.Background(), "ws-1", "session-1",
				ComposerSettingsPatch{ReasoningEffort: &reasoning}); err != nil {
				t.Fatalf("UpdateSettings returned error: %v", err)
			}
			if len(runtime.updateSettingsCalls) != 1 {
				t.Fatalf("runtime UpdateSettings calls = %d, want 1", len(runtime.updateSettingsCalls))
			}
			got := runtime.updateSettingsCalls[0].Settings.ReasoningEffort
			if got == nil {
				t.Fatal("runtime patch dropped reasoningEffort entirely")
			}
			if *got != tc.want {
				t.Fatalf("reasoningEffort delivered to runtime = %q, want %q", *got, tc.want)
			}
		})
	}
}
