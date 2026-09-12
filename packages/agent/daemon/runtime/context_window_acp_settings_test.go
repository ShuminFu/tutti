package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/contextwindow"
)

// ACP has no context-window parameter, so the 1M marker must never reach the
// runtime: the live settings path has to strip it and switch to the bare model.
// Without that, a marked selection is simply not advertised and the switch is
// dropped without a word — the row looks selectable and does nothing.
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
