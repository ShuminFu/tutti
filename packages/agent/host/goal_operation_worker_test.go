package agenthost

import (
	"context"
	"errors"
	"testing"
	"time"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func TestGoalActorWaitObservesContextCancellation(t *testing.T) {
	actor := NewGoalActor()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	ref := SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
	go func() {
		done <- actor.Do(context.Background(), ref, func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := actor.Do(ctx, ref, func(context.Context) error {
		t.Fatal("canceled waiter entered GoalActor")
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GoalActor.Do() error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type retryRecordingGoalStore struct {
	GoalStateStore
	current  storesqlite.GoalControlOperation
	released []storesqlite.ReleaseGoalControlOperationInput
}

func (s *retryRecordingGoalStore) GetGoalControlOperation(context.Context, string, string) (storesqlite.GoalControlOperation, bool, error) {
	return s.current, true, nil
}

func (s *retryRecordingGoalStore) ReleaseGoalControlOperation(_ context.Context, input storesqlite.ReleaseGoalControlOperationInput) (storesqlite.GoalControlOperation, bool, error) {
	s.released = append(s.released, input)
	return s.current, true, nil
}

func TestRetryRecoveredGoalOperationPreservesRepairEvidence(t *testing.T) {
	store := &retryRecordingGoalStore{current: storesqlite.GoalControlOperation{
		OperationID: "repair-op", WorkspaceID: "workspace-1", AgentSessionID: "session-1",
		GoalRevision: 2, LeaseOwner: "goal-worker", RepairEpoch: 3, Attempt: 1,
		Evidence: map[string]any{"repair": map[string]any{"repairId": "incident-1"}},
	}}
	host := New(Config{GoalStore: store, GoalOwner: "goal-worker", GoalClock: fixedClock{at: time.UnixMilli(1_000)}})
	if err := host.retryRecoveredGoalOperation(context.Background(), store.current, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if len(store.released) != 1 {
		t.Fatalf("release inputs = %#v", store.released)
	}
	repair, ok := store.released[0].Evidence["repair"].(map[string]any)
	if !ok || repair["repairId"] != "incident-1" || store.released[0].RepairEpoch != 3 {
		t.Fatalf("release evidence = %#v", store.released[0])
	}
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }
