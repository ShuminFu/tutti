package claudesidecar

import (
	"sync"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar/claudecli"
)

// fakeSessionQuery scripts the query surface for runtime tests, the way the
// TypeScript sessionRuntime tests substitute a query factory.
type fakeSessionQuery struct {
	mu         sync.Mutex
	messages   chan map[string]any
	written    []map[string]any
	initResult map[string]any
	interrupts int
	closed     bool
}

func newFakeSessionQuery(initResult map[string]any) *fakeSessionQuery {
	return &fakeSessionQuery{
		messages:   make(chan map[string]any, 64),
		initResult: initResult,
	}
}

func (f *fakeSessionQuery) Messages() <-chan map[string]any { return f.messages }
func (*fakeSessionQuery) ReadError() error                  { return nil }
func (f *fakeSessionQuery) InitializationResult() (map[string]any, error) {
	return f.initResult, nil
}
func (f *fakeSessionQuery) WriteUserMessage(message map[string]any) error {
	f.mu.Lock()
	f.written = append(f.written, message)
	f.mu.Unlock()
	return nil
}
func (*fakeSessionQuery) EndInput() {}
func (f *fakeSessionQuery) Interrupt() error {
	f.mu.Lock()
	f.interrupts++
	f.mu.Unlock()
	return nil
}
func (*fakeSessionQuery) StopTask(string) error                  { return nil }
func (*fakeSessionQuery) SetPermissionMode(string) error         { return nil }
func (*fakeSessionQuery) SetModel(string) error                  { return nil }
func (*fakeSessionQuery) ApplyFlagSettings(map[string]any) error { return nil }
func (*fakeSessionQuery) GetContextUsage() (map[string]any, error) {
	return map[string]any{"totalTokens": float64(1200), "maxTokens": float64(200000)}, nil
}
func (f *fakeSessionQuery) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	close(f.messages)
}

func (f *fakeSessionQuery) feed(message map[string]any) {
	f.messages <- message
}

func (f *fakeSessionQuery) waitForWritten(t *testing.T, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.written) >= count {
			written := append([]map[string]any(nil), f.written...)
			f.mu.Unlock()
			return written
		}
		f.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("prompt %d never written", count)
	return nil
}

type runtimeFixture struct {
	mu      sync.Mutex
	events  []recordedEvent
	queries []*fakeSessionQuery
	runtime *SessionRuntime
}

func (f *runtimeFixture) emit(eventType string, _ string, payload map[string]any) {
	f.mu.Lock()
	f.events = append(f.events, recordedEvent{eventType: eventType, payload: payload})
	f.mu.Unlock()
}

func (f *runtimeFixture) waitForEventType(t *testing.T, eventType string) recordedEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, event := range f.events {
			if event.eventType == eventType {
				f.mu.Unlock()
				return event
			}
		}
		f.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("event %q never emitted; events = %#v", eventType, f.events)
	return recordedEvent{}
}

func (f *runtimeFixture) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, 0, len(f.events))
	for _, event := range f.events {
		types = append(types, event.eventType)
	}
	return types
}

