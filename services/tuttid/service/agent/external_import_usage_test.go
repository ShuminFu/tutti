package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

func TestImportExternalSessionsPersistsClaudeMessageUsage(t *testing.T) {
	ctx := context.Background()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-1", Name: "Workspace One"}); err != nil {
		t.Fatalf("Create workspace error = %v", err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "project-a")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("create project error = %v", err)
	}
	if canonical, ok := canonicalExistingDir(project); ok {
		project = canonical
	}
	claudeHome := filepath.Join(root, "claude-home")
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("GROK_HOME", filepath.Join(root, "grok-home"))
	timestamp := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	writeAgentServiceJSONL(t, filepath.Join(claudeHome, "projects", "project-a", "claude-usage.jsonl"),
		map[string]any{
			"timestamp": timestamp,
			"sessionId": "claude-usage",
			"cwd":       project,
			"uuid":      "claude-user",
			"message":   map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Count tokens"}}},
		},
		map[string]any{
			"timestamp": timestamp,
			"sessionId": "claude-usage",
			"cwd":       project,
			"uuid":      "claude-assistant",
			"message": map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "text", "text": "Done"}},
				"usage": map[string]any{
					"input_tokens":                111,
					"output_tokens":               22,
					"cache_read_input_tokens":     8,
					"cache_creation_input_tokens": 4,
				},
			},
		},
	)

	service := newIsolatedAgentService(newFakeRuntime())
	service.AgentTargetStore = fakeAgentTargetStore{targets: defaultTestAgentTargets()}
	projection := NewActivityProjection(store)
	service.SessionReader = projection
	service.MessageReader = projection
	service.ExternalImportStore = store

	sessionID := externalImportedSessionID("claude-code", "claude-usage")
	result, err := service.ImportExternalSessions(ctx, "ws-1", ExternalImportInput{
		Projects: []ExternalImportProjectSelection{{Path: project, SessionIDs: []string{sessionID}}},
	})
	if err != nil {
		t.Fatalf("ImportExternalSessions error = %v", err)
	}
	if result.ImportedSessions != 1 || result.ImportedMessages < 2 {
		t.Fatalf("import result = %#v, want one session and both messages", result)
	}

	imported, err := service.Get(ctx, "ws-1", sessionID)
	if err != nil {
		t.Fatalf("Get imported session error = %v", err)
	}
	if imported.Metadata.Usage == nil || !imported.Metadata.Usage.Tokens.HasReported() ||
		imported.Metadata.Usage.Tokens.InputTokens == nil || *imported.Metadata.Usage.Tokens.InputTokens != 111 ||
		imported.Metadata.Usage.Tokens.OutputTokens == nil || *imported.Metadata.Usage.Tokens.OutputTokens != 22 {
		t.Fatalf("session tokens = %#v, want latest imported Claude usage", imported.Metadata.Usage)
	}

	page, err := service.ListMessages(ctx, "ws-1", sessionID, ListMessagesInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages error = %v", err)
	}
	usage := agentactivitybiz.ParseProviderTokenUsage(assistantImportUsage(page.Messages))
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 111 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 22 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 8 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 4 {
		t.Fatalf("imported messages = %#v, want non-zero payload.usage", page.Messages)
	}
}

func assistantImportUsage(messages []SessionMessage) map[string]any {
	for _, message := range messages {
		if message.Role == "assistant" && message.Kind == "text" {
			usage, _ := message.Payload["usage"].(map[string]any)
			return usage
		}
	}
	return nil
}
