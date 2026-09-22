package agentruntime

import (
	"strings"
)

// TurnSlotObserver is told that a session's canonical turn slot became free.
//
// Why this exists: a session can run exactly one canonical turn at a time, and
// the controller is the only component that knows the truth. Every other layer
// (the AgentGUI prompt queue, the RnDMaster peer deliverer) used to keep its
// own "is this session busy?" projection and decide from it whether to submit.
// Two writers reading two stale copies start two turns for one slot, and the
// loser was rejected with ErrSessionActiveTurn — a red banner on a message the
// user had already sent. Ordinary prompts now park above the controller and
// are admitted from this signal, so the projections only drive affordances and
// never correctness.
//
// Implementations must return promptly; the notification runs on its own
// goroutine and may take the session lifecycle lock.
type TurnSlotObserver interface {
	ObserveTurnSlotReleased(roomID string, agentSessionID string)
}

// TurnSlotObserverFunc adapts a function to TurnSlotObserver.
type TurnSlotObserverFunc func(roomID string, agentSessionID string)

func (f TurnSlotObserverFunc) ObserveTurnSlotReleased(roomID string, agentSessionID string) {
	if f == nil {
		return
	}
	f(roomID, agentSessionID)
}

// SetTurnSlotObserver registers the single consumer of slot-release signals.
// Passing nil clears it.
func (c *Controller) SetTurnSlotObserver(observer TurnSlotObserver) {
	if c == nil {
		return
	}
	c.turnSlotObserverMu.Lock()
	defer c.turnSlotObserverMu.Unlock()
	c.turnSlotObserver = observer
}

func (c *Controller) loadTurnSlotObserver() TurnSlotObserver {
	if c == nil {
		return nil
	}
	c.turnSlotObserverMu.RLock()
	defer c.turnSlotObserverMu.RUnlock()
	return c.turnSlotObserver
}

// releaseTurnSlotLocked is the ONLY place a c.turns entry is removed. Every
// path that ends a turn — normal settle, cancel, commit, rollback, session
// close — goes through here so the slot-free signal can never be forgotten by
// one of them. Callers hold c.mu; the observer runs on its own goroutine.
func (c *Controller) releaseTurnSlotLocked(key string) {
	if c == nil {
		return
	}
	if _, ok := c.turns[key]; !ok {
		return
	}
	delete(c.turns, key)
	observer := c.loadTurnSlotObserver()
	if observer == nil {
		return
	}
	roomID, agentSessionID := splitSessionKey(key)
	if agentSessionID == "" {
		return
	}
	go observer.ObserveTurnSlotReleased(roomID, agentSessionID)
}

// splitSessionKey reverses sessionKey. Room ids never contain "/", so the
// first separator is the boundary.
func splitSessionKey(key string) (roomID string, agentSessionID string) {
	roomID, agentSessionID, found := strings.Cut(key, "/")
	if !found {
		return "", strings.TrimSpace(key)
	}
	return strings.TrimSpace(roomID), strings.TrimSpace(agentSessionID)
}
