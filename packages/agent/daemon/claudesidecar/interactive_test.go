package claudesidecar

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

type coordinatorFixture struct {
	coordinator *InteractiveCoordinator
	mu          sync.Mutex
	events      []recordedEvent
}

func (f *coordinatorFixture) waitForEvent(t *testing.T, index int) recordedEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.events) > index {
			event := f.events[index]
			f.mu.Unlock()
			return event
		}
		f.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("event %d never arrived", index)
	return recordedEvent{}
}

func (f *coordinatorFixture) countEvents(eventType string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, event := range f.events {
		if event.eventType == eventType {
			count++
		}
	}
	return count
}

func newCoordinatorFixture() *coordinatorFixture {
	fixture := &coordinatorFixture{}
	fixture.coordinator = NewInteractiveCoordinator(InteractiveCoordinatorOptions{
		Settings: &SessionSettings{PermissionModeID: "default", Speed: "standard"},
		ResolveTurnID: func(claudecli.ToolPermissionRequest) string {
			return "turn-1"
		},
		ActivateSyntheticTurn: func() string { return "synthetic-1" },
		Emit: func(eventType string, _ string, payload map[string]any) {
			fixture.events = append(fixture.events, recordedEvent{eventType: eventType, payload: payload})
		},
		Lock: func(callback func()) {
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			callback()
		},
	})
	return fixture
}

type permissionOutcome struct {
	result claudecli.PermissionResult
	err    error
}

func TestInteractiveCoordinatorResolvesApprovalOnOriginatingTurn(t *testing.T) {
	fixture := newCoordinatorFixture()
	outcome := make(chan permissionOutcome, 1)
	go func() {
		result, err := fixture.coordinator.HandleToolPermission(
			"Bash",
			map[string]any{"command": "pwd"},
			claudecli.ToolPermissionRequest{Ctx: context.Background(), ToolUseID: "tool-1"},
		)
		outcome <- permissionOutcome{result: result, err: err}
	}()
	request := fixture.waitForEvent(t, 0)
	requestID := stringValue(request.payload["requestId"])

	var submitted InteractiveSubmitResult
	fixture.mu.Lock()
	submitted = fixture.coordinator.Submit("turn-1", requestID, "approved", "allow", map[string]any{})
	fixture.mu.Unlock()

	result := <-outcome
	if result.err != nil {
		t.Fatalf("HandleToolPermission error = %v", result.err)
	}
	if result.result.Behavior != "allow" || !reflect.DeepEqual(result.result.UpdatedInput, map[string]any{"command": "pwd"}) {
		t.Fatalf("result = %#v", result.result)
	}
	if request.eventType != "approval_requested" || request.payload["turnId"] != "turn-1" {
		t.Fatalf("request = %#v", request)
	}
	resolved := fixture.waitForEvent(t, 1)
	if resolved.eventType != "approval_resolved" || resolved.payload["turnId"] != "turn-1" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if !reflect.DeepEqual(submitted.payload(), map[string]any{"disposition": "answered", "replayed": false}) {
		t.Fatalf("submitted = %#v", submitted.payload())
	}
	fixture.mu.Lock()
	disposition := fixture.coordinator.Disposition("turn-1", requestID, nil)
	fixture.mu.Unlock()
	if !reflect.DeepEqual(disposition.payload(), map[string]any{"disposition": "answered", "replayed": true}) {
		t.Fatalf("disposition = %#v", disposition.payload())
	}
}

func TestInteractiveCoordinatorReplaysIdenticalSubmissionsAndRejectsConflicts(t *testing.T) {
	fixture := newCoordinatorFixture()
	outcome := make(chan permissionOutcome, 1)
	go func() {
		result, err := fixture.coordinator.HandleToolPermission(
			"Bash",
			map[string]any{"command": "pwd"},
			claudecli.ToolPermissionRequest{Ctx: context.Background()},
		)
		outcome <- permissionOutcome{result: result, err: err}
	}()
	request := fixture.waitForEvent(t, 0)
	requestID := stringValue(request.payload["requestId"])
	payload := map[string]any{"reason": "approved"}

	fixture.mu.Lock()
	first := fixture.coordinator.Submit("turn-1", requestID, "approved", "allow", payload)
	fixture.mu.Unlock()
	if !reflect.DeepEqual(first.payload(), map[string]any{"disposition": "answered", "replayed": false}) {
		t.Fatalf("first = %#v", first.payload())
	}
	<-outcome

	fixture.mu.Lock()
	replay := fixture.coordinator.Submit("turn-1", requestID, "approved", "allow", payload)
	conflict := fixture.coordinator.Submit("turn-1", requestID, "approved", "deny", payload)
	dispositionConflict := fixture.coordinator.Disposition("turn-1", requestID, &InteractiveSubmission{
		Action:   "approved",
		OptionID: "deny",
		Payload:  payload,
	})
	fixture.mu.Unlock()

	if !reflect.DeepEqual(replay.payload(), map[string]any{"disposition": "answered", "replayed": true}) {
		t.Fatalf("replay = %#v", replay.payload())
	}
	if !reflect.DeepEqual(conflict.payload(), map[string]any{"disposition": "conflict"}) {
		t.Fatalf("conflict = %#v", conflict.payload())
	}
	if !reflect.DeepEqual(dispositionConflict.payload(), map[string]any{"disposition": "conflict"}) {
		t.Fatalf("disposition conflict = %#v", dispositionConflict.payload())
	}
	if fixture.countEvents("approval_resolved") != 1 {
		t.Fatalf("approval_resolved count = %d", fixture.countEvents("approval_resolved"))
	}
}

func TestInteractiveCoordinatorRejectsAllLiveRequestsOnShutdown(t *testing.T) {
	fixture := newCoordinatorFixture()
	outcome := make(chan permissionOutcome, 1)
	go func() {
		result, err := fixture.coordinator.HandleToolPermission(
			"Bash",
			map[string]any{"command": "pwd"},
			claudecli.ToolPermissionRequest{Ctx: context.Background()},
		)
		outcome <- permissionOutcome{result: result, err: err}
	}()
	request := fixture.waitForEvent(t, 0)
	requestID := stringValue(request.payload["requestId"])

	fixture.mu.Lock()
	fixture.coordinator.RejectAll(errors.New("session closed"))
	fixture.mu.Unlock()

	result := <-outcome
	if result.err == nil || result.err.Error() != "session closed" {
		t.Fatalf("error = %v", result.err)
	}
	fixture.mu.Lock()
	disposition := fixture.coordinator.Disposition("turn-1", requestID, nil)
	fixture.mu.Unlock()
	if !reflect.DeepEqual(disposition.payload(), map[string]any{"disposition": "superseded", "replayed": true}) {
		t.Fatalf("disposition = %#v", disposition.payload())
	}
}
