package agenthost

import (
	"context"
	"strings"
	"sync"
	"time"
)

type goalActorEntry struct {
	gate chan struct{}
	refs int
}

// GoalActor serializes one session's goal revision transitions. An adapter
// that constructs short-lived Host values must share one GoalActor instance.
type GoalActor struct {
	mu      sync.Mutex
	entries map[string]*goalActorEntry
}

func NewGoalActor() *GoalActor {
	return &GoalActor{entries: make(map[string]*goalActorEntry)}
}

func (a *GoalActor) Do(ctx context.Context, ref SessionRef, fn func(context.Context) error) error {
	if a == nil || strings.TrimSpace(ref.WorkspaceID) == "" || strings.TrimSpace(ref.AgentSessionID) == "" || fn == nil {
		return ErrInvalidArgument
	}
	return a.do(ctx, ref.WorkspaceID, ref.AgentSessionID, fn)
}

func (h *Host) withGoalActor(ctx context.Context, workspaceID, agentSessionID string, fn func(context.Context) error) error {
	return h.goalActor.Do(ctx, SessionRef{WorkspaceID: workspaceID, AgentSessionID: agentSessionID}, fn)
}

func (a *GoalActor) do(ctx context.Context, workspaceID, agentSessionID string, fn func(context.Context) error) error {
	key := strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(agentSessionID)
	a.mu.Lock()
	entry := a.entries[key]
	if entry == nil {
		entry = &goalActorEntry{gate: make(chan struct{}, 1)}
		entry.gate <- struct{}{}
		a.entries[key] = entry
	}
	entry.refs++
	a.mu.Unlock()

	select {
	case <-ctx.Done():
		a.releaseReference(key, entry)
		return ctx.Err()
	case <-entry.gate:
	}
	if err := ctx.Err(); err != nil {
		entry.gate <- struct{}{}
		a.releaseReference(key, entry)
		return err
	}
	err := fn(ctx)
	entry.gate <- struct{}{}
	a.releaseReference(key, entry)
	return err
}

func (a *GoalActor) releaseReference(key string, entry *goalActorEntry) {
	a.mu.Lock()
	entry.refs--
	if entry.refs == 0 && a.entries[key] == entry {
		delete(a.entries, key)
	}
	a.mu.Unlock()
}

func (h *Host) goalOperationNow() time.Time {
	if h != nil && h.goalClock != nil {
		return h.goalClock.Now().UTC()
	}
	return time.Now().UTC()
}

func goalPersistenceContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
