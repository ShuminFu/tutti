package agenthost_test

import (
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

// waitForTurn polls for a canonical turn: admission runs on the runtime's
// release-signal goroutine, so the assertion is eventually-consistent.
func waitForTurn(t *testing.T, store *storesqlite.Store, turnID string) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, found, err := store.GetTurn(t.Context(), "workspace-1", "session-1", turnID)
		if err == nil && found {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// A session runs one canonical turn at a time. When two writers submit at once
// — the user's composer and a host-side deliverer, which is exactly what
// produced the 2026-09-22 "消息未能发送. agent session already has an active
// turn" banner — the runtime refuses the loser. The Host must absorb that
// refusal: park the prompt, report it as queued, and dispatch it when the slot
// frees. Deleting the park branch in sendInputSerialized turns this red.
func TestHostParksOrdinaryPromptWhenTurnSlotIsBusy(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore:    sqliteCanonicalStore{Store: store},
		TurnSubmissions:   store,
		EffectiveHistory:  store,
		RuntimeOperations: store,
		Runtime:           runtime,
		HistoryRuntime:    runtime,
		GoalRuntime:       runtime,
		OperationOwner:    "worker-1",
	})
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}

	runtime.mu.Lock()
	runtime.turnSlotBusy = true
	runtime.mu.Unlock()

	result, err := host.SendInput(t.Context(), ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "生成测试用例"}},
		ClientSubmitID: "submit-parked",
		TurnID:         "turn-parked",
	})
	if err != nil {
		t.Fatalf("SendInput() error = %v, want nil (a busy slot is not a send failure)", err)
	}
	if result.Kind != agenthost.SubmitKindQueued {
		t.Fatalf("SendInput() kind = %q, want %q", result.Kind, agenthost.SubmitKindQueued)
	}
	if result.TurnID != "turn-parked" {
		t.Fatalf("SendInput() turnId = %q, want the preallocated canonical turn", result.TurnID)
	}

	if _, found, err := store.GetTurn(t.Context(), "workspace-1", "session-1", "turn-parked"); err != nil || found {
		t.Fatalf("parked prompt ran while the slot was still busy (found=%v err=%v)", found, err)
	}

	// The slot frees: the runtime tells the Host, and the parked prompt runs
	// without the caller resending anything.
	runtime.mu.Lock()
	runtime.turnSlotBusy = false
	runtime.mu.Unlock()
	host.ObserveTurnSlotReleased("workspace-1", "session-1")

	if !waitForTurn(t, store, "turn-parked") {
		t.Fatal("parked prompt never produced its canonical turn after the slot was released")
	}
}

// The parked prompt must not be dispatched twice: one release signal admits it
// once, and a second signal finds an empty queue.
func TestHostAdmitsEachParkedPromptOnce(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore:    sqliteCanonicalStore{Store: store},
		TurnSubmissions:   store,
		EffectiveHistory:  store,
		RuntimeOperations: store,
		Runtime:           runtime,
		HistoryRuntime:    runtime,
		GoalRuntime:       runtime,
		OperationOwner:    "worker-1",
	})
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}

	runtime.mu.Lock()
	runtime.turnSlotBusy = true
	runtime.mu.Unlock()

	if _, err := host.SendInput(t.Context(), ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "只跑一次"}},
		ClientSubmitID: "submit-once",
		TurnID:         "turn-once",
	}); err != nil {
		t.Fatalf("SendInput() error = %v", err)
	}
	runtime.mu.Lock()
	runtime.turnSlotBusy = false
	runtime.mu.Unlock()
	host.ObserveTurnSlotReleased("workspace-1", "session-1")
	if !waitForTurn(t, store, "turn-once") {
		t.Fatal("parked prompt never ran")
	}
	runtime.mu.Lock()
	afterFirst := runtime.execCalls
	runtime.mu.Unlock()

	host.ObserveTurnSlotReleased("workspace-1", "session-1")
	runtime.mu.Lock()
	afterSecond := runtime.execCalls
	runtime.mu.Unlock()

	if afterSecond != afterFirst {
		t.Fatalf("exec calls after a second release = %d, want %d (the queue is empty)", afterSecond, afterFirst)
	}
}
