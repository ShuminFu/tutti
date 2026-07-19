package claudesidecar

import (
	"regexp"
	"sync"
	"testing"
	"time"
)

type lifecycleFixture struct {
	lifecycle   *TurnLifecycle
	mu          sync.Mutex
	events      []recordedEvent
	activations int
	settlements int
	timeouts    int
}

func newLifecycleFixture(timeout time.Duration) *lifecycleFixture {
	fixture := &lifecycleFixture{}
	fixture.lifecycle = NewTurnLifecycle(TurnLifecycleOptions{
		Emit: func(eventType string, _ string, payload map[string]any) {
			fixture.events = append(fixture.events, recordedEvent{eventType: eventType, payload: payload})
		},
		OnActivate:                 func() { fixture.activations++ },
		OnSettled:                  func() { fixture.settlements++ },
		OnContinuationStartTimeout: func() { fixture.timeouts++ },
		ContinuationStartTimeout:   timeout,
		LockTimerCallback: func(callback func()) {
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			callback()
		},
	})
	return fixture
}

func TestTurnLifecycleActivatesAndSettlesQueuedTurn(t *testing.T) {
	fixture := newLifecycleFixture(0)
	lifecycle := fixture.lifecycle
	lifecycle.Enqueue(&RuntimeTurn{TurnID: "turn-1", PromptUUID: "prompt-1"})

	lifecycle.ActivateForUserMessage("prompt-1")
	lifecycle.SettleActive("turn_completed", map[string]any{"stopReason": "end_turn"})

	if lifecycle.ActiveID() != "" || lifecycle.TurnCount() != 1 {
		t.Fatalf("activeId=%q turnCount=%v", lifecycle.ActiveID(), lifecycle.TurnCount())
	}
	if fixture.activations != 1 || fixture.settlements != 1 {
		t.Fatalf("activations=%d settlements=%d", fixture.activations, fixture.settlements)
	}
	first := fixture.events[0]
	if first.eventType != "turn_completed" ||
		first.payload["stopReason"] != "end_turn" ||
		first.payload["turnId"] != "turn-1" {
		t.Fatalf("first event = %#v", first)
	}
}

func TestTurnLifecycleAnnouncesGoalArmBeforeFirstOutput(t *testing.T) {
	fixture := newLifecycleFixture(0)
	lifecycle := fixture.lifecycle
	lifecycle.Enqueue(&RuntimeTurn{TurnID: "goal-arm-1", PromptUUID: "prompt-goal", Origin: "goal_arm"})

	lifecycle.ActivateForUserMessage("prompt-goal")

	first := fixture.events[0]
	if first.eventType != "turn_started" ||
		first.payload["turnId"] != "goal-arm-1" ||
		first.payload["turnOrigin"] != "goal_arm" {
		t.Fatalf("first event = %#v", first)
	}
}

func TestGoalActivationCarriesImmutableCommandIdentity(t *testing.T) {
	fixture := newLifecycleFixture(0)
	lifecycle := fixture.lifecycle
	lifecycle.Enqueue(&RuntimeTurn{
		TurnID:          "goal-arm-immutable",
		PromptUUID:      "prompt-goal-immutable",
		Origin:          "goal_arm",
		GoalOperationID: "goal-op-1",
		GoalRevision:    1,
		GoalRepairEpoch: 7,
		GoalAction:      "set",
	})

	lifecycle.ActivateForUserMessage("prompt-goal-immutable")

	var applied, started *recordedEvent
	for index := range fixture.events {
		switch fixture.events[index].eventType {
		case "goal_command_started":
			applied = &fixture.events[index]
		case "turn_started":
			started = &fixture.events[index]
		}
	}
	if applied == nil ||
		applied.payload["operationId"] != "goal-op-1" ||
		numberValue(applied.payload["revision"]) != 1 ||
		numberValue(applied.payload["repairEpoch"]) != 7 {
		t.Fatalf("goal_command_started = %#v", applied)
	}
	if started == nil ||
		started.payload["sourceGoalOperationId"] != "goal-op-1" ||
		numberValue(started.payload["sourceGoalRevision"]) != 1 ||
		numberValue(started.payload["sourceGoalRepairEpoch"]) != 7 {
		t.Fatalf("turn_started = %#v", started)
	}
}

func TestTurnLifecycleCreatesSyntheticTurnForOrphanAssistantOutput(t *testing.T) {
	fixture := newLifecycleFixture(0)
	turn := fixture.lifecycle.EnsureActive("assistant")

	if turn == nil || !turn.Synthetic {
		t.Fatalf("turn = %#v", turn)
	}
	if !regexp.MustCompile(`^synthetic-`).MatchString(turn.TurnID) {
		t.Fatalf("turnId = %q", turn.TurnID)
	}
	first := fixture.events[0]
	if first.eventType != "turn_started" || first.payload["synthetic"] != true {
		t.Fatalf("first event = %#v", first)
	}
}

