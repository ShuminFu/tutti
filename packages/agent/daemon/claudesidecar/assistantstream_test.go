package claudesidecar

import (
	"reflect"
	"testing"
)

func eventTypeAndContent(events []recordedEvent) [][2]any {
	result := make([][2]any, 0, len(events))
	for _, event := range events {
		result = append(result, [2]any{event.eventType, event.payload["content"]})
	}
	return result
}

func TestAssistantStreamKeepsDeltaAndCompletionOnOneMessage(t *testing.T) {
	var events []recordedEvent
	projector := NewAssistantStreamProjector(func() string { return "turn-1" }, recordingEmitter(&events))

	index := 0
	projector.SetMessageBase("message-1")
	projector.AppendDelta(&index, "assistant", "hello")
	if !projector.CompleteIndex(&index) {
		t.Fatal("CompleteIndex returned false")
	}

	expected := [][2]any{
		{"assistant_delta", "hello"},
		{"assistant_completed", "hello"},
	}
	if !reflect.DeepEqual(eventTypeAndContent(events), expected) {
		t.Fatalf("events = %#v", events)
	}
	if events[0].payload["messageId"] != events[1].payload["messageId"] {
		t.Fatalf("messageId mismatch: %#v", events)
	}
}

func TestAssistantFallbackReusesStreamedPrefix(t *testing.T) {
	var events []recordedEvent
	projector := NewAssistantStreamProjector(func() string { return "turn-1" }, recordingEmitter(&events))

	index := 0
	projector.SetMessageBase("message-1")
	projector.AppendDelta(&index, "assistant", "hel")
	projector.CompleteContent("assistant", "message-1", "hello", map[string]struct{}{})

	expected := [][2]any{
		{"assistant_delta", "hel"},
		{"assistant_delta", "lo"},
		{"assistant_completed", "hello"},
	}
	if !reflect.DeepEqual(eventTypeAndContent(events), expected) {
		t.Fatalf("events = %#v", events)
	}
}

func TestAssistantStreamResetDropsStaleIndexes(t *testing.T) {
	var events []recordedEvent
	projector := NewAssistantStreamProjector(func() string { return "turn-2" }, recordingEmitter(&events))

	index := 0
	projector.Start(&index, "thinking")
	projector.Reset()

	if projector.CompleteIndex(&index) {
		t.Fatal("CompleteIndex found stale segment after reset")
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v", events)
	}
}
