package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

// Whether the `[1m]` context-window marker may reach the runtime is a
// per-provider property, and both halves have to be pinned here:
//
//   - A standard ACP runtime has no context-window parameter, so the marker must
//     come off before the lookup and before the request: the live settings path
//     has to strip it and switch to the bare model. Without that, a marked
//     selection is simply not advertised and the switch is dropped without a
//     word — the row looks selectable and does nothing.
//   - Claude Code is the exception: its ACP adapter reads the window request off
//     the model value, so stripping the marker is exactly the "DinTalDock cc
//     falls back to 200k" defect — the session keeps `X[1m]` while the runtime is
//     told `X`, and every switch silently downgrades the window. See
//     standardACPConfig.modelValueCarriesContextWindow.
//
// Both directions are asserted on the same two paths (session start and live
// switch) so neither can drift into the other's spelling.
func TestExtensionACPLiveModelSwitchStripsTheContextWindowMarker(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-1m-live")
	adapter := newExtensionRuntimeContractTestAdapter(transport)
	session := extensionRuntimeContractTestSession()
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The runtime advertises bare ids; start the session on a different one so the
	// switch is a real change the adapter has to send.
	session.Settings = &SessionSettings{Model: "default"}
	if err := adapter.ApplySessionSettings(context.Background(), session, SessionSettingsPatch{
		Model: stringPtr(contextwindow.WithMarker(extensionRuntimeContractModel)),
	}); err != nil {
		t.Fatalf("ApplySessionSettings: %v", err)
	}

	calls := transport.conn.setConfigOptionCalls()
	if len(calls) != 1 {
		t.Fatalf("config option calls = %#v, want the model switch to be sent", calls)
	}
	if got, _ := calls[0]["value"].(string); got != extensionRuntimeContractModel {
		t.Fatalf("config value = %q, want the bare %q", got, extensionRuntimeContractModel)
	}
}

// The extension path validates before it applies, so the same stripping has to
// happen there — otherwise the bare id never matches and the patch is rejected
// with our internal spelling echoed back at the user.
func TestExtensionACPModelValidationAcceptsAMarkedModel(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-1m-validate")
	adapter := newExtensionRuntimeContractTestAdapter(transport)
	session := extensionRuntimeContractTestSession()
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.Settings = &SessionSettings{Model: "default"}

	if err := adapter.ValidateSessionSettings(session, SessionSettingsPatch{
		Model: stringPtr(contextwindow.WithMarker(extensionRuntimeContractModel)),
	}); err != nil {
		t.Fatalf("ValidateSessionSettings(marked) = %v, want the bare model accepted", err)
	}

	unknown := "deepseek-not-advertised"
	err := adapter.ValidateSessionSettings(session, SessionSettingsPatch{
		Model: stringPtr(contextwindow.WithMarker(unknown)),
	})
	if err == nil {
		t.Fatal("ValidateSessionSettings(unknown) = nil, want it rejected")
	}
	// The message names the model the runtime knows, not our marker spelling.
	if !strings.Contains(err.Error(), unknown) || strings.Contains(err.Error(), "[1m]") {
		t.Fatalf("error = %q, want the bare model named without the marker", err)
	}
}

// The two models the fake claude-agent-acp advertises in both lanes. They stand
// in for the managed availableModels allowlist that
// tuttiruntime.expandManagedModelAllowlist writes (bare id plus its `[1m]`
// variant) — the marked entry is what makes the marked lane selectable at all,
// and without it the runtime rejects or fuzzes the value back to bare.
const (
	claudeACPContextWindowModel       = "deepseek-v4-pro"
	claudeACPContextWindowSwitchModel = "claude-opus-4-8"
)

// claudeACPContextWindowTestAdapter builds the adapter production builds for
// Claude Code under TUTTI_CLAUDE_CODE_RUNTIME=acp: the real migrated descriptor
// and the real dispatch, so this pins configuration rather than a hand-built
// adapter that could disagree with it.
func claudeACPContextWindowTestAdapter(t *testing.T, transport *standardACPTransport) *standardACPAdapter {
	t.Helper()
	t.Setenv(claudeCodeRuntimeEnv, claudeCodeRuntimeACP)

	descriptor, ok := providerregistry.Find(providerregistry.ClaudeCodeProviderID)
	if !ok {
		t.Fatalf("provider registry has no %q descriptor", providerregistry.ClaudeCodeProviderID)
	}
	options := []any{map[string]any{"value": "default", "name": "Auto"}}
	for _, id := range []string{claudeACPContextWindowModel, claudeACPContextWindowSwitchModel} {
		for _, value := range []string{id, contextwindow.WithMarker(id)} {
			options = append(options, map[string]any{"value": value, "name": value})
		}
	}
	transport.conn.configOptions = []map[string]any{{
		"id":           "model",
		"name":         "Model",
		"category":     "model",
		"type":         "select",
		"currentValue": "default",
		"options":      options,
	}}

	adapter := newAdapterFromProviderDescriptor(
		descriptor, transport, LegacyHostMetadata(), nil, providerAdapterOptions{},
	)
	standardACPAdapter, ok := adapter.(*standardACPAdapter)
	if !ok {
		t.Fatalf("claude ACP dispatch produced %T, want the standard ACP adapter", adapter)
	}
	// The switch itself, before any behaviour is read out of it: without this the
	// tests below could pass for a runtime that never carries the window.
	if !standardACPAdapter.config.modelValueCarriesContextWindow {
		t.Fatalf("claude ACP adapter must carry the context window on the model value")
	}
	return standardACPAdapter
}

