package agentruntime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func detachedCodexChild(t *testing.T) (*CodexAppServerAdapter, Session, *codexAppServerThreadContext) {
	t.Helper()
	adapter := NewCodexAppServerAdapter(nil)
	session := Session{AgentSessionID: "root-session", Provider: ProviderCodex, ProviderSessionID: "root-thread", CWD: "/workspace"}
	adapter.storeSession(session.AgentSessionID, &codexAppServerSession{threadID: session.ProviderSessionID, pendingRequests: make(map[string]*pendingInteractiveRequest)})
	adapter.rememberAppServerChildThreads(session, session.ProviderSessionID, session.AgentSessionID, "root-turn", session.AgentSessionID, "root-turn", map[string]any{"type": "collabAgentToolCall", "id": "spawn-child", "tool": "spawnAgent", "receiverThreadIds": []any{"child-thread"}})
	child, ok := adapter.appServerChildThread(session.AgentSessionID, "child-thread")
	if !ok {
		t.Fatal("missing child")
	}
	return adapter, session, child
}

func TestCodexDetachedChildInteraction(t *testing.T) {
	for _, test := range []struct {
		name, method string
		outOfBand    bool
	}{
		{"approval", appServerMethodCommandApproval, false},
		{"question", appServerMethodRequestUserInput, false},
		{"provider resolution", appServerMethodCommandApproval, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter, session, child := detachedCodexChild(t)
			conn := newAppServerCaptureConn()
			client := newCodexAppServerClient(conn)
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mu sync.Mutex
			var emitted []activityshared.Event
			adapter.SetSessionEventSink(func(id string, events []activityshared.Event) {
				mu.Lock()
				defer mu.Unlock()
				if id != session.AgentSessionID {
					t.Errorf("sink owner = %s", id)
				}
				emitted = append(emitted, events...)
			})
			formerRoot := &codexAppServerActiveTurn{turnID: "root-turn", emit: func([]activityshared.Event) { t.Error("child interaction reached finalized root emitter") }}
			formerRoot.settleFinalized.Store(true)
			adapter.beginActiveTurn(session.AgentSessionID, formerRoot)
			params := map[string]any{"threadId": "child-thread", "turnId": "child-provider-turn", "itemId": "child-command", "command": "pwd", "questions": []any{map[string]any{"id": "question", "question": "Continue?"}}}
			message := acpMessage{ID: json.RawMessage(`"child-request"`), Method: test.method, Params: mustJSONRawMessage(t, params)}
			if _, err := adapter.handleAppServerMessage(ctx, client, session, "", message, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			if adapter.getPendingRequest(child.agentSessionID, child.turnID, "child-request") == nil {
				t.Fatal("missing child pending request")
			}
			// A new root emitter must not inherit the previous root's child response.
			newRoot := &codexAppServerActiveTurn{turnID: "new-root-turn", emit: func([]activityshared.Event) { t.Error("child response reached newer root emitter") }}
			adapter.endActiveTurn(session.AgentSessionID, formerRoot)
			adapter.beginActiveTurn(session.AgentSessionID, newRoot)
			if test.outOfBand {
				_, err := adapter.handleAppServerMessage(ctx, client, session, "new-root-turn", acpMessage{Method: appServerNotifyServerRequestResolved, Params: mustJSONRawMessage(t, map[string]any{"threadId": "child-thread", "requestId": "child-request"})}, newACPTurnNormalizer(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := adapter.SubmitInteractive(ctx, session, SubmitInteractiveInput{AgentSessionID: child.agentSessionID, TurnID: child.turnID, RequestID: "child-request", OptionID: "approve", Payload: map[string]any{"answersByQuestionId": map[string]any{"question": "yes"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			waitForCondition(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				if test.outOfBand {
					return len(eventsOfType(emitted, activityshared.EventInteractionSuperseded)) > 0
				}
				return len(eventsOfType(emitted, activityshared.EventCallCompleted)) > 0
			})
			mu.Lock()
			events := append([]activityshared.Event(nil), emitted...)
			mu.Unlock()
			for _, event := range events {
				if event.AgentSessionID != child.agentSessionID || event.Payload.TurnID != child.turnID || event.RootTurnID != "root-turn" || event.ParentToolCallID != "spawn-child" {
					t.Fatalf("wrong child identity: %#v", event)
				}
			}
			responses := conn.responses(t)
			if test.outOfBand {
				if len(responses) != 0 {
					t.Fatalf("provider-resolved request was answered again: %#v", responses)
				}
			} else {
				if len(responses) != 1 {
					t.Fatalf("responses = %#v", responses)
				}
				var result map[string]any
				if err := json.Unmarshal(responses[0].Result, &result); err != nil {
					t.Fatal(err)
				}
				if test.method == appServerMethodCommandApproval && result["decision"] != "accept" {
					t.Fatalf("result = %#v", result)
				}
				if test.method == appServerMethodRequestUserInput && len(payloadObject(result["answers"])) != 1 {
					t.Fatalf("answers = %#v", result)
				}
			}
		})
	}
}

func TestCodexDetachedChildInteractionRejectsUnavailableOwner(t *testing.T) {
	for _, state := range []string{"unknown", "completed", "failed", "canceled", "root"} {
		t.Run(state, func(t *testing.T) {
			adapter, session, child := detachedCodexChild(t)
			conn := newAppServerCaptureConn()
			client := newCodexAppServerClient(conn)
			defer client.Close()
			threadID := "child-thread"
			switch state {
			case "unknown":
				threadID = "unknown-thread"
			case "root":
				threadID = session.ProviderSessionID
			default:
				newCodexAppServerReducer(adapter).ReduceNotification(client, session, "", acpMessage{Method: appServerNotifyTurnCompleted, Params: mustJSONRawMessage(t, map[string]any{"threadId": threadID, "turn": map[string]any{"id": "child-provider-turn", "status": state}})}, nil, nil)
			}
			adapter.SetSessionEventSink(func(string, []activityshared.Event) { t.Error("rejected request emitted activity") })
			_, err := adapter.handleAppServerMessage(context.Background(), client, session, "", acpMessage{ID: json.RawMessage(`"request"`), Method: appServerMethodCommandApproval, Params: mustJSONRawMessage(t, map[string]any{"threadId": threadID, "turnId": "child-provider-turn", "itemId": "command"})}, nil, nil, nil)
			if err == nil {
				t.Fatal("unavailable owner was accepted")
			}
			if adapter.getPendingRequest(child.agentSessionID, child.turnID, "request") != nil {
				t.Fatal("rejected request became pending")
			}
			responses := conn.responses(t)
			if len(responses) != 1 || responses[0].Error == nil {
				t.Fatalf("rejection = %#v", responses)
			}
		})
	}
}

func TestCodexDetachedChildInteractionThroughSessionReader(t *testing.T) {
	adapter, transport, session := startedAppServerAdapter(t)
	adapter.rememberAppServerChildThreads(session, "codex-thread-1", session.AgentSessionID, "root-turn", session.AgentSessionID, "root-turn", map[string]any{"type": "collabAgentToolCall", "id": "spawn-child", "receiverThreadIds": []any{"child-thread"}})
	child, _ := adapter.appServerChildThread(session.AgentSessionID, "child-thread")
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "text", Text: "parent"}}, "", "root-turn", nil, nil); err != nil {
		t.Fatal(err)
	}
	if adapter.sessionActiveTurn(session.AgentSessionID) != nil {
		t.Fatal("root did not detach")
	}
	var mu sync.Mutex
	var emitted []activityshared.Event
	adapter.SetSessionEventSink(func(_ string, events []activityshared.Event) {
		mu.Lock()
		emitted = append(emitted, events...)
		mu.Unlock()
	})
	transport.conn.sendJSON(map[string]any{"id": "detached-request", "method": appServerMethodRequestUserInput, "params": map[string]any{"threadId": "child-thread", "turnId": "child-provider-turn", "itemId": "question-item", "questions": []any{map[string]any{"id": "q", "question": "Continue?"}}}})
	waitForCondition(t, func() bool {
		return adapter.getPendingRequest(child.agentSessionID, child.turnID, "detached-request") != nil
	})
	transport.conn.notify(appServerNotifyServerRequestResolved, map[string]any{"threadId": "child-thread", "requestId": "detached-request"})
	waitForCondition(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(eventsOfType(emitted, activityshared.EventInteractionSuperseded)) == 1
	})
	mu.Lock()
	defer mu.Unlock()
	for _, event := range emitted {
		if event.AgentSessionID != child.agentSessionID || event.Payload.TurnID != child.turnID || event.RootTurnID != "root-turn" {
			t.Fatalf("misrouted reader event: %#v", event)
		}
	}
}
