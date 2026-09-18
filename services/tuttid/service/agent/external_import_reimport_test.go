package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

func TestServiceReimportRepairsLegacyTurnlessExternalMessages(t *testing.T) {
	ctx := context.Background()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-1", Name: "Workspace One"}); err != nil {
		t.Fatalf("Create workspace error = %v", err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("create project error = %v", err)
	}
	if canonical, ok := canonicalExistingDir(project); ok {
		project = canonical
	}
	codexHome := filepath.Join(root, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	sourcePath := filepath.Join(codexHome, "sessions", "legacy-turnless.jsonl")
	writeAgentServiceJSONL(t, sourcePath,
		map[string]any{
			"timestamp": "2026-07-24T00:00:00Z",
			"type":      "session_meta",
			"payload":   map[string]any{"id": "legacy-turnless", "cwd": project},
		},
		map[string]any{"timestamp": "2026-07-24T00:00:01Z", "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "user-1", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "Inspect the project"}},
		}},
		map[string]any{"timestamp": "2026-07-24T00:00:02Z", "type": "response_item", "payload": map[string]any{
			"type": "function_call", "id": "tool-1", "name": "exec_command",
			"call_id": "call-status", "arguments": `{"cmd":"git status --short"}`,
		}},
		map[string]any{"timestamp": "2026-07-24T00:00:03Z", "type": "response_item", "payload": map[string]any{
			"type": "function_call_output", "call_id": "call-status", "output": "clean",
		}},
		map[string]any{"timestamp": "2026-07-24T00:00:04Z", "type": "response_item", "payload": map[string]any{
			"type": "message", "id": "assistant-1", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": "The project is clean"}},
		}},
	)
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open source transcript error = %v", err)
	}
	defer source.Close()
	parsed, ok, err := parseCodexJSONL(context.Background(), sourcePath, source)
	if err != nil || !ok {
		t.Fatalf("parseCodexJSONL ok=%v error=%v", ok, err)
	}
	agentSessionID := externalImportedSessionID(parsed.Provider, parsed.ProviderSessionID)
	legacyUpdates := make([]agentactivitybiz.MessageUpdate, 0, len(parsed.Messages))
	for index, message := range parsed.Messages {
		legacyUpdates = append(legacyUpdates, agentactivitybiz.MessageUpdate{
			MessageID:         externalImportedMessageIDForMessage(parsed.Provider, parsed.ProviderSessionID, message, index),
			Role:              message.Role,
			Kind:              message.Kind,
			Status:            message.Status,
			Payload:           externalImportedMessagePayload(message),
			OccurredAtUnixMS:  message.OccurredAtUnixMS,
			StartedAtUnixMS:   message.StartedAtUnixMS,
			CompletedAtUnixMS: message.CompletedAtUnixMS,
		})
	}
	if _, err := store.ReportSessionMessages(ctx, agentactivitybiz.SessionMessageReport{
		WorkspaceID: "ws-1", AgentSessionID: agentSessionID,
		Origin: WorkspaceAgentSessionOriginImported, Provider: parsed.Provider,
		HistoricalImport: true, Messages: legacyUpdates,
	}); err != nil {
		t.Fatalf("seed legacy turnless messages error = %v", err)
	}

	service := newIsolatedAgentService(newFakeRuntime())
	service.ExternalImportStore = store
	result, err := service.ImportExternalSessions(ctx, "ws-1", ExternalImportInput{
		Projects: []ExternalImportProjectSelection{{Path: project}},
	})
	if err != nil {
		t.Fatalf("ImportExternalSessions repair error = %v", err)
	}
	if result.ImportedMessages != len(parsed.Messages) {
		t.Fatalf("repair result = %#v, want %d repaired updates", result, len(parsed.Messages))
	}
	page, found, err := store.ListSessionMessages(ctx, agentactivitybiz.ListSessionMessagesInput{
		WorkspaceID: "ws-1", AgentSessionID: agentSessionID, Limit: 10,
	})
	if err != nil || !found {
		t.Fatalf("ListSessionMessages repaired found=%v error=%v", found, err)
	}
	repairedTurnID := ""
	for _, message := range page.Messages {
		if message.TurnID == "" {
			t.Fatalf("repaired message %q remains turnless", message.MessageID)
		}
		if repairedTurnID == "" {
			repairedTurnID = message.TurnID
		} else if message.TurnID != repairedTurnID {
			t.Fatalf("repaired message %q turn = %q, want %q", message.MessageID, message.TurnID, repairedTurnID)
		}
	}
}
