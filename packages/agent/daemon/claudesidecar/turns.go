package claudesidecar

import (
	"time"
)

// RuntimeTurn mirrors turnLifecycle.ts's RuntimeTurn.
type RuntimeTurn struct {
	TurnID               string
	PromptUUID           string
	Synthetic            bool
	AwaitingContinuation bool
	Origin               string
	GoalOperationID      string
	GoalRevision         float64
	GoalRepairEpoch      float64
	GoalAction           string
	Settled              bool
}

// TurnLifecycle owns turn ordering, activation, and terminal settlement.
// Callers must hold the session runtime lock; timers re-enter through it.
type TurnLifecycle struct {
	turns                         []*RuntimeTurn
	emit                          Emitter
	onActivate                    func()
	onSettled                     func()
	onContinuationStartTimeout    func()
	continuationStartTimeout      time.Duration
	active                        *RuntimeTurn
	activeIDValue                 string
	lastTurnIDValue               string
	pendingOrphanCount            int
	cancelledValue                bool
	completedTurnCount            float64
	continuationStartTimer        *time.Timer
	rejectingTimedOutContinuation bool
	// lockTimerCallback wraps timer callbacks in the session runtime lock.
	lockTimerCallback func(func())
}

// TurnLifecycleOptions configures a TurnLifecycle.
type TurnLifecycleOptions struct {
	Emit                       Emitter
	OnActivate                 func()
	OnSettled                  func()
	OnContinuationStartTimeout func()
	ContinuationStartTimeout   time.Duration
	LockTimerCallback          func(func())
}

func NewTurnLifecycle(options TurnLifecycleOptions) *TurnLifecycle {
	timeout := options.ContinuationStartTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	onActivate := options.OnActivate
	if onActivate == nil {
		onActivate = func() {}
	}
	onSettled := options.OnSettled
	if onSettled == nil {
		onSettled = func() {}
	}
	onTimeout := options.OnContinuationStartTimeout
	if onTimeout == nil {
		onTimeout = func() {}
	}
	lock := options.LockTimerCallback
	if lock == nil {
		lock = func(callback func()) { callback() }
	}
	return &TurnLifecycle{
		emit:                       options.Emit,
		onActivate:                 onActivate,
		onSettled:                  onSettled,
		onContinuationStartTimeout: onTimeout,
		continuationStartTimeout:   timeout,
		lockTimerCallback:          lock,
	}
}

func (t *TurnLifecycle) ActiveID() string { return t.activeIDValue }

// LastTurnID is the most recent turn id, kept after the turn settles. Lets
// late background task events attribute to the turn that launched them.
func (t *TurnLifecycle) LastTurnID() string {
	if t.activeIDValue != "" {
		return t.activeIDValue
	}
	return t.lastTurnIDValue
}

func (t *TurnLifecycle) ActiveTurn() *RuntimeTurn { return t.active }

func (t *TurnLifecycle) AwaitingContinuation() bool {
	return t.active != nil && t.active.AwaitingContinuation
}

func (t *TurnLifecycle) Queue() []*RuntimeTurn { return t.turns }

func (t *TurnLifecycle) Cancelled() bool { return t.cancelledValue }

func (t *TurnLifecycle) TurnCount() float64 { return t.completedTurnCount }

func (t *TurnLifecycle) PendingOrphans() int { return t.pendingOrphanCount }

func (t *TurnLifecycle) RestoreTurnCount(value float64) {
	t.completedTurnCount = value
}

func (t *TurnLifecycle) ActivateTransient(turnID string) {
	t.activeIDValue = turnID
	t.onActivate()
}

func (t *TurnLifecycle) Enqueue(turn *RuntimeTurn) {
	t.turns = append(t.turns, turn)
}

func (t *TurnLifecycle) ActivateForPromptUUID(promptUUID string) {
	if promptUUID == "" {
		return
	}
	for _, turn := range t.turns {
		if !turn.Settled && turn.PromptUUID == promptUUID {
			if !turn.Synthetic {
				t.rejectingTimedOutContinuation = false
			}
			t.activate(turn)
			return
		}
	}
}

