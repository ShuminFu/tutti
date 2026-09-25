package agentruntime

import (
	"context"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestUserCodexHomeReleasesWriterWhenTurnIdles(t *testing.T) {
	t.Parallel()

	controller, adapter, transport, started := startCodexWriterController(t, true)
	transport.server.holdTurn = true
	if _, err := controller.Exec(context.Background(), ExecInput{
		RoomID: started.RoomID, AgentSessionID: started.AgentSessionID,
		Content: textPrompt("hello"), RequireProviderAcceptance: true,
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurnID(started.AgentSessionID) == "turn-1"
	})
	if !adapter.HasLiveSession(started) {
		t.Fatal("app-server closed while the turn was still running")
	}
	transport.server.completePendingTurn()
	waitForCondition(t, func() bool {
		return !adapter.HasLiveSession(started)
	})
}

func TestUserCodexHomeKeepsWriterWhileGoalActive(t *testing.T) {
	t.Parallel()

	controller, adapter, transport, started := startCodexWriterController(t, true)
	adapter.goalContinuationGraceWindow = time.Hour
	transport.server.holdTurn = true
	if _, err := controller.Exec(context.Background(), ExecInput{
		RoomID: started.RoomID, AgentSessionID: started.AgentSessionID,
		Content: textPrompt("hello"), RequireProviderAcceptance: true,
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurnID(started.AgentSessionID) == "turn-1"
	})
	adapter.mu.Lock()
	adapter.sessions[started.AgentSessionID].goal = map[string]any{"status": "active"}
	adapter.mu.Unlock()
	transport.server.completePendingTurn()
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurnID(started.AgentSessionID) == ""
	})
	if !adapter.HasLiveSession(started) {
		t.Fatal("app-server closed while the goal was still active")
	}
	adapter.mu.Lock()
	adapter.sessions[started.AgentSessionID].goal = map[string]any{"status": "complete"}
	adapter.mu.Unlock()
	adapter.releaseIdleUserCodexWriter(context.Background(), started)
	if adapter.HasLiveSession(started) {
		t.Fatal("app-server still held the writer after the goal settled")
	}
}

func TestIsolatedCodexHomeKeepsAppServerAfterTurn(t *testing.T) {
	t.Parallel()

	controller, adapter, transport, started := startCodexWriterController(t, false)
	transport.server.holdTurn = true
	if _, err := controller.Exec(context.Background(), ExecInput{
		RoomID: started.RoomID, AgentSessionID: started.AgentSessionID,
		Content: textPrompt("hello"),
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurnID(started.AgentSessionID) == "turn-1"
	})
	transport.server.completePendingTurn()
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurnID(started.AgentSessionID) == ""
	})
	if !adapter.HasLiveSession(started) {
		t.Fatal("isolated app-server closed after the turn")
	}
}

func startCodexWriterController(t *testing.T, userHome bool) (*Controller, *CodexAppServerAdapter, *scriptedAppServerTransport, Session) {
	t.Helper()
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	controller := NewController([]Adapter{adapter}, &recordingReporter{})
	input := StartInput{
		RoomID: "room-writer", AgentSessionID: "session-writer",
		Provider: ProviderCodex, CWD: "/workspace", Title: "Codex",
	}
	if userHome {
		input.Env = []string{runtimeprep.CodexConfigOverridesEnv + `=["project_root_markers=[]"]`}
	}
	started, err := controller.Start(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return controller, adapter, transport, started.Session
}
