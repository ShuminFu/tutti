package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

func TestParseGrokSessionDirUsesUpdatesAndSummary(t *testing.T) {
	cwd := t.TempDir()
	if canonical, ok := canonicalExistingDir(cwd); ok {
		cwd = canonical
	}
	sessionID := "0193f0a8-2c1e-7b6a-9d44-3a1c8e5f0011"
	dir := writeGrokLocalSession(t, t.TempDir(), cwd, sessionID, grokLocalSessionFixture{
		title:     "Investigate import layout",
		model:     "grok-4.6",
		createdAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		updatedAt: time.Date(2026, 9, 1, 10, 2, 0, 0, time.UTC),
		updates: []map[string]any{
			grokUpdateLine(sessionID, 1_725_188_400_000, 0, "user_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "How does Grok store sessions?"},
			}),
			grokUpdateLine(sessionID, 1_725_188_401_000, 0, "agent_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Under ~/.grok/sessions."},
			}),
			grokUpdateLine(sessionID, 1_725_188_402_000, 0, "tool_call", map[string]any{
				"toolCallId": "call-read-1",
				"title":      "Read `summary.json`",
				"kind":       "read",
				"rawInput":   map[string]any{"path": "summary.json"},
			}),
			grokUpdateLine(sessionID, 1_725_188_403_000, 0, "tool_call_update", map[string]any{
				"toolCallId": "call-read-1",
				"status":     "completed",
				"content": []any{
					map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "ok"}},
				},
			}),
		},
	})

	session, ok, err := parseGrokSessionDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("parseGrokSessionDir error = %v", err)
	}
	if !ok {
		t.Fatal("parseGrokSessionDir ok = false")
	}
	if session.Provider != grokImportProvider {
		t.Fatalf("provider = %q, want %q", session.Provider, grokImportProvider)
	}
	if session.ProviderSessionID != sessionID {
		t.Fatalf("providerSessionID = %q, want the on-disk session id", session.ProviderSessionID)
	}
	if session.Cwd != cwd {
		t.Fatalf("cwd = %q, want %q", session.Cwd, cwd)
	}
	if session.Title != "Investigate import layout" {
		t.Fatalf("title = %q, want summary generated_title", session.Title)
	}
	if session.Model != "grok-4.6" {
		t.Fatalf("model = %q, want summary current_model_id", session.Model)
	}
	if len(session.Messages) != 4 {
		t.Fatalf("messages = %#v, want user, assistant, tool call, tool result", session.Messages)
	}
	if session.Messages[0].Role != "user" || session.Messages[0].Text != "How does Grok store sessions?" {
		t.Fatalf("user message = %#v", session.Messages[0])
	}
	if session.Messages[0].OccurredAtUnixMS != 1_725_188_400_000 {
		t.Fatalf("user timestamp = %d, want ACP agentTimestampMs", session.Messages[0].OccurredAtUnixMS)
	}
	if session.Messages[1].Role != "assistant" || session.Messages[1].Text != "Under ~/.grok/sessions." {
		t.Fatalf("assistant message = %#v", session.Messages[1])
	}
	if session.Messages[1].Usage != nil {
		t.Fatalf("assistant usage = %#v, want empty; this fixture has no usage.json", session.Messages[1].Usage)
	}
	if session.Messages[2].Kind != "tool_call" || session.Messages[2].Payload["callId"] != "call-read-1" {
		t.Fatalf("tool call = %#v", session.Messages[2])
	}
}

func TestParseGrokSessionDirSkipsUserInfoAndFallsBackToChatHistory(t *testing.T) {
	cwd := t.TempDir()
	sessionID := "chat-history-only"
	dir := writeGrokLocalSession(t, t.TempDir(), cwd, sessionID, grokLocalSessionFixture{
		title: "Fallback title",
		chat: []map[string]any{
			{"type": "user", "content": "<user_info>\nWorkspace Path: " + cwd + "\n</user_info>"},
			{"type": "user", "content": "Resume the last turn"},
			{"type": "assistant", "content": "Resumed.", "model_id": "grok-4.5"},
		},
	})

	session, ok, err := parseGrokSessionDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("parseGrokSessionDir error = %v", err)
	}
	if !ok {
		t.Fatal("parseGrokSessionDir ok = false")
	}
	if session.ProviderSessionID != sessionID {
		t.Fatalf("providerSessionID = %q, want directory id", session.ProviderSessionID)
	}
	if session.Model != "grok-4.5" {
		t.Fatalf("model = %q, want chat_history model_id", session.Model)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("messages = %#v, want user_info skipped", session.Messages)
	}
	if session.Messages[0].Text != "Resume the last turn" {
		t.Fatalf("first visible message = %#v", session.Messages[0])
	}
}

func TestScanAndImportGrokSessionsPersistsStableProviderSessionID(t *testing.T) {
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
	grokHome := filepath.Join(root, "grok-home")
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("TUTTI_GROK_EXTRA_IMPORT_ROOTS", "")
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	sessionID := "0193f0a8-2c1e-7b6a-9d44-3a1c8e5f00aa"
	now := time.Now().UTC()
	writeGrokLocalSession(t, grokHome, project, sessionID, grokLocalSessionFixture{
		title:     "Grok local import",
		model:     "grok-4.6",
		createdAt: now.Add(-time.Minute),
		updatedAt: now,
		updates: []map[string]any{
			grokUpdateLine(sessionID, now.Add(-30*time.Second).UnixMilli(), 0, "user_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Import this Grok thread"},
			}),
			grokUpdateLine(sessionID, now.UnixMilli(), 0, "agent_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Imported with token counts."},
			}),
		},
		usage: grokUsageSnapshot(sessionID),
	})

	service := newIsolatedAgentService(newFakeRuntime())
	scan, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Providers: []string{"grok"}, Days: -1})
	if err != nil {
		t.Fatalf("ScanExternalImports error = %v", err)
	}
	if len(scan.Errors) != 0 {
		t.Fatalf("scan errors = %#v, want none", scan.Errors)
	}
	if scan.ScannedSessions != 1 || scan.ScannedMessages != 2 {
		t.Fatalf("scan = %#v, want one Grok session and two messages", scan)
	}
	if len(scan.Providers) != 1 || scan.Providers[0].Provider != grokImportProvider || !scan.Providers[0].Available {
		t.Fatalf("scan providers = %#v, want available %s", scan.Providers, grokImportProvider)
	}
	if len(scan.Sessions) != 1 || scan.Sessions[0].Provider != grokImportProvider || scan.Sessions[0].ProjectPath != project {
		t.Fatalf("scan sessions = %#v, want Grok session under %q", scan.Sessions, project)
	}

	projection := NewActivityProjection(store)
	service.SessionReader = projection
	service.MessageReader = projection
	service.ExternalImportStore = store
	result, err := service.ImportExternalSessions(ctx, "ws-1", ExternalImportInput{
		Projects: []ExternalImportProjectSelection{{
			Path:      project,
			Providers: []string{"acp:grok"},
		}},
	})
	if err != nil {
		t.Fatalf("ImportExternalSessions error = %v", err)
	}
	if result.ImportedSessions != 1 || result.ImportedMessages != 2 {
		t.Fatalf("import result = %#v, want one session and two messages", result)
	}

	importedID := externalImportedSessionID(grokImportProvider, sessionID)
	imported, err := service.Get(ctx, "ws-1", importedID)
	if err != nil {
		t.Fatalf("Get imported Grok session error = %v", err)
	}
	if imported.Provider != grokImportProvider || imported.ProviderSessionID != sessionID {
		t.Fatalf("imported session = %#v, want stable acp:grok provider_session_id", imported)
	}
	if imported.AgentTargetID != grokImportTargetID {
		t.Fatalf("agentTargetID = %q, want %q", imported.AgentTargetID, grokImportTargetID)
	}
	if value(imported.Title) != "Grok local import" {
		t.Fatalf("title = %q, want summary title", value(imported.Title))
	}
	if imported.Settings == nil || imported.Settings.Model != "grok-4.6" {
		t.Fatalf("settings = %#v, want imported model grok-4.6", imported.Settings)
	}
	if imported.Metadata.Usage == nil || !imported.Metadata.Usage.Tokens.HasReported() ||
		imported.Metadata.Usage.Tokens.InputTokens == nil || *imported.Metadata.Usage.Tokens.InputTokens != 421051 ||
		imported.Metadata.Usage.Tokens.OutputTokens == nil || *imported.Metadata.Usage.Tokens.OutputTokens != 10894 {
		t.Fatalf("session usage = %#v, want the usage.json session totals", imported.Metadata.Usage)
	}

	page, err := service.ListMessages(ctx, "ws-1", importedID, ListMessagesInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages error = %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("imported messages = %#v, want user and assistant", page.Messages)
	}
	usage := agentactivitybiz.ParseProviderTokenUsage(assistantImportUsage(page.Messages))
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 421051 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 10894 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 306048 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 512 {
		t.Fatalf("assistant payload.usage = %#v, want the mapped usage.json counts", page.Messages[1].Payload["usage"])
	}
}

