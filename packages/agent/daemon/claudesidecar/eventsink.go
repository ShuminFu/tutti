package claudesidecar

import (
	"encoding/json"
	"io"
	"sync"
)

// EventSink serializes sidecar events onto one writer, stamping the protocol
// version. Writes are ordered under a mutex so concurrent emitters (request
// handling, query consumption, interactive callbacks) never interleave lines.
type EventSink struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewEventSink(writer io.Writer) *EventSink {
	return &EventSink{writer: writer}
}

func (s *EventSink) Emit(eventType string, id string, payload map[string]any) {
	event := Event{
		Version: ProtocolVersion,
		ID:      id,
		Type:    eventType,
		Payload: payload,
	}
	data, err := json.Marshal(event)
	if err != nil {
		fallback, fallbackErr := json.Marshal(Event{
			Version: ProtocolVersion,
			ID:      id,
			Type:    "error",
			Payload: map[string]any{"error": "sidecar event serialization failed: " + err.Error()},
		})
		if fallbackErr != nil {
			return
		}
		data = fallback
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.writer.Write(data)
}
