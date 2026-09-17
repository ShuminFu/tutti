package storesqlite

import (
	"context"
	"testing"
)

func TestReportSessionStatePersistsLastTurnTokensOnCompletedAssistantMessage(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-usage", AgentSessionID: "session-usage", Origin: "runtime",
		Provider: "claude-code", Status: "running", OccurredAtUnixMS: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-usage", AgentSessionID: "session-usage", TurnID: "turn-1",
		Phase: TurnPhaseRunning, OccurredAtUnixMS: 101,
	}); err != nil || !accepted {
		t.Fatalf("RecordTurnTransition() accepted=%v error=%v", accepted, err)
	}
	if result, err := store.ReportSessionMessages(ctx, SessionMessageReport{
		WorkspaceID: "ws-usage", AgentSessionID: "session-usage", Origin: "runtime", Provider: "claude-code",
		Messages: []MessageUpdate{{
			MessageID: "assistant-1", TurnID: "turn-1", Role: "assistant", Kind: "text",
			Status: "completed", Payload: map[string]any{"text": "done"}, OccurredAtUnixMS: 110,
		}},
	}); err != nil || result.AcceptedCount != 1 {
		t.Fatalf("ReportSessionMessages() result=%#v error=%v", result, err)
	}

	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-usage", AgentSessionID: "session-usage", Origin: "runtime",
		Provider: "claude-code", Status: "ready", OccurredAtUnixMS: 120,
		RuntimeContext: map[string]any{
			"usage": map[string]any{
				"contextWindow": map[string]any{"usedTokens": 120, "totalTokens": 200_000},
				"lastTurn": map[string]any{
					"models": map[string]any{
						"default": map[string]any{
							"inputTokens":              100,
							"outputTokens":             20,
							"cacheReadInputTokens":     7,
							"cacheCreationInputTokens": 3,
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	session, ok, err := store.GetSession(ctx, "ws-usage", "session-usage")
	if err != nil || !ok {
		t.Fatalf("GetSession() ok=%v error=%v", ok, err)
	}
	if session.Metadata.Usage == nil || !session.Metadata.Usage.Tokens.HasReported() ||
		session.Metadata.Usage.Tokens.InputTokens == nil || *session.Metadata.Usage.Tokens.InputTokens != 100 ||
		session.Metadata.Usage.Tokens.OutputTokens == nil || *session.Metadata.Usage.Tokens.OutputTokens != 20 {
		t.Fatalf("session tokens=%#v, want persisted lastTurn counts", session.Metadata.Usage)
	}

	page, ok, err := store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-usage", AgentSessionID: "session-usage", Limit: 10,
	})
	if err != nil || !ok || len(page.Messages) != 1 {
		t.Fatalf("ListSessionMessages() page=%#v ok=%v error=%v", page, ok, err)
	}
	usage := ParseProviderTokenUsage(page.Messages[0].Payload["usage"])
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 100 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 20 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 7 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 3 {
		t.Fatalf("message payload=%#v, want non-zero usage on the completed turn", page.Messages[0].Payload)
	}
}

func TestReportActivityStatePersistsCodexLastTurnOnSettledAssistantMessage(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-codex", AgentSessionID: "session-codex", Origin: "runtime",
		Provider: "codex", Status: "running", OccurredAtUnixMS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-codex", AgentSessionID: "session-codex", TurnID: "turn-1",
		Phase: TurnPhaseRunning, OccurredAtUnixMS: 2,
	}); err != nil || !accepted {
		t.Fatalf("RecordTurnTransition() accepted=%v error=%v", accepted, err)
	}

	settled, err := store.ReportActivityState(ctx, ActivityStateReport{
		Session: SessionStateReport{
			WorkspaceID: "ws-codex", AgentSessionID: "session-codex", Kind: SessionKindRoot,
			Origin: "runtime", Provider: "codex", Status: "ready", CurrentPhase: "idle",
			OccurredAtUnixMS: 4,
			RuntimeContext: map[string]any{
				"usage": map[string]any{
					"contextWindow": map[string]any{"usedTokens": 1000, "totalTokens": 272_000},
					"lastTurn": map[string]any{
						"models": map[string]any{
							"default": map[string]any{
								"inputTokens":       1000,
								"cachedInputTokens": 30,
								"outputTokens":      200,
							},
						},
					},
				},
			},
		},
		Turn: &TurnTransition{
			WorkspaceID: "ws-codex", AgentSessionID: "session-codex", TurnID: "turn-1",
			Phase: TurnPhaseSettled, Outcome: TurnOutcomeCompleted, OccurredAtUnixMS: 4,
		},
		Messages: []MessageUpdate{{
			MessageID: "assistant-1", TurnID: "turn-1", Role: "assistant", Kind: "text",
			Status: "completed", Payload: map[string]any{"text": "ok"}, OccurredAtUnixMS: 3,
		}},
	})
	if err != nil || !settled.TurnAccepted || settled.Messages.AcceptedCount != 1 {
		t.Fatalf("ReportActivityState() result=%#v error=%v", settled, err)
	}

	page, ok, err := store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-codex", AgentSessionID: "session-codex", Limit: 10,
	})
	if err != nil || !ok || len(page.Messages) != 1 {
		t.Fatalf("ListSessionMessages() page=%#v ok=%v error=%v", page, ok, err)
	}
	usage := ParseProviderTokenUsage(page.Messages[0].Payload["usage"])
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 1000 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 200 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 30 {
		t.Fatalf("message payload=%#v, want Codex last-turn tokens", page.Messages[0].Payload)
	}
}