// grokUsageSnapshot mirrors the on-disk `~/.grok/.../usage.json` shape:
// camelCase session totals plus a per-turn breakdown.
func grokUsageSnapshot(sessionID string) map[string]any {
	totals := map[string]any{
		"inputTokens":         421051,
		"outputTokens":        10894,
		"cachedReadTokens":    306048,
		"cacheCreationTokens": 512,
		"reasoningTokens":     7510,
		"totalTokens":         431945,
		"modelCalls":          10,
		"costUsdTicks":        1524539600,
		"turnCount":           1,
		"primaryModelId":      "grok-4.6-build",
		"modelUsage": map[string]any{
			"grok-4.6-build": map[string]any{
				"inputTokens":         421051,
				"outputTokens":        10894,
				"cachedReadTokens":    306048,
				"cacheCreationTokens": 512,
			},
		},
	}
	turn := map[string]any{"turnNumber": 1}
	for key, value := range totals {
		turn[key] = value
	}
	return map[string]any{
		"sessionId": sessionID,
		"updatedAt": "2026-09-15T02:20:58.582820+00:00",
		"session":   totals,
		"turns":     []any{turn},
	}
}

func TestParseGrokSessionDirMapsUsageJSONOntoLastAssistantMessage(t *testing.T) {
	cwd := t.TempDir()
	if canonical, ok := canonicalExistingDir(cwd); ok {
		cwd = canonical
	}
	sessionID := "0193f0a8-2c1e-7b6a-9d44-3a1c8e5f0022"
	dir := writeGrokLocalSession(t, t.TempDir(), cwd, sessionID, grokLocalSessionFixture{
		title: "Token snapshot",
		model: "grok-4.6-build",
		updates: []map[string]any{
			grokUpdateLine(sessionID, 1_725_188_400_000, 0, "user_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Count my tokens"},
			}),
			grokUpdateLine(sessionID, 1_725_188_401_000, 0, "agent_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Counted."},
			}),
		},
		usage: grokUsageSnapshot(sessionID),
	})

	session, ok, err := parseGrokSessionDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("parseGrokSessionDir error = %v", err)
	}
	if !ok {
		t.Fatal("parseGrokSessionDir ok = false")
	}
	if len(session.Messages) != 2 {
		t.Fatalf("messages = %#v, want user and assistant", session.Messages)
	}
	if session.Messages[0].Usage != nil {
		t.Fatalf("user usage = %#v, want the snapshot only on the assistant message", session.Messages[0].Usage)
	}
	usage := agentactivitybiz.ParseProviderTokenUsage(session.Messages[1].Usage)
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 421051 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 10894 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 306048 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 512 {
		t.Fatalf("assistant usage = %#v, want the usage.json session totals", session.Messages[1].Usage)
	}
	// Grok reports reasoning/total/cost alongside the token counts; they have no
	// Claude usage counterpart and must not leak into the mapped object.
	for _, key := range []string{"reasoningTokens", "totalTokens", "costUsdTicks", "modelCalls"} {
		if _, exists := session.Messages[1].Usage[key]; exists {
			t.Fatalf("assistant usage = %#v, want %q dropped", session.Messages[1].Usage, key)
		}
	}
}

