package claudesidecar

import (
	"reflect"
	"testing"
)

type recordedEvent struct {
	eventType string
	payload   map[string]any
}

func recordingEmitter(events *[]recordedEvent) Emitter {
	return func(eventType string, _ string, payload map[string]any) {
		*events = append(*events, recordedEvent{eventType: eventType, payload: payload})
	}
}

type fakeContextUsageQuery struct {
	fetches []func() (map[string]any, error)
}

func (f *fakeContextUsageQuery) GetContextUsage() (map[string]any, error) {
	fetch := f.fetches[0]
	f.fetches = f.fetches[1:]
	return fetch()
}

func TestCompactionFailureCollapsesDuplicatedProviderReason(t *testing.T) {
	var events []recordedEvent
	tracker := NewCompactionTracker(CompactionTrackerOptions{
		ActiveTurnID:        func() string { return "turn-1" },
		EnsureActive:        func(string) {},
		ClearPendingOrphans: func() {},
		GetQuery:            func() contextUsageQuery { return nil },
		Emit:                recordingEmitter(&events),
	})

	tracker.HandleSystemMessage("status", map[string]any{"status": "compacting"})
	tracker.HandleSystemMessage("status", map[string]any{
		"compact_result": "failed",
		"compact_error":  "Not enough messages to compact.Not enough messages to compact.",
	})

	if len(events) < 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[1].payload["content"] != "Compacting failed: Not enough messages to compact." {
		t.Fatalf("failure content = %#v", events[1].payload["content"])
	}
}

func TestNewerContextUsageRequestSupersedesOlderDelayedSnapshot(t *testing.T) {
	var events []recordedEvent
	// pendingApplies captures deferred snapshot applications so the test can
	// resolve them out of order, the way the TS test resolves promises.
	type pendingApply struct {
		fetch func() (map[string]any, error)
		apply func(map[string]any, error)
	}
	var pending []pendingApply
	// The newer snapshot fetches first below, so the first response belongs to
	// the newer request (222) and the second to the delayed older one (111).
	responses := []map[string]any{
		{"totalTokens": float64(222), "maxTokens": float64(200000)},
		{"totalTokens": float64(111), "maxTokens": float64(200000)},
	}
	query := &fakeContextUsageQuery{}
	for index := range responses {
		response := responses[index]
		query.fetches = append(query.fetches, func() (map[string]any, error) {
			return response, nil
		})
	}
	tracker := NewCompactionTracker(CompactionTrackerOptions{
		ActiveTurnID:        func() string { return "turn-1" },
		EnsureActive:        func(string) {},
		ClearPendingOrphans: func() {},
		GetQuery:            func() contextUsageQuery { return query },
		Emit:                recordingEmitter(&events),
		RunAsync: func(fetch func() (map[string]any, error), apply func(map[string]any, error)) {
			pending = append(pending, pendingApply{fetch: fetch, apply: apply})
		},
	})

	var outcomes []string
	tracker.EmitContextUsageSnapshot("turn-1", nil, nil, func(outcome string) {
		outcomes = append(outcomes, "older:"+outcome)
	})
	tracker.EmitContextUsageSnapshot("turn-1", nil, nil, func(outcome string) {
		outcomes = append(outcomes, "newer:"+outcome)
	})
	// Resolve the newer snapshot first, then the older one.
	pending[1].apply(pending[1].fetch())
	pending[0].apply(pending[0].fetch())

	if !reflect.DeepEqual(outcomes, []string{"newer:emitted", "older:stale"}) {
		t.Fatalf("outcomes = %#v", outcomes)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	contextWindow := recordValue(events[0].payload["contextWindow"])
	expected := map[string]any{
		"usedTokens":            float64(222),
		"totalTokens":           float64(200000),
		"compactsAutomatically": false,
	}
	if !reflect.DeepEqual(contextWindow, expected) {
		t.Fatalf("contextWindow = %#v", contextWindow)
	}
}
