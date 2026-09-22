package agentruntime

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingTurnSlotObserver struct {
	mu       sync.Mutex
	released []string
	hadTurn  []bool
	notify   chan struct{}
}

func newRecordingTurnSlotObserver() *recordingTurnSlotObserver {
	return &recordingTurnSlotObserver{notify: make(chan struct{}, 8)}
}

func (o *recordingTurnSlotObserver) observe(c *Controller) TurnSlotObserver {
	return TurnSlotObserverFunc(func(roomID string, agentSessionID string) {
		o.mu.Lock()
		o.released = append(o.released, roomID+"/"+agentSessionID)
		o.hadTurn = append(o.hadTurn, c.HasActiveTurn(roomID, agentSessionID))
		o.mu.Unlock()
		select {
		case o.notify <- struct{}{}:
		default:
		}
	})
}

func (o *recordingTurnSlotObserver) wait(t *testing.T) {
	t.Helper()
	select {
	case <-o.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("turn slot release was never observed")
	}
}

// The turn slot is the session's one serial resource, and the controller is
// the only component that knows when it frees. Everything that parks a prompt
// behind a running turn depends on this signal; if a settle path forgets to
// emit it, those prompts hang forever.
func TestControllerNotifiesTurnSlotObserverOnSettle(t *testing.T) {
	t.Parallel()

	adapter := newBlockingExecAdapter()
	controller := NewController([]Adapter{adapter}, nil)
	observer := newRecordingTurnSlotObserver()
	controller.SetTurnSlotObserver(observer.observe(controller))
	ctx := context.Background()

	started, err := controller.Start(ctx, StartInput{
		RoomID:         "room-1",
		AgentSessionID: "agent-session-1",
		Provider:       ProviderCodex,
		CWD:            "/workspace",
		Title:          "Codex",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := controller.Exec(ctx, ExecInput{
		RoomID:         started.Session.RoomID,
		AgentSessionID: started.Session.AgentSessionID,
		Content:        textPrompt("first prompt"),
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	adapter.waitForPrompt(t, "first prompt")

	observer.mu.Lock()
	early := len(observer.released)
	observer.mu.Unlock()
	if early != 0 {
		t.Fatalf("slot release signals while the turn is running = %d, want 0", early)
	}

	adapter.releaseNext()
	waitForSessionStatus(t, controller, "room-1", started.Session.AgentSessionID, SessionStatusReady)
	observer.wait(t)

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.released) == 0 {
		t.Fatal("settling the turn did not release the slot")
	}
	if got := observer.released[0]; got != "room-1/agent-session-1" {
		t.Fatalf("released session = %q, want room-1/agent-session-1", got)
	}
	// The signal must mean the slot is already free: a consumer that retries a
	// parked prompt on this signal would otherwise be rejected again.
	if observer.hadTurn[0] {
		t.Fatal("slot release was announced while the controller still held the turn")
	}
}

func TestSplitSessionKeyReversesSessionKey(t *testing.T) {
	t.Parallel()

	roomID, agentSessionID := splitSessionKey(sessionKey("room-1", "agent-session-1"))
	if roomID != "room-1" || agentSessionID != "agent-session-1" {
		t.Fatalf("splitSessionKey = (%q, %q), want (room-1, agent-session-1)", roomID, agentSessionID)
	}
}