func (t *TurnLifecycle) ActivateForUserMessage(promptUUID string) {
	t.ActivateForPromptUUID(promptUUID)
	if t.active == nil {
		t.EnsureActive("user")
	}
}

func (t *TurnLifecycle) EnsureActive(messageType string) *RuntimeTurn {
	if t.active != nil && !t.active.Settled {
		if messageType == "assistant" || messageType == "stream_event" {
			t.confirmContinuationStarted()
		}
		return t.active
	}
	if t.rejectingTimedOutContinuation && messageType != "user" {
		return nil
	}
	if messageType != "user" && t.pendingOrphanCount > 0 {
		return nil
	}
	var turn *RuntimeTurn
	for _, candidate := range t.turns {
		if !candidate.Settled {
			turn = candidate
			break
		}
	}
	if turn == nil {
		if messageType == "assistant" {
			return t.ActivateSynthetic()
		}
		return nil
	}
	t.activate(turn)
	return turn
}

func (t *TurnLifecycle) ActivateSynthetic() *RuntimeTurn {
	turn := &RuntimeTurn{
		TurnID:    "synthetic-" + randomUUID(),
		Synthetic: true,
	}
	t.turns = append(t.turns, turn)
	t.activate(turn)
	return turn
}

func (t *TurnLifecycle) ExpectSyntheticContinuation() *RuntimeTurn {
	if t.active != nil && !t.active.Settled {
		return t.active
	}
	if t.rejectingTimedOutContinuation {
		return nil
	}
	turn := t.ActivateSynthetic()
	turn.AwaitingContinuation = true
	t.continuationStartTimer = time.AfterFunc(t.continuationStartTimeout, func() {
		t.lockTimerCallback(func() {
			if t.active != turn || turn.Settled || !turn.AwaitingContinuation {
				return
			}
			turn.AwaitingContinuation = false
			t.rejectingTimedOutContinuation = true
			t.SettleActive("turn_completed", map[string]any{
				"stopReason":       "background_agent_continuation_timeout",
				"syntheticTimeout": true,
			})
			t.onContinuationStartTimeout()
		})
	})
	return turn
}

func (t *TurnLifecycle) ConsumeTimedOutContinuationResult() bool {
	if !t.rejectingTimedOutContinuation {
		return false
	}
	t.rejectingTimedOutContinuation = false
	return true
}

func (t *TurnLifecycle) CloseSyntheticBeforeUserTurn() {
	if t.active == nil || !t.active.Synthetic || t.active.Settled {
		return
	}
	t.SettleActive("turn_completed", map[string]any{"stopReason": "background_agent"})
}

func (t *TurnLifecycle) SettleActive(eventType string, payload map[string]any) {
	turn := t.active
	if turn == nil || turn.Settled {
		return
	}
	turn.Settled = true
	t.completedTurnCount++
	eventPayload := cloneRecord(payload)
	if eventPayload == nil {
		eventPayload = map[string]any{}
	}
	eventPayload["turnId"] = turn.TurnID
	t.emit(eventType, "", eventPayload)
	t.clearContinuationStartTimer()
	t.active = nil
	t.activeIDValue = ""
	t.compactQueue()
	t.onSettled()
}

func (t *TurnLifecycle) FailLiveTurns(errorText string) {
	if t.active != nil {
		eventType := "turn_failed"
		if t.cancelledValue {
			eventType = "turn_canceled"
		}
		t.SettleActive(eventType, map[string]any{"error": errorText})
	}
	t.FailQueuedTurns(errorText)
}

func (t *TurnLifecycle) FailQueuedTurns(errorText string) {
	for _, turn := range t.turns {
		if turn.Settled || turn == t.active {
			continue
		}
		eventType := "turn_failed"
		if t.cancelledValue {
			eventType = "turn_canceled"
		}
		t.settleQueuedTurn(turn, eventType, map[string]any{"error": errorText})
	}
	t.compactQueue()
}