func TestParseGrokSessionDirLeavesUsageEmptyWithoutUsageJSON(t *testing.T) {
	cwd := t.TempDir()
	if canonical, ok := canonicalExistingDir(cwd); ok {
		cwd = canonical
	}
	sessionID := "0193f0a8-2c1e-7b6a-9d44-3a1c8e5f0033"
	dir := writeGrokLocalSession(t, t.TempDir(), cwd, sessionID, grokLocalSessionFixture{
		title: "No token snapshot",
		updates: []map[string]any{
			grokUpdateLine(sessionID, 1_725_188_400_000, 0, "user_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Count my tokens"},
			}),
			grokUpdateLine(sessionID, 1_725_188_401_000, 0, "agent_message_chunk", map[string]any{
				"content": map[string]any{"type": "text", "text": "Counted."},
			}),
		},
	})
	// signals.json is deliberately not a token source: a session dir carrying one
	// but no usage.json still reports nothing.
	if err := os.WriteFile(filepath.Join(dir, "signals.json"), []byte(`{"inputTokens":999}`), 0o644); err != nil {
		t.Fatalf("write grok signals error = %v", err)
	}

	session, ok, err := parseGrokSessionDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("parseGrokSessionDir error = %v", err)
	}
	if !ok {
		t.Fatal("parseGrokSessionDir ok = false")
	}
	for _, message := range session.Messages {
		if len(message.Usage) != 0 {
			t.Fatalf("message usage = %#v, want empty without usage.json", message.Usage)
		}
		if usage, _ := message.Payload["usage"].(map[string]any); len(usage) != 0 {
			t.Fatalf("payload.usage = %#v, want empty without usage.json", usage)
		}
	}
	if usage := lastExternalImportedUsage(session.Messages); len(usage) != 0 {
		t.Fatalf("session usage = %#v, want empty without usage.json", usage)
	}
}

func TestNormalizeExternalImportProvidersAcceptsGrokAliases(t *testing.T) {
	got := normalizeExternalImportProviders([]string{" GROK ", "acp:grok", "chatgpt"})
	if len(got) != 2 || got[0] != grokImportProvider || got[1] != chatgptExportProvider {
		t.Fatalf("providers = %#v, want [acp:grok chatgpt]", got)
	}
	if externalImportAgentTargetID("grok") != grokImportTargetID {
		t.Fatalf("target = %q, want %q", externalImportAgentTargetID("grok"), grokImportTargetID)
	}
}

type grokLocalSessionFixture struct {
	title     string
	model     string
	createdAt time.Time
	updatedAt time.Time
	updates   []map[string]any
	chat      []map[string]any
	usage     map[string]any
}

func writeGrokLocalSession(t *testing.T, grokHome string, cwd string, sessionID string, fixture grokLocalSessionFixture) string {
	t.Helper()
	dir := filepath.Join(grokHome, grokSessionsDirName, grokTestEncodeCwd(cwd), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create grok session dir error = %v", err)
	}
	createdAt := fixture.createdAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := fixture.updatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	summary := map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"generated_title":  fixture.title,
		"session_summary":  fixture.title,
		"current_model_id": fixture.model,
		"created_at":       createdAt.Format(time.RFC3339Nano),
		"updated_at":       updatedAt.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal grok summary error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, grokSummaryFileName), encoded, 0o644); err != nil {
		t.Fatalf("write grok summary error = %v", err)
	}
	if len(fixture.updates) > 0 {
		writeAgentServiceJSONL(t, filepath.Join(dir, grokUpdatesFileName), fixture.updates...)
	}
	if len(fixture.chat) > 0 {
		writeAgentServiceJSONL(t, filepath.Join(dir, grokChatHistoryFileName), fixture.chat...)
	}
	if len(fixture.usage) > 0 {
		encodedUsage, err := json.Marshal(fixture.usage)
		if err != nil {
			t.Fatalf("marshal grok usage error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, grokUsageFileName), encodedUsage, 0o644); err != nil {
			t.Fatalf("write grok usage error = %v", err)
		}
	}
	return dir
}

func grokUpdateLine(sessionID string, timestampMS int64, promptIndex int, sessionUpdate string, update map[string]any) map[string]any {
	if update == nil {
		update = map[string]any{}
	}
	update["sessionUpdate"] = sessionUpdate
	meta, _ := update["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["promptIndex"] = promptIndex
	update["_meta"] = meta
	return map[string]any{
		"timestamp": timestampMS / 1000,
		"method":    "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update":    update,
			"_meta": map[string]any{
				"agentTimestampMs": timestampMS,
			},
		},
	}
}

func grokTestEncodeCwd(cwd string) string {
	var builder strings.Builder
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			builder.WriteByte(c)
			continue
		}
		builder.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return builder.String()
}
