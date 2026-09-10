package agentruntime

import (
	"context"
	"encoding/json"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestStandardACPAdapterGuideActiveTurn(t *testing.T) {
	transport := newScriptedACPTransport()
	transport.conn.supportsSteering = true

	adapter := &standardACPAdapter{
		config: standardACPConfig{
			provider:    "test-provider",
			adapterName: "test-acp",
		},
		transport: transport,
		sessions:  make(map[string]*standardACPSession),
	}

	session := testSession()
	ctx := context.Background()

	client := newACPClientWithStderrMessageMapper(transport.conn, nil)

	acpSession := &standardACPSession{
		providerSessionID: "test-session-1",
		client:            client,
	}
	adapter.sessions[session.AgentSessionID] = acpSession

	guidanceContent := []PromptContentBlock{
		{Type: "text", Text: "Please add error handling"},
	}
	displayPrompt := "Please add error handling"
	turnID := "turn-guidance-1"

	var emittedEvents []activityshared.Event
	emitSink := func(events []activityshared.Event) {
		emittedEvents = append(emittedEvents, events...)
	}

	guidanceEvents, guidanceErr := adapter.GuideActiveTurn(
		ctx,
		session,
		guidanceContent,
		displayPrompt,
		turnID,
		emitSink,
		nil,
	)

	if guidanceErr != nil {
		t.Logf("GuideActiveTurn returned error (expected in mock): %v", guidanceErr)
	}

	if len(guidanceEvents) == 0 {
		t.Fatal("GuideActiveTurn returned no events")
	}

	foundGuidanceEvent := false
	for _, evt := range guidanceEvents {
		if evt.Payload.Metadata != nil {
			if evt.Payload.Metadata["guidance"] == true && evt.Payload.Metadata["steered"] == true {
				foundGuidanceEvent = true
				break
			}
		}
	}
	if !foundGuidanceEvent {
		t.Error("GuideActiveTurn events missing guidance:true and steered:true markers")
	}

	transport.conn.mu.Lock()
	sentMessages := transport.conn.sent
	transport.conn.mu.Unlock()

	foundSteeringCall := false
	for _, rawMsg := range sentMessages {
		var msg struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}
		if msg.Method == acpMethodSteering {
			foundSteeringCall = true
			if msg.Params["sessionId"] == nil {
				t.Error("steering request missing sessionId")
			}
			if msg.Params["prompt"] == nil {
				t.Error("steering request missing prompt")
			}
			meta, hasMeta := msg.Params["_meta"].(map[string]any)
			if !hasMeta {
				t.Error("steering request missing _meta")
			} else {
				steering, hasSteering := meta["steering"].(map[string]any)
				if !hasSteering {
					t.Error("steering request missing _meta.steering")
				} else if steering["idleBehavior"] != "promptRequired" {
					t.Errorf("steering idleBehavior = %v, want promptRequired", steering["idleBehavior"])
				}
			}
			break
		}
	}
	if !foundSteeringCall {
		t.Error("_session/steering method was not called")
	}
}

func TestStandardACPAdapterGuideActiveTurnNoSession(t *testing.T) {
	adapter := &standardACPAdapter{
		config: standardACPConfig{
			provider:    "test-provider",
			adapterName: "test-acp",
		},
		sessions: make(map[string]*standardACPSession),
	}

	session := testSession()
	ctx := context.Background()

	guidanceContent := []PromptContentBlock{
		{Type: "text", Text: "Test guidance"},
	}

	_, err := adapter.GuideActiveTurn(
		ctx,
		session,
		guidanceContent,
		"Test guidance",
		"turn-1",
		nil,
		nil,
	)

	if err != ErrSessionDisconnected {
		t.Errorf("GuideActiveTurn without session = %v, want ErrSessionDisconnected", err)
	}
}