func claudeACPContextWindowTestSession() Session {
	session := standardTestSession(providerregistry.ClaudeCodeProviderID)
	session.ProviderSessionID = ""
	return session
}

// Claude Code reads the window off the model value, so the very first request
// has to carry the marker. This is the startup half of the fallback: the
// session row and the composer both hold `X[1m]`, and if the launch hands the
// runtime `X` the session runs at 200k while everything on screen says 1M.
func TestClaudeACPStartKeepsTheContextWindowMarker(t *testing.T) {
	transport := newStandardACPTransport("Claude Code", "claude-session-1m-start")
	adapter := claudeACPContextWindowTestAdapter(t, transport)
	session := claudeACPContextWindowTestSession()
	// Start on the marked lane while the runtime is still on its default, so the
	// marker has to be sent for real rather than matching what is already there.
	session.Settings = &SessionSettings{Model: contextwindow.WithMarker(claudeACPContextWindowModel)}

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}

	calls := transport.conn.setConfigOptionCalls()
	if len(calls) != 1 {
		t.Fatalf("config option calls = %#v, want exactly the model switch", calls)
	}
	want := contextwindow.WithMarker(claudeACPContextWindowModel)
	if got, _ := calls[0]["value"].(string); got != want {
		t.Fatalf("config value = %q, want the marked %q", got, want)
	}
}

// The live switch has to keep the marker too — and it has to keep the *bare*
// lane bare. Forcing the marker on would collapse the two lanes and silently
// upgrade a user who picked 200k, which is the mirror image of the defect.
func TestClaudeACPLiveModelSwitchKeepsTheContextWindowLane(t *testing.T) {
	transport := newStandardACPTransport("Claude Code", "claude-session-1m-switch")
	adapter := claudeACPContextWindowTestAdapter(t, transport)
	session := claudeACPContextWindowTestSession()
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.Settings = &SessionSettings{Model: claudeACPContextWindowModel}

	// 1M lane: the marker rides the value.
	if err := adapter.ApplySessionSettings(context.Background(), session, SessionSettingsPatch{
		Model: stringPtr(contextwindow.WithMarker(claudeACPContextWindowSwitchModel)),
	}); err != nil {
		t.Fatalf("ApplySessionSettings(marked): %v", err)
	}
	calls := transport.conn.setConfigOptionCalls()
	if len(calls) != 1 {
		t.Fatalf("config option calls = %#v, want the marked switch sent", calls)
	}
	if got, _ := calls[0]["value"].(string); got != contextwindow.WithMarker(claudeACPContextWindowSwitchModel) {
		t.Fatalf("config value = %q, want the marked %q",
			got, contextwindow.WithMarker(claudeACPContextWindowSwitchModel))
	}

	// Bare lane: no marker is added, and switching back has to reach the runtime.
	session.Settings = &SessionSettings{Model: contextwindow.WithMarker(claudeACPContextWindowSwitchModel)}
	if err := adapter.ApplySessionSettings(context.Background(), session, SessionSettingsPatch{
		Model: stringPtr(claudeACPContextWindowModel),
	}); err != nil {
		t.Fatalf("ApplySessionSettings(bare): %v", err)
	}
	calls = transport.conn.setConfigOptionCalls()
	if len(calls) != 2 {
		t.Fatalf("config option calls = %#v, want the bare switch sent", calls)
	}
	if got, _ := calls[1]["value"].(string); got != claudeACPContextWindowModel {
		t.Fatalf("config value = %q, want the bare %q (the lanes must not collapse)",
			got, claudeACPContextWindowModel)
	}
}

// The same startup path on a runtime with no context-window channel still has
// to strip the marker — the per-provider switch is what decides, not the call
// site. This is the pair to TestClaudeACPStartKeepsTheContextWindowMarker.
func TestExtensionACPStartStripsTheContextWindowMarker(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-1m-start")
	adapter := newExtensionRuntimeContractTestAdapter(transport)
	session := extensionRuntimeContractTestSession()
	session.Settings = &SessionSettings{Model: contextwindow.WithMarker(extensionRuntimeContractModel)}

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}

	calls := transport.conn.setConfigOptionCalls()
	if len(calls) != 1 {
		t.Fatalf("config option calls = %#v, want exactly the model switch", calls)
	}
	if got, _ := calls[0]["value"].(string); got != extensionRuntimeContractModel {
		t.Fatalf("config value = %q, want the bare %q", got, extensionRuntimeContractModel)
	}
}
