package agentstatus

import (
	"testing"
	"time"
)

func TestHostGatewayStatusOwnsAuthAndLoginActions(t *testing.T) {
	t.Setenv("TUTTI_HOST_MODEL_ENDPOINTS_FILE", "")
	base := ProviderStatus{
		CLI:          CLIStatus{Installed: true},
		Adapter:      AdapterStatus{Installed: true},
		Availability: Availability{Status: AvailabilityAuthRequired},
		Actions:      []Action{{ID: ActionLogin}, {ID: ActionRefresh, Kind: ActionKindRefresh}},
	}
	spec := ProviderSpec{Provider: "claude-code"}
	now := time.Now()

	t.Setenv("TUTTI_HOST_MODEL_ENDPOINTS", `{"version":1,"routes":{"claude-code":{"mode":"gateway","status":"gateway_config_missing"}},"providers":{}}`)
	blocked := applyHostGatewayStatus(base, spec, now)
	if blocked.Availability.Status != AvailabilityUnknown || blocked.Availability.ReasonCode != "gateway_config_missing" || hasProviderAction(blocked.Actions, ActionLogin) {
		t.Fatalf("blocked = %#v", blocked)
	}

	t.Setenv("TUTTI_HOST_MODEL_ENDPOINTS", `{"version":1,"routes":{"claude-code":{"mode":"gateway","status":"ready"}},"providers":{}}`)
	ready := applyHostGatewayStatus(base, spec, now)
	if ready.Availability.Status != AvailabilityReady || ready.Auth.Status != AuthAuthenticated || ready.Auth.AuthMethod != "gateway" || hasProviderAction(ready.Actions, ActionLogin) {
		t.Fatalf("ready = %#v", ready)
	}
}

func TestPostInstallGatewayFailureKeepsSpecificReason(t *testing.T) {
	for _, reason := range []string{"gateway_config_missing", "gateway_auth_unavailable"} {
		if got := postInstallProbeFailureReason(ProbeResult{ReasonCode: reason}); got != reason {
			t.Fatalf("reason = %q, got %q", reason, got)
		}
	}
	if got := postInstallProbeFailureReason(ProbeResult{ReasonCode: "acp_adapter_launch_failed"}); got != "post_install_probe_failed" {
		t.Fatalf("ordinary probe reason = %q", got)
	}
}