func TestTurnLifecycleCancelsQueuedTurnsAndConsumesOrphanResults(t *testing.T) {
	fixture := newLifecycleFixture(0)
	lifecycle := fixture.lifecycle
	lifecycle.Enqueue(&RuntimeTurn{TurnID: "turn-1", PromptUUID: "prompt-1"})
	lifecycle.Enqueue(&RuntimeTurn{TurnID: "turn-2", PromptUUID: "prompt-2"})
	lifecycle.ActivateForUserMessage("prompt-1")

	if !lifecycle.CancelQueued() {
		t.Fatal("CancelQueued returned false")
	}
	if lifecycle.PendingOrphans() != 1 {
		t.Fatalf("pendingOrphans = %d", lifecycle.PendingOrphans())
	}
	if !lifecycle.ConsumePendingOrphan() {
		t.Fatal("first ConsumePendingOrphan returned false")
	}
	if lifecycle.ConsumePendingOrphan() {
		t.Fatal("second ConsumePendingOrphan returned true")
	}
	last := fixture.events[len(fixture.events)-1]
	if last.eventType != "turn_canceled" || last.payload["turnId"] != "turn-2" {
		t.Fatalf("last event = %#v", last)
	}
}

func TestReservedSyntheticTurnTimesOutAndRejectsLateContinuation(t *testing.T) {
	fixture := newLifecycleFixture(5 * time.Millisecond)
	fixture.mu.Lock()
	lifecycle := fixture.lifecycle
	reserved := lifecycle.ExpectSyntheticContinuation()
	if reserved == nil || !reserved.Synthetic || !lifecycle.AwaitingContinuation() {
		t.Fatalf("reserved = %#v awaiting=%v", reserved, lifecycle.AwaitingContinuation())
	}
	if fixture.events[0].eventType != "turn_started" {
		t.Fatalf("first event = %#v", fixture.events[0])
	}
	fixture.mu.Unlock()

	waitUntil(t, func() bool {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		return fixture.timeouts == 1
	})

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if lifecycle.ActiveID() != "" {
		t.Fatalf("activeId = %q", lifecycle.ActiveID())
	}
	last := fixture.events[len(fixture.events)-1]
	if last.eventType != "turn_completed" ||
		last.payload["stopReason"] != "background_agent_continuation_timeout" ||
		last.payload["syntheticTimeout"] != true ||
		last.payload["turnId"] != reserved.TurnID {
		t.Fatalf("last event = %#v", last)
	}
	if lifecycle.EnsureActive("assistant") != nil {
		t.Fatal("EnsureActive activated after timeout rejection")
	}
	if !lifecycle.ConsumeTimedOutContinuationResult() {
		t.Fatal("ConsumeTimedOutContinuationResult returned false")
	}

	lifecycle.Enqueue(&RuntimeTurn{TurnID: "turn-after-timeout", PromptUUID: "prompt-after-timeout"})
	lifecycle.ActivateForUserMessage("prompt-after-timeout")
	if lifecycle.ActiveID() != "turn-after-timeout" {
		t.Fatalf("activeId = %q", lifecycle.ActiveID())
	}
	if lifecycle.ConsumeTimedOutContinuationResult() {
		t.Fatal("ConsumeTimedOutContinuationResult returned true after activation")
	}
}

func TestRootOutputConfirmsReservedContinuationAndDisarmsTimeout(t *testing.T) {
	fixture := newLifecycleFixture(5 * time.Millisecond)
	fixture.mu.Lock()
	lifecycle := fixture.lifecycle
	reserved := lifecycle.ExpectSyntheticContinuation()

	if lifecycle.EnsureActive("assistant") != reserved {
		t.Fatal("EnsureActive did not confirm the reserved turn")
	}
	if lifecycle.AwaitingContinuation() {
		t.Fatal("still awaiting continuation")
	}
	fixture.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.timeouts != 0 {
		t.Fatalf("timeouts = %d", fixture.timeouts)
	}
	started := 0
	completed := false
	for _, event := range fixture.events {
		if event.eventType == "turn_started" {
			started++
		}
		if event.eventType == "turn_completed" {
			completed = true
		}
	}
	if started != 1 || completed {
		t.Fatalf("started=%d completed=%v events=%#v", started, completed, fixture.events)
	}
	lifecycle.SettleActive("turn_completed", nil)
}

func TestCancelAndGuidancePreserveReservedContinuationOwnership(t *testing.T) {
	fixture := newLifecycleFixture(5 * time.Millisecond)
	fixture.mu.Lock()
	lifecycle := fixture.lifecycle
	reserved := lifecycle.ExpectSyntheticContinuation()

	lifecycle.ActivateForUserMessage("guidance-prompt")
	if lifecycle.ActiveTurn() != reserved {
		t.Fatal("guidance stole the reserved turn")
	}
	started := 0
	for _, event := range fixture.events {
		if event.eventType == "turn_started" {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("turn_started count = %d", started)
	}

	if !lifecycle.CancelQueued() {
		t.Fatal("CancelQueued returned false")
	}
	fixture.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.timeouts != 0 {
		t.Fatalf("timeouts = %d", fixture.timeouts)
	}
	lifecycle.SettleActive("turn_canceled", nil)
	last := fixture.events[len(fixture.events)-1]
	if last.eventType != "turn_canceled" || last.payload["turnId"] != reserved.TurnID {
		t.Fatalf("last event = %#v", last)
	}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached before deadline")
}
