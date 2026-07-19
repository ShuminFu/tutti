package claudesidecar

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolVersion is the sidecar wire protocol version. It must stay in sync
// with claude_sdk_protocol.go in the daemon runtime and (while it exists)
// src/protocol.ts in the TypeScript sidecar.
const ProtocolVersion = 2

// Request is one NDJSON request envelope from the daemon.
type Request struct {
	Version int            `json:"version"`
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Event is one NDJSON event envelope emitted to the daemon.
type Event struct {
	Version int            `json:"version"`
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Emitter emits a sidecar event; the version field is stamped by the sink.
type Emitter func(eventType string, id string, payload map[string]any)

var requestTypes = map[string]struct{}{
	"start":                   {},
	"exec":                    {},
	"guide":                   {},
	"cancel":                  {},
	"stop_task":               {},
	"submit_interactive":      {},
	"interactive_disposition": {},
	"apply_settings":          {},
	"close":                   {},
}

// ParseRequest validates one request line the way protocol.ts does.
func ParseRequest(line []byte) (Request, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return Request{}, err
	}
	if raw == nil {
		return Request{}, fmt.Errorf("sidecar request must be an object")
	}
	version, ok := raw["version"]
	if !ok || numberValue(version) != ProtocolVersion {
		return Request{}, fmt.Errorf("unsupported sidecar protocol version %s", versionForError(version))
	}
	requestType, _ := raw["type"].(string)
	if _, valid := requestTypes[requestType]; !valid {
		return Request{}, fmt.Errorf("unsupported request type %s", requestTypeForError(raw["type"]))
	}
	request := Request{Version: ProtocolVersion, Type: requestType}
	if id, present := raw["id"]; present {
		text, isString := id.(string)
		if !isString || strings.TrimSpace(text) == "" {
			return Request{}, fmt.Errorf("sidecar request id must be a non-empty string")
		}
		request.ID = text
	}
	if payload, present := raw["payload"]; present {
		record, isRecord := payload.(map[string]any)
		if !isRecord {
			return Request{}, fmt.Errorf("sidecar request payload must be an object")
		}
		request.Payload = record
	}
	return request, nil
}

func versionForError(value any) string {
	if value == nil {
		return "missing"
	}
	return fmt.Sprintf("%v", value)
}

func requestTypeForError(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
