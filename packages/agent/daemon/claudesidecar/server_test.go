package claudesidecar

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// serverSmokeConnection drives the Server exactly like the daemon drives a
// sidecar process: NDJSON requests in, versioned events out. This mirrors the
// desktop packaging protocol smoke (start, exec, close).
type serverSmokeConnection struct {
	writer  *io.PipeWriter
	scanner *bufio.Scanner
	done    chan error
}

func newServerSmokeConnection(t *testing.T, options ...ServerOption) *serverSmokeConnection {
	t.Helper()
	requestReader, requestWriter := io.Pipe()
	eventReader, eventWriter := io.Pipe()
	server := NewServer(eventWriter, options...)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(requestReader)
		_ = eventWriter.Close()
	}()
	scanner := bufio.NewScanner(eventReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &serverSmokeConnection{writer: requestWriter, scanner: scanner, done: done}
}

func (c *serverSmokeConnection) send(t *testing.T, request map[string]any) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := c.writer.Write(append(data, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

func (c *serverSmokeConnection) next(t *testing.T) Event {
	t.Helper()
	if !c.scanner.Scan() {
		t.Fatalf("event stream ended: %v", c.scanner.Err())
	}
	var event Event
	if err := json.Unmarshal(c.scanner.Bytes(), &event); err != nil {
		t.Fatalf("parse event %q: %v", c.scanner.Text(), err)
	}
	if event.Version != ProtocolVersion {
		t.Fatalf("event version = %d", event.Version)
	}
	return event
}

func (c *serverSmokeConnection) waitFor(t *testing.T, eventType string, id string) Event {
	t.Helper()
	for {
		event := c.next(t)
		if event.Type == eventType && (id == "" || event.ID == id) {
			return event
		}
		if event.Type == "error" {
			t.Fatalf("unexpected error event: %#v", event)
		}
	}
}

func TestServerProtocolSmokeStartExecClose(t *testing.T) {
	conn := newServerSmokeConnection(t, WithTestDriver())

	conn.send(t, map[string]any{
		"version": ProtocolVersion,
		"id":      "req-start",
		"type":    "start",
		"payload": map[string]any{
			"agentSessionId": "agent-session-1",
			"cwd":            t.TempDir(),
		},
	})
	started := conn.waitFor(t, "ok", "req-start")
	if stringValue(started.Payload["providerSessionId"]) == "" {
		t.Fatalf("start ok payload = %#v", started.Payload)
	}

	conn.send(t, map[string]any{
		"version": ProtocolVersion,
		"id":      "req-exec",
		"type":    "exec",
		"payload": map[string]any{
			"agentSessionId": "agent-session-1",
			"turnId":         "turn-1",
			"prompt":         "say hello",
		},
	})
	sawDelta := false
	sawCompleted := false
	for !sawCompleted {
		event := conn.next(t)
		switch event.Type {
		case "assistant_delta":
			if strings.Contains(stringValue(event.Payload["content"]), "Echo: say hello") {
				sawDelta = true
			}
		case "turn_completed":
			if event.Payload["turnId"] == "turn-1" {
				sawCompleted = true
			}
		case "error":
			t.Fatalf("unexpected error event: %#v", event)
		}
	}
	if !sawDelta {
		t.Fatal("assistant delta never streamed")
	}
	conn.waitFor(t, "ok", "req-exec")

	conn.send(t, map[string]any{
		"version": ProtocolVersion,
		"id":      "req-close",
		"type":    "close",
		"payload": map[string]any{"agentSessionId": "agent-session-1"},
	})
	conn.waitFor(t, "ok", "req-close")

	_ = conn.writer.Close()
	select {
	case err := <-conn.done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve never returned after input close")
	}
}

func TestServerRejectsUnknownSessionAndBadVersion(t *testing.T) {
	conn := newServerSmokeConnection(t, WithTestDriver())

	conn.send(t, map[string]any{
		"version": ProtocolVersion,
		"id":      "req-exec",
		"type":    "exec",
		"payload": map[string]any{"agentSessionId": "missing", "turnId": "turn-1"},
	})
	event := conn.next(t)
	if event.Type != "error" || !strings.Contains(stringValue(event.Payload["error"]), "session missing is not started") {
		t.Fatalf("event = %#v", event)
	}

	conn.send(t, map[string]any{"version": 1, "type": "exec"})
	event = conn.next(t)
	if event.Type != "error" || !strings.Contains(stringValue(event.Payload["error"]), "protocol version") {
		t.Fatalf("event = %#v", event)
	}
	_ = conn.writer.Close()
}
