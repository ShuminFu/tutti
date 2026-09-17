package storesqlite

import (
	"context"
	"testing"
)

func TestRecordTurnTransitionCompletesStreamingAssistantText(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-1", AgentSessionID: "session-1",
		Origin: "runtime", Provider: "codex", OccurredAtUnixMS: 1,
	}); err != nil {
		t.Fatalf("ReportSessionState: %v", err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-1", AgentSessionID: "session-1", TurnID: "turn-1",
		Phase: TurnPhaseSubmitted, OccurredAtUnixMS: 2,
	}); err != nil || !accepted {
		t.Fatalf("submit turn accepted=%v error=%v", accepted, err)
	}
	if _, err := store.ReportSessionMessages(ctx, SessionMessageReport{
		WorkspaceID: "ws-1", AgentSessionID: "session-1", Origin: "runtime",
		Messages: []MessageUpdate{{
			MessageID: "msg-final", TurnID: "turn-1",
			Role: "assistant", Kind: "text", Status: "streaming",
			Payload:          map[string]any{"text": "the real reply"},
			OccurredAtUnixMS: 3,
		}},
	}); err != nil {
		t.Fatalf("ReportSessionMessages: %v", err)
	}

	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-1", AgentSessionID: "session-1", TurnID: "turn-1",
		Phase: TurnPhaseSettled, Outcome: TurnOutcomeCompleted, OccurredAtUnixMS: 4,
	}); err != nil || !accepted {
		t.Fatalf("settle turn accepted=%v error=%v", accepted, err)
	}

	page, ok, err := store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-1", AgentSessionID: "session-1", Limit: 10,
	})
	if err != nil || !ok {
		t.Fatalf("ListSessionMessages ok=%v error=%v", ok, err)
	}
	found := false
	for _, message := range page.Messages {
		if message.MessageID != "msg-final" {
			continue
		}
		found = true
		if message.Status != "completed" {
			t.Fatalf("settled assistant status = %q, want completed", message.Status)
		}
		if message.CompletedAtUnixMS == 0 {
			t.Fatal("settled assistant completed_at was left at 0")
		}
	}
	if !found {
		t.Fatalf("messages = %#v, want msg-final", page.Messages)
	}
}