func (t *TurnLifecycle) CancelQueued() bool {
	t.cancelledValue = true
	t.clearContinuationStartTimer()
	if t.active != nil {
		t.active.AwaitingContinuation = false
	}
	orphaned := 0
	for _, turn := range t.turns {
		if turn.Settled || turn == t.active {
			continue
		}
		t.settleQueuedTurn(turn, "turn_canceled", nil)
		orphaned++
	}
	t.pendingOrphanCount += orphaned
	t.compactQueue()
	return t.active != nil
}

func (t *TurnLifecycle) CancelActiveExact(turnID string) bool {
	expected := normalizeTitle(turnID)
	if expected == "" || t.active == nil || t.active.TurnID != expected || t.active.Settled {
		return false
	}
	t.cancelledValue = true
	return true
}

func (t *TurnLifecycle) ClearCancelled() {
	t.cancelledValue = false
}

func (t *TurnLifecycle) ClearPendingOrphans() {
	t.pendingOrphanCount = 0
}

func (t *TurnLifecycle) ConsumePendingOrphan() bool {
	if t.pendingOrphanCount <= 0 {
		return false
	}
	t.pendingOrphanCount--
	return true
}

func (t *TurnLifecycle) Close() {
	t.clearContinuationStartTimer()
}

func (t *TurnLifecycle) activate(turn *RuntimeTurn) {
	if t.active == turn {
		return
	}
	t.active = turn
	t.activeIDValue = turn.TurnID
	t.lastTurnIDValue = turn.TurnID
	t.cancelledValue = false
	t.pendingOrphanCount = 0
	t.onActivate()
	if turn.GoalOperationID != "" && turn.GoalRevision != 0 && turn.GoalAction != "" {
		t.emit("goal_command_started", "", map[string]any{
			"turnId":      turn.TurnID,
			"operationId": turn.GoalOperationID,
			"revision":    turn.GoalRevision,
			"repairEpoch": turn.GoalRepairEpoch,
			"action":      turn.GoalAction,
		})
	}
	if turn.Synthetic || turn.Origin != "" {
		payload := map[string]any{"turnId": turn.TurnID}
		if turn.Synthetic {
			payload["synthetic"] = true
		}
		if turn.Origin != "" {
			payload["turnOrigin"] = turn.Origin
		}
		if turn.GoalOperationID != "" {
			payload["sourceGoalOperationId"] = turn.GoalOperationID
		}
		if turn.GoalRevision != 0 {
			payload["sourceGoalRevision"] = turn.GoalRevision
		}
		if turn.GoalRepairEpoch != 0 {
			payload["sourceGoalRepairEpoch"] = turn.GoalRepairEpoch
		}
		t.emit("turn_started", "", payload)
	}
}

func (t *TurnLifecycle) confirmContinuationStarted() {
	if t.active == nil || !t.active.AwaitingContinuation {
		return
	}
	t.active.AwaitingContinuation = false
	t.clearContinuationStartTimer()
}

func (t *TurnLifecycle) clearContinuationStartTimer() {
	if t.continuationStartTimer == nil {
		return
	}
	t.continuationStartTimer.Stop()
	t.continuationStartTimer = nil
}

func (t *TurnLifecycle) settleQueuedTurn(turn *RuntimeTurn, eventType string, payload map[string]any) {
	if turn.Settled {
		return
	}
	turn.Settled = true
	eventPayload := cloneRecord(payload)
	if eventPayload == nil {
		eventPayload = map[string]any{}
	}
	eventPayload["turnId"] = turn.TurnID
	t.emit(eventType, "", eventPayload)
}

func (t *TurnLifecycle) compactQueue() {
	for len(t.turns) > 0 && t.turns[0].Settled {
		t.turns = t.turns[1:]
	}
}
