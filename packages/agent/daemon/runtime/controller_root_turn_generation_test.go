package agentruntime

import (
	"testing"
)

func TestReconcileRootTurnSettlementIgnoresOlderTurnAfterNewerTurnStarted(t *testing.T) {
	t.Parallel()

	controller := NewController(nil, nil)
	newTurnID := "turn-new"
	session := Session{
		RoomID:         "room-1",
		AgentSessionID: "agent-1",
		Provider:       ProviderClaudeCode,
		Status:         SessionStatusWorking,
		TurnLifecycle: &TurnLifecycle{
			ActiveTurnID: &newTurnID,
			Phase:        "running",
		},
		SubmitAvailability: blockedSubmitAvailability("active_turn"),
	}
	controller.store(session)
	controller.mu.Lock()
	controller.turns[sessionKey(session.RoomID, session.AgentSessionID)] = activeTurn{turnID: newTurnID}
	controller.mu.Unlock()

	controller.ReconcileRootTurnSettlement(RootTurnSettlement{
		RoomID:         session.RoomID,
		AgentSessionID: session.AgentSessionID,
		TurnID:         "turn-old",
		Outcome:        "completed",
	})

	if !controller.HasActiveTurn(session.RoomID, session.AgentSessionID) {
		t.Fatal("stale settlement cleared the newer active turn")
	}
	got, ok := controller.get(session.RoomID, session.AgentSessionID)
	if !ok {
		t.Fatal("session missing after stale settlement")
	}
	if got.Status != SessionStatusWorking {
		t.Fatalf("status = %q, want working", got.Status)
	}
	if runtimeTurnLifecycleActiveTurnID(got.TurnLifecycle) != newTurnID {
		t.Fatalf("live turn = %q, want %q", runtimeTurnLifecycleActiveTurnID(got.TurnLifecycle), newTurnID)
	}
	if got.SubmitAvailability == nil || got.SubmitAvailability.State != "blocked" {
		t.Fatalf("submit availability = %#v, want blocked", got.SubmitAvailability)
	}
}

func TestReconcileRootTurnSettlementIgnoresStaleTurnWhenLiveLifecycleDiffers(t *testing.T) {
	t.Parallel()

	controller := NewController(nil, nil)
	newTurnID := "turn-new"
	session := Session{
		RoomID:         "room-1",
		AgentSessionID: "agent-1",
		Status:         SessionStatusWorking,
		TurnLifecycle: &TurnLifecycle{
			ActiveTurnID: &newTurnID,
			Phase:        "running",
		},
	}
	controller.store(session)

	controller.ReconcileRootTurnSettlement(RootTurnSettlement{
		RoomID:         session.RoomID,
		AgentSessionID: session.AgentSessionID,
		TurnID:         "turn-old",
		Outcome:        "completed",
	})

	got, ok := controller.get(session.RoomID, session.AgentSessionID)
	if !ok {
		t.Fatal("session missing")
	}
	if got.Status != SessionStatusWorking || runtimeTurnLifecycleActiveTurnID(got.TurnLifecycle) != newTurnID {
		t.Fatalf("stale settlement overwrote live lifecycle: %#v", got)
	}
}

func TestReconcileRootTurnSettlementIgnoresDuplicateAfterSettle(t *testing.T) {
	t.Parallel()

	controller := NewController(nil, nil)
	turnID := "turn-1"
	session := Session{
		RoomID:         "room-1",
		AgentSessionID: "agent-1",
		Status:         SessionStatusWorking,
		TurnLifecycle: &TurnLifecycle{
			ActiveTurnID: &turnID,
			Phase:        "running",
		},
	}
	controller.store(session)
	controller.mu.Lock()
	controller.turns[sessionKey(session.RoomID, session.AgentSessionID)] = activeTurn{turnID: turnID}
	controller.mu.Unlock()

	controller.ReconcileRootTurnSettlement(RootTurnSettlement{
		RoomID:         session.RoomID,
		AgentSessionID: session.AgentSessionID,
		TurnID:         turnID,
		Outcome:        "completed",
	})
	got, ok := controller.get(session.RoomID, session.AgentSessionID)
	if !ok || got.Status != SessionStatusReady {
		t.Fatalf("first settlement = %#v", got)
	}
	updatedAt := got.UpdatedAtUnixMS

	controller.ReconcileRootTurnSettlement(RootTurnSettlement{
		RoomID:         session.RoomID,
		AgentSessionID: session.AgentSessionID,
		TurnID:         turnID,
		Outcome:        "failed",
		ErrorMessage:   "late duplicate",
	})
	got, ok = controller.get(session.RoomID, session.AgentSessionID)
	if !ok {
		t.Fatal("session missing after duplicate settlement")
	}
	if got.Status != SessionStatusReady {
		t.Fatalf("duplicate settlement changed status to %q", got.Status)
	}
	if got.TurnLifecycle == nil || got.TurnLifecycle.Outcome == nil || *got.TurnLifecycle.Outcome != "completed" {
		t.Fatalf("duplicate settlement changed outcome: %#v", got.TurnLifecycle)
	}
	if got.UpdatedAtUnixMS != updatedAt {
		t.Fatalf("duplicate settlement rewrote session timestamp")
	}
}

func TestReconcileRootTurnSettlementStillAppliesMatchingLiveTurn(t *testing.T) {
	t.Parallel()

	controller := NewController(nil, nil)
	turnID := "turn-1"
	session := Session{
		RoomID:         "room-1",
		AgentSessionID: "agent-1",
		Status:         SessionStatusWorking,
		TurnLifecycle: &TurnLifecycle{
			ActiveTurnID: &turnID,
			Phase:        "running",
		},
		SubmitAvailability: blockedSubmitAvailability("active_turn"),
	}
	controller.store(session)
	controller.mu.Lock()
	controller.turns[sessionKey(session.RoomID, session.AgentSessionID)] = activeTurn{turnID: turnID}
	controller.mu.Unlock()

	controller.ReconcileRootTurnSettlement(RootTurnSettlement{
		RoomID:         session.RoomID,
		AgentSessionID: session.AgentSessionID,
		TurnID:         turnID,
		Outcome:        "completed",
	})

	if controller.HasActiveTurn(session.RoomID, session.AgentSessionID) {
		t.Fatal("matching settlement left the active turn in place")
	}
	got, ok := controller.get(session.RoomID, session.AgentSessionID)
	if !ok || got.Status != SessionStatusReady || sessionHasLiveTurnLifecycle(got) {
		t.Fatalf("matching settlement = %#v", got)
	}
	if got.SubmitAvailability == nil || got.SubmitAvailability.State != "available" {
		t.Fatalf("submit availability = %#v, want available", got.SubmitAvailability)
	}
}
