package claudesidecar

import (
	"strings"
	"testing"
)

func TestSidecarProtocolAcceptsCurrentVersion(t *testing.T) {
	request, err := ParseRequest([]byte(`{"version":2,"id":"request-1","type":"exec","payload":{"turnId":"turn-1"}}`))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if request.Version != ProtocolVersion || request.ID != "request-1" || request.Type != "exec" {
		t.Fatalf("request = %#v", request)
	}
	if request.Payload["turnId"] != "turn-1" {
		t.Fatalf("payload = %#v", request.Payload)
	}
}

func TestSidecarProtocolAcceptsStopTaskRequests(t *testing.T) {
	request, err := ParseRequest([]byte(`{"version":2,"type":"stop_task","payload":{"agentSessionId":"session-1","taskId":"task-1"}}`))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if request.Type != "stop_task" {
		t.Fatalf("type = %q", request.Type)
	}
}

func TestSidecarProtocolRejectsMissingAndUnknownVersions(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"type":"exec"}`)); err == nil || !strings.Contains(err.Error(), "protocol version missing") {
		t.Fatalf("missing version error = %v", err)
	}
	if _, err := ParseRequest([]byte(`{"version":1,"type":"exec"}`)); err == nil || !strings.Contains(err.Error(), "protocol version 1") {
		t.Fatalf("unknown version error = %v", err)
	}
}

func TestSidecarProtocolRejectsInvalidIDAndPayload(t *testing.T) {
	if _, err := ParseRequest([]byte(`{"version":2,"type":"exec","id":"  "}`)); err == nil {
		t.Fatal("blank id accepted")
	}
	if _, err := ParseRequest([]byte(`{"version":2,"type":"exec","payload":[1]}`)); err == nil {
		t.Fatal("array payload accepted")
	}
	if _, err := ParseRequest([]byte(`{"version":2,"type":"unknown"}`)); err == nil {
		t.Fatal("unknown type accepted")
	}
}