func (f *runtimeFixture) countEventType(eventType string) int {
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

func (f *runtimeFixture) latestQuery() *fakeSessionQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

func newRuntimeFixture(t *testing.T, initResult map[string]any) *runtimeFixture {
	t.Helper()
	fixture := &runtimeFixture{}
	fixture.runtime = NewSessionRuntime(SessionRuntimeConfig{
		ProviderSessionID: "provider-session-1",
		CWD:               t.TempDir(),
		Settings:          &SessionSettings{PermissionModeID: "default"},
		ClaudeOptions:     sidecarClaudeOptionsFromPayload(map[string]any{}),
		Emit:              fixture.emit,
		QueryFactory: func(_ *claudecli.Options) (SessionQuery, error) {
			query := newFakeSessionQuery(initResult)
			fixture.mu.Lock()
			fixture.queries = append(fixture.queries, query)
			fixture.mu.Unlock()
			return query, nil
		},
	})
	return fixture
}

func TestSessionRuntimeStartEmitsSessionStartedWithModelOptions(t *testing.T) {
	fixture := newRuntimeFixture(t, map[string]any{
		"models": []any{
			map[string]any{"value": "default", "displayName": "Default"},
			map[string]any{"value": "haiku", "displayName": "Haiku"},
		},
	})
	if err := fixture.runtime.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	started := fixture.waitForEventType(t, "session_started")
	if started.payload["providerSessionId"] != "provider-session-1" {
		t.Fatalf("session_started = %#v", started.payload)
	}
	cursor := recordValue(started.payload["resumeCursor"])
	if cursor["resume"] != "provider-session-1" || cursor["kind"] != "claude-agent-sdk" {
		t.Fatalf("resumeCursor = %#v", cursor)
	}
	options, _ := started.payload["configOptions"].([]map[string]any)
	if len(options) != 1 || options[0]["id"] != "model" {
		t.Fatalf("configOptions = %#v", started.payload["configOptions"])
	}
}

func TestSessionRuntimeExecStreamsAssistantOutputAndSettlesTurn(t *testing.T) {
	fixture := newRuntimeFixture(t, map[string]any{})
	if err := fixture.runtime.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fixture.runtime.Exec("turn-1", "say hello", nil, "", nil)
	written := fixture.latestQuery().waitForWritten(t, 1)
	promptUUID := stringValue(written[0]["uuid"])
	if promptUUID == "" {
		t.Fatalf("prompt uuid missing: %#v", written[0])
	}
	query := fixture.latestQuery()
	query.feed(map[string]any{
		"type":       "user",
		"uuid":       promptUUID,
		"session_id": "provider-session-1",
		"message":    map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "say hello"}}},
	})
	query.feed(map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "message-1"},
		},
	})
	query.feed(map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":          "content_block_start",
			"index":         float64(0),
			"content_block": map[string]any{"type": "text"},
		},
	})
	query.feed(map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": float64(0),
			"delta": map[string]any{"type": "text_delta", "text": "hello there"},
		},
	})
	query.feed(map[string]any{
		"type": "assistant",
		"uuid": "assistant-uuid-1",
		"message": map[string]any{
			"id":      "message-1",
			"content": []any{map[string]any{"type": "text", "text": "hello there"}},
		},
	})
	query.feed(map[string]any{"type": "result", "subtype": "success"})

	completed := fixture.waitForEventType(t, "turn_completed")
	if completed.payload["turnId"] != "turn-1" || completed.payload["stopReason"] != "end_turn" {
		t.Fatalf("turn_completed = %#v", completed.payload)
	}
	assistant := fixture.waitForEventType(t, "assistant_completed")
	if assistant.payload["content"] != "hello there" || assistant.payload["turnId"] != "turn-1" {
		t.Fatalf("assistant_completed = %#v", assistant.payload)
	}
	if fixture.countEventType("assistant_completed") != 1 {
		t.Fatalf("assistant_completed count = %d; events=%v", fixture.countEventType("assistant_completed"), fixture.eventTypes())
	}
	usage := fixture.waitForEventType(t, "usage_updated")
	if usage.payload["turnId"] != "turn-1" {
		t.Fatalf("usage_updated = %#v", usage.payload)
	}
	if err := fixture.runtime.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionRuntimeCancelInterruptsAndQuarantinesCanceledTail(t *testing.T) {
	fixture := newRuntimeFixture(t, map[string]any{})
	if err := fixture.runtime.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fixture.runtime.Exec("turn-1", "long task", nil, "", nil)
	firstQuery := fixture.latestQuery()
	written := firstQuery.waitForWritten(t, 1)
	firstQuery.feed(map[string]any{
		"type":       "user",
		"uuid":       stringValue(written[0]["uuid"]),
		"session_id": "provider-session-1",
		"message":    map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "long task"}}},
	})
	deadlineActivate := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadlineActivate) && fixture.runtime.ActiveTurnID() != "turn-1" {
		time.Sleep(2 * time.Millisecond)
	}
	if fixture.runtime.ActiveTurnID() != "turn-1" {
		t.Fatal("turn-1 never activated")
	}

	canceled, err := fixture.runtime.Cancel("")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !canceled {
		t.Fatal("Cancel reported no active turn")
	}
	turnCanceled := fixture.waitForEventType(t, "turn_canceled")
	if turnCanceled.payload["turnId"] != "turn-1" {
		t.Fatalf("turn_canceled = %#v", turnCanceled.payload)
	}
	firstQuery.mu.Lock()
	interrupts := firstQuery.interrupts
	firstQuery.mu.Unlock()
	if interrupts != 1 {
		t.Fatalf("interrupts = %d", interrupts)
	}

	// The next exec creates a fresh generation whose canceled-tail quarantine
	// swallows the replayed task tail and orphan result during resume.
	fixture.runtime.Exec("turn-2", "next", nil, "", nil)
	deadlineQuery := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadlineQuery) {
		fixture.mu.Lock()
		queryCount := len(fixture.queries)
		fixture.mu.Unlock()
		if queryCount >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	secondQuery := fixture.latestQuery()
	if secondQuery == firstQuery {
		t.Fatal("second exec reused the canceled query")
	}
	secondWritten := secondQuery.waitForWritten(t, 1)
	secondQuery.feed(map[string]any{
		"type":    "system",
		"subtype": "task_notification",
		"task_id": "task-1",
		"status":  "completed",
	})
	secondQuery.feed(map[string]any{"type": "result", "subtype": "success"})
	secondQuery.feed(map[string]any{
		"type":       "user",
		"uuid":       stringValue(secondWritten[0]["uuid"]),
		"session_id": "provider-session-1",
		"message":    map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "next"}}},
	})
	secondQuery.feed(map[string]any{
		"type": "assistant",
		"uuid": "assistant-uuid-2",
		"message": map[string]any{
			"id":      "message-2",
			"content": []any{map[string]any{"type": "text", "text": "done"}},
		},
	})
	secondQuery.feed(map[string]any{"type": "result", "subtype": "success"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fixture.mu.Lock()
		completed := ""
		for _, event := range fixture.events {
			if event.eventType == "turn_completed" {
				completed = stringValue(event.payload["turnId"])
			}
		}
		fixture.mu.Unlock()
		if completed == "turn-2" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var completedTurns []string
	for _, event := range fixture.events {
		if event.eventType == "turn_completed" {
			completedTurns = append(completedTurns, stringValue(event.payload["turnId"]))
		}
	}
	if len(completedTurns) != 1 || completedTurns[0] != "turn-2" {
		t.Fatalf("completed turns = %#v", completedTurns)
	}
}

func TestSessionRuntimeExecAfterCloseFailsTurn(t *testing.T) {
	fixture := newRuntimeFixture(t, map[string]any{})
	if err := fixture.runtime.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := fixture.runtime.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	fixture.runtime.Exec("turn-9", "hello", nil, "", nil)
	failed := fixture.waitForEventType(t, "turn_failed")
	if failed.payload["turnId"] != "turn-9" || failed.payload["error"] != "Claude SDK query is closed" {
		t.Fatalf("turn_failed = %#v", failed.payload)
	}
}
