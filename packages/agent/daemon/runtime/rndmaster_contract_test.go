package agentruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRnDMasterContractFlowsIntoCodexParams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.json")
	raw, _ := json.Marshal(map[string]any{
		"version": 1, "provider": "codex", "systemPrompt": "audit every action",
		"mcpConfig": map[string]any{"mcpServers": map[string]any{"report": map[string]any{"command": "reporter"}}},
	})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	session := Session{RuntimeContext: map[string]any{"rndmaster": map[string]any{"contractFile": path}}}
	session.Settings = &SessionSettings{Model: "gpt-test"}
	thread := appServerThreadStartParams(session, t.TempDir())
	config := payloadObject(thread["config"])
	if len(payloadObject(config["mcp_servers"])) != 1 {
		t.Fatalf("thread config = %#v, want managed MCP server", config)
	}
	turn := appServerTurnStartParams(
		session,
		"thread",
		[]PromptContentBlock{{Type: "text", Text: "go"}},
		nil,
		map[string]any{"mode": "default"},
		"gpt-test",
		"",
		false,
	)
	mode := payloadObject(turn["collaborationMode"])
	settings := payloadObject(mode["settings"])
	if got := asString(settings["developer_instructions"]); got != "audit every action" {
		t.Fatalf("developer instructions = %q", got)
	}
}

func TestCodexUsagePublishesExactLastTurn(t *testing.T) {
	state, ok := appServerTokenUsageState(map[string]any{"tokenUsage": map[string]any{
		"modelContextWindow": 200000,
		"last":               map[string]any{"inputTokens": 100, "cachedInputTokens": 30, "outputTokens": 10, "reasoningOutputTokens": 2},
	}})
	if !ok {
		t.Fatal("usage was not parsed")
	}
	usage := acpUsageRuntimeContext(state)
	models := payloadObject(payloadObject(usage["lastTurn"])["models"])
	last := payloadObject(models["default"])
	if got, _ := firstInt64Value(last, "cachedInputTokens"); got != 30 {
		t.Fatalf("last turn = %#v", last)
	}
}

func TestClaudeUsagePublishesExactLastTurn(t *testing.T) {
	update := claudeSDKUsageUpdate(map[string]any{
		"usage": map[string]any{
			"input_tokens":                100,
			"output_tokens":               20,
			"cache_read_input_tokens":     7,
			"cache_creation_input_tokens": 3,
		},
		"contextWindow": map[string]any{
			"usedTokens":  130,
			"totalTokens": 200_000,
		},
	}, claudeSDKUsageState{}, "")
	state, ok := claudeSDKUsageStateFromPayload(update)
	if !ok {
		t.Fatal("usage was not parsed")
	}
	models := payloadObject(payloadObject(claudeSDKUsageRuntimeContext(state)["lastTurn"])["models"])
	last := payloadObject(models["default"])
	if got, _ := firstInt64Value(last, "inputTokens"); got != 100 {
		t.Fatalf("last turn = %#v", last)
	}
}

func TestRnDMasterEnvListDeduplicatesWindowsNamesCaseInsensitively(t *testing.T) {
	got := rndmasterEnvListForOS(
		[]string{"Path=C:\\Windows", "PATH=C:\\duplicate", "HOME=C:\\Users\\test"},
		map[string]string{"PATH": "C:\\managed", "TEMP": "C:\\Temp"},
		"windows",
	)
	want := []string{"Path=C:\\managed", "HOME=C:\\Users\\test", "TEMP=C:\\Temp"}
	if len(got) != len(want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("env = %#v, want %#v", got, want)
		}
	}
}

func TestRnDMasterContractPermissionFieldIsOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"provider":"deepseek-harness"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	session := Session{RuntimeContext: map[string]any{"rndmaster": map[string]any{"contractFile": path}}}
	contract, err := rndmasterContractFromSession(session)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Permission != nil {
		t.Fatalf("old contract permission = %#v, want omitted", contract.Permission)
	}
	if got := rndmasterContractAutomaticDecision(session); got != "" {
		t.Fatalf("old contract decision = %q, want empty", got)
	}

	if err := os.WriteFile(path, []byte(`{"version":1,"permission":{"automaticDecision":"approved"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := rndmasterContractAutomaticDecision(session); got != "approved" {
		t.Fatalf("decision = %q, want approved", got)
	}
}

func TestRnDMasterEnvListExceptKeepsReservedHomeAndAPIKey(t *testing.T) {
	got := rndmasterEnvListExceptForOS(
		[]string{"TEST_AGENT_HOME=/runtime/home", "OPENAI_API_KEY=runtime-key", "KEEP=session"},
		map[string]string{
			"TEST_AGENT_HOME": "contract-must-not-win",
			"OPENAI_API_KEY":  "contract-key-must-not-win",
			"TASK_ISSUE_ID":   "issue-42",
		},
		[]string{"TEST_AGENT_HOME", "OPENAI_API_KEY"},
		"linux",
	)
	want := []string{
		"TEST_AGENT_HOME=/runtime/home",
		"OPENAI_API_KEY=runtime-key",
		"KEEP=session",
		"TASK_ISSUE_ID=issue-42",
	}
	if len(got) != len(want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("env = %#v, want %#v", got, want)
		}
	}
}
