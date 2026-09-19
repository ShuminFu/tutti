package agentruntime

import (
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestClaudeSDKObservedModeUpdatesCanonicalSettings(t *testing.T) {
	adapter := NewClaudeCodeSDKAdapter(nil)
	session := standardTestSession(ProviderClaudeCode)
	session.PermissionModeID = "bypassPermissions"
	session.Settings = &SessionSettings{PermissionModeID: "bypassPermissions", Model: "sonnet"}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(session)
	live := &claudeSDKAdapterSession{liveState: newClaudeSDKLiveState()}
	for _, mode := range []string{"plan", "auto", "acceptEdits"} {
		events, terminal, err := adapter.sidecarTurnEvents(live, session, "", claudeSDKSidecarEvent{Type: "permission_mode_updated", Payload: map[string]any{"permissionMode": mode}})
		if err != nil || terminal || len(events) != 1 {
			t.Fatalf("mode %s: %v %v %#v", mode, err, terminal, events)
		}
		controller.applySessionEventsByAgentSessionID(session.AgentSessionID, events)
		current, ok := controller.Session(session.RoomID, session.AgentSessionID)
		if !ok || current.Settings.PlanMode != (mode == "plan") || current.Settings.Model != "sonnet" {
			t.Fatalf("mode %s: %#v", mode, current)
		}
		wantPermission := mode
		if mode == "plan" {
			wantPermission = "bypassPermissions"
		}
		if current.Settings.PermissionModeID != wantPermission {
			t.Fatalf("mode %s changed permission to %s", mode, current.Settings.PermissionModeID)
		}
		if context := claudeSDKRuntimeContext(session, live); context["planMode"] != (mode == "plan") {
			t.Fatalf("stale snapshot: %#v", context)
		}
	}
}

func TestClaudeSDKDetachedProcessUpdatesOriginalSettledTurn(t *testing.T) {
	adapter := NewClaudeCodeSDKAdapter(nil)
	session := standardTestSession(ProviderClaudeCode)
	live := &claudeSDKAdapterSession{rootTurnID: "new-turn", settledTurns: map[string]string{"old-turn": "completed"}}
	for status, wantEvent := range map[string]activityshared.EventType{
		"running":   activityshared.EventCallStarted,
		"failed":    activityshared.EventCallFailed,
		"completed": activityshared.EventCallCompleted,
		"stopped":   activityshared.EventCallFailed,
	} {
		events, terminal, err := adapter.sidecarTurnEvents(live, session, "old-turn", claudeSDKSidecarEvent{Type: "background_process_updated", Payload: map[string]any{
			"turnId": "old-turn", "toolCallId": "background:process-1", "name": "Compile", "callType": "command", "status": status,
			"output": map[string]any{"text": "compiler output", "status": status},
		}})
		if err != nil || terminal || len(events) != 1 {
			t.Fatalf("%s: %v %v %#v", status, err, terminal, events)
		}
		event := events[0]
		if event.Payload.TurnID != "old-turn" || event.Payload.CallID != "background:process-1" || event.Payload.TurnPhase != "" || event.Payload.TurnOutcome != "" {
			t.Fatalf("background changed owner/lifecycle: %#v", event)
		}
		if event.Type != wantEvent {
			t.Fatalf("status %s: event type = %s, want %s", status, event.Type, wantEvent)
		}
	}
	if live.rootTurnID != "new-turn" {
		t.Fatalf("root changed: %#v", live)
	}
}
