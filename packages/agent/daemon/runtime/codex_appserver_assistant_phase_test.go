package agentruntime

import (
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestCodexAppServerAssistantPhaseCommentaryThenFinal(t *testing.T) {
	t.Parallel()

	reducer, session, normalizer := newCodexAssistantPhaseFixture()

	started := reduceAppServerItem(t, reducer, session, normalizer, false, map[string]any{
		"type": "agentMessage", "id": "item-a", "text": "", "phase": "commentary",
	})
	if len(started) != 0 {
		t.Fatalf("started events = %#v, want none", started)
	}

	deltaA := reduceAppServerDelta(t, reducer, session, normalizer, "item-a", "Checking files")
	if len(deltaA) != 1 || assistantKind(deltaA) != assistantMessageKindCommentary {
		t.Fatalf("commentary delta = %#v", deltaA)
	}

	completedA := reduceAppServerItem(t, reducer, session, normalizer, true, map[string]any{
		"type": "agentMessage", "id": "item-a", "text": "Checking files.", "phase": "commentary",
	})
	if len(activityMessagesWithRole(completedA, activityshared.MessageRoleAssistant)) != 1 {
		t.Fatalf("commentary completed = %#v", completedA)
	}
	if assistantKind(completedA) != assistantMessageKindCommentary {
		t.Fatalf("commentary completed kind = %#v", assistantKind(completedA))
	}

	tool := reduceAppServerItem(t, reducer, session, normalizer, false, map[string]any{
		"type": "commandExecution", "id": "item-tool", "command": "ls", "cwd": "/workspace", "status": "inProgress",
		"commandActions": []any{},
	})
	if len(tool) == 0 {
		t.Fatal("tool started emitted no events")
	}

	deltaB := reduceAppServerDelta(t, reducer, session, normalizer, "item-b", "Here is the answer")
	if assistantKind(deltaB) != "" {
		t.Fatalf("final delta inherited commentary kind = %#v", deltaB)
	}
	completedB := reduceAppServerItem(t, reducer, session, normalizer, true, map[string]any{
		"type": "agentMessage", "id": "item-b", "text": "Here is the answer.", "phase": "final_answer",
	})
	if assistantKind(completedB) != assistantMessageKindFinal {
		t.Fatalf("final completed kind = %#v", assistantKind(completedB))
	}
	if completedB[0].EventID == deltaA[0].EventID {
		t.Fatalf("final item reused commentary message id %q", deltaA[0].EventID)
	}
}

func TestCodexAppServerAssistantPhaseCompletedOnly(t *testing.T) {
	t.Parallel()

	reducer, session, normalizer := newCodexAssistantPhaseFixture()
	events := reduceAppServerItem(t, reducer, session, normalizer, true, map[string]any{
		"type": "agentMessage", "id": "item-final", "text": "Only the answer.", "phase": "final_answer",
	})
	if len(events) != 1 || events[0].EventID != "item-final" {
		t.Fatalf("completed-only events = %#v, want stable item id", events)
	}
	if assistantKind(events) != assistantMessageKindFinal {
		t.Fatalf("completed-only kind = %#v", assistantKind(events))
	}
}

func TestCodexAppServerAssistantPhaseDuplicateCompletedDoesNotDuplicateBubble(t *testing.T) {
	t.Parallel()

	reducer, session, normalizer := newCodexAssistantPhaseFixture()
	first := reduceAppServerItem(t, reducer, session, normalizer, true, map[string]any{
		"type": "agentMessage", "id": "item-a", "text": "Hello.", "phase": "final_answer",
	})
	second := reduceAppServerItem(t, reducer, session, normalizer, true, map[string]any{
		"type": "agentMessage", "id": "item-a", "text": "Hello.", "phase": "final_answer",
	})
	if len(first) != 1 {
		t.Fatalf("first completed = %#v", first)
	}
	if len(second) != 1 || second[0].EventID != first[0].EventID {
		t.Fatalf("duplicate completed = %#v, want original id", second)
	}
}

func TestCodexAppServerAssistantDeltaUsesItemIdentity(t *testing.T) {
	t.Parallel()

	reducer, session, normalizer := newCodexAssistantPhaseFixture()
	events := reduceAppServerDelta(t, reducer, session, normalizer, "item-msg-1", "Hello")
	if len(events) != 1 || events[0].EventID != "item-msg-1" {
		t.Fatalf("delta events = %#v, want item id as message id", events)
	}
}

func newCodexAssistantPhaseFixture() (codexAppServerReducer, Session, *acpTurnNormalizer) {
	session := Session{
		AgentSessionID:    "agent-session-1",
		Provider:          ProviderCodex,
		ProviderSessionID: "thread-1",
		CWD:               "/workspace",
	}
	return newCodexAppServerReducer(&CodexAppServerAdapter{}), session, newACPTurnNormalizer()
}

func reduceAppServerItem(
	t *testing.T,
	reducer codexAppServerReducer,
	session Session,
	normalizer *acpTurnNormalizer,
	completed bool,
	item map[string]any,
) []activityshared.Event {
	t.Helper()
	method := appServerNotifyItemStarted
	if completed {
		method = appServerNotifyItemCompleted
	}
	return reducer.ReduceNotification(nil, session, "turn-1", acpMessage{
		Method: method,
		Params: mustJSONRawMessage(t, map[string]any{
			"threadId": session.ProviderSessionID,
			"turnId":   "turn-1",
			"item":     item,
		}),
	}, normalizer, nil).Events
}

func reduceAppServerDelta(
	t *testing.T,
	reducer codexAppServerReducer,
	session Session,
	normalizer *acpTurnNormalizer,
	itemID string,
	delta string,
) []activityshared.Event {
	t.Helper()
	return reducer.ReduceNotification(nil, session, "turn-1", acpMessage{
		Method: appServerNotifyAgentMessageDelta,
		Params: mustJSONRawMessage(t, map[string]any{
			"threadId": session.ProviderSessionID,
			"turnId":   "turn-1",
			"itemId":   itemID,
			"delta":    delta,
		}),
	}, normalizer, nil).Events
}