func TestReportSessionMessagesBackfillsUsageWhenTokensArrivedFirst(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", Origin: "runtime",
		Provider: "claude-code", Status: "running", OccurredAtUnixMS: 10,
		RuntimeContext: map[string]any{
			"usage": map[string]any{
				"contextWindow": map[string]any{"usedTokens": 120, "totalTokens": 200_000},
				"lastTurn": map[string]any{
					"models": map[string]any{
						"default": map[string]any{
							"inputTokens":              90,
							"outputTokens":             18,
							"cacheReadInputTokens":     2,
							"cacheCreationInputTokens": 1,
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", TurnID: "turn-1",
		Phase: TurnPhaseRunning, OccurredAtUnixMS: 11,
	}); err != nil || !accepted {
		t.Fatalf("RecordTurnTransition() accepted=%v error=%v", accepted, err)
	}
	if result, err := store.ReportSessionMessages(ctx, SessionMessageReport{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", Origin: "runtime", Provider: "claude-code",
		Messages: []MessageUpdate{{
			MessageID: "assistant-late", TurnID: "turn-1", Role: "assistant", Kind: "text",
			Status: "completed", Payload: map[string]any{"text": "later"}, OccurredAtUnixMS: 12,
		}},
	}); err != nil || result.AcceptedCount != 1 {
		t.Fatalf("ReportSessionMessages() result=%#v error=%v", result, err)
	}

	page, ok, err := store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", Limit: 10,
	})
	if err != nil || !ok || len(page.Messages) != 1 {
		t.Fatalf("ListSessionMessages() page=%#v ok=%v error=%v", page, ok, err)
	}
	usage := ParseProviderTokenUsage(page.Messages[0].Payload["usage"])
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 90 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 18 {
		t.Fatalf("payload=%#v, want backfilled usage after tokens arrived first", page.Messages[0].Payload)
	}

	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", TurnID: "turn-1",
		Phase: TurnPhaseSettled, Outcome: TurnOutcomeCompleted, OccurredAtUnixMS: 19,
	}); err != nil || !accepted {
		t.Fatalf("settle turn-1 accepted=%v error=%v", accepted, err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", TurnID: "turn-2",
		Phase: TurnPhaseRunning, OccurredAtUnixMS: 20,
	}); err != nil || !accepted {
		t.Fatalf("second RecordTurnTransition() accepted=%v error=%v", accepted, err)
	}
	if result, err := store.ReportSessionMessages(ctx, SessionMessageReport{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", Origin: "runtime", Provider: "claude-code",
		Messages: []MessageUpdate{{
			MessageID: "assistant-next", TurnID: "turn-2", Role: "assistant", Kind: "text",
			Status: "completed", Payload: map[string]any{"text": "next"}, OccurredAtUnixMS: 21,
		}},
	}); err != nil || result.AcceptedCount != 1 {
		t.Fatalf("second ReportSessionMessages() result=%#v error=%v", result, err)
	}
	page, ok, err = store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-order", AgentSessionID: "session-order", Limit: 10, Order: MessageOrderAsc,
	})
	if err != nil || !ok || len(page.Messages) != 2 {
		t.Fatalf("ListSessionMessages() later page=%#v ok=%v error=%v", page, ok, err)
	}
	if ParseProviderTokenUsage(page.Messages[1].Payload["usage"]).HasReported() {
		t.Fatalf("next-turn payload=%#v, want no restamp of the earlier snapshot", page.Messages[1].Payload)
	}
}

func TestReportSessionMessagesPersistsAssistantPayloadUsage(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID: "ws-payload", AgentSessionID: "session-payload", Origin: "runtime",
		Provider: "claude-code", Status: "ready", OccurredAtUnixMS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-payload", AgentSessionID: "session-payload", TurnID: "turn-1",
		Phase: TurnPhaseRunning, OccurredAtUnixMS: 2,
	}); err != nil || !accepted {
		t.Fatalf("RecordTurnTransition() accepted=%v error=%v", accepted, err)
	}
	if result, err := store.ReportSessionMessages(ctx, SessionMessageReport{
		WorkspaceID: "ws-payload", AgentSessionID: "session-payload", Origin: "runtime", Provider: "claude-code",
		Messages: []MessageUpdate{{
			MessageID: "assistant-1", TurnID: "turn-1", Role: "assistant", Kind: "text",
			Status: "completed", OccurredAtUnixMS: 3,
			Payload: map[string]any{
				"text": "done",
				"usage": map[string]any{
					"input_tokens":                int64(64),
					"output_tokens":               int64(8),
					"cache_read_input_tokens":     int64(1),
					"cache_creation_input_tokens": int64(2),
				},
			},
		}},
	}); err != nil || result.AcceptedCount != 1 {
		t.Fatalf("ReportSessionMessages() result=%#v error=%v", result, err)
	}

	page, ok, err := store.ListSessionMessages(ctx, ListSessionMessagesInput{
		WorkspaceID: "ws-payload", AgentSessionID: "session-payload", Limit: 10,
	})
	if err != nil || !ok || len(page.Messages) != 1 {
		t.Fatalf("ListSessionMessages() page=%#v ok=%v error=%v", page, ok, err)
	}
	usage := ParseProviderTokenUsage(page.Messages[0].Payload["usage"])
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 64 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 8 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 1 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 2 {
		t.Fatalf("payload=%#v, want persisted assistant payload.usage", page.Messages[0].Payload)
	}
}
