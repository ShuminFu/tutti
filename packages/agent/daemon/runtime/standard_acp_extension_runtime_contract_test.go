package agentruntime

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	extensionRuntimeContractTestTarget   = "extension:deepseek-harness"
	extensionRuntimeContractTestProvider = "acp:deepseek-harness"
	extensionRuntimeHomeEnv              = "TEST_AGENT_HOME"
	extensionRuntimeAPIKeyEnv            = "OPENAI_API_KEY"
	extensionRuntimeContractSystemPrompt = "prepared extension system prompt"
	extensionRuntimeContractCWD          = "/issue/workspace"
	extensionRuntimeContractModel        = "deepseek-chat"
	extensionRuntimeContractIssueEnv     = "TASK_ISSUE_ID"
	extensionRuntimePrepHomeValue        = "/runtime/home"
	extensionRuntimePrepAPIKeyValue      = "runtime-key"
)

func newExtensionRuntimeContractTestAdapter(transport *standardACPTransport) *standardACPAdapter {
	transport.conn.configOptions = []map[string]any{{
		"id":           "model",
		"name":         "Model",
		"category":     "model",
		"type":         "select",
		"currentValue": "default",
		"options": []any{
			map[string]any{"value": "default", "name": "Auto"},
			map[string]any{"value": extensionRuntimeContractModel, "name": "DeepSeek Chat"},
		},
	}}
	adapter, err := NewStandardACPAdapter(StandardACPAdapterConfig{
		Provider:                extensionRuntimeContractTestProvider,
		Name:                    "deepseek-harness-acp",
		DisplayName:             "DeepSeek Harness",
		Command:                 []string{"dsh", "acp"},
		ModelConfigOptionID:     "model",
		AgentTargetID:           extensionRuntimeContractTestTarget,
		IsolatedRuntimeEnvNames: []string{extensionRuntimeHomeEnv, extensionRuntimeAPIKeyEnv},
	}, transport, LegacyHostMetadata())
	if err != nil {
		panic(err)
	}
	return adapter.(*standardACPAdapter)
}

func extensionRuntimeContractTestSession() Session {
	session := standardTestSession(extensionRuntimeContractTestProvider)
	session.ProviderSessionID = ""
	session.AgentTargetID = extensionRuntimeContractTestTarget
	session.Env = []string{
		extensionRuntimeHomeEnv + "=" + extensionRuntimePrepHomeValue,
		extensionRuntimeAPIKeyEnv + "=" + extensionRuntimePrepAPIKeyValue,
	}
	return session
}

func writeExtensionRuntimeContractFile(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"version":      1,
		"systemPrompt": extensionRuntimeContractSystemPrompt,
		"cwd":          extensionRuntimeContractCWD,
		"model":        extensionRuntimeContractModel,
		"env": map[string]string{
			extensionRuntimeHomeEnv:          "contract-must-not-win",
			extensionRuntimeAPIKeyEnv:        "contract-key-must-not-win",
			extensionRuntimeContractIssueEnv: "issue-42",
		},
		"mcpConfig": map[string]any{
			"mcpServers": map[string]any{
				"audit":           map[string]any{"type": "http", "url": "http://127.0.0.1:9/mcp"},
				"workflow_report": map[string]any{"command": "workflow-report", "args": []any{"--stdio"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runtime-contract.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func bindExtensionRuntimeContract(session *Session, path string) {
	session.RuntimeContext = map[string]any{
		"rndmaster": map[string]any{"contractFile": path},
	}
}

func cloneACPParams(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	return maps.Clone(source)
}

func TestExtensionAdapterMergesRuntimeContractBeforeSessionNew(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-contract")
	adapter := newExtensionRuntimeContractTestAdapter(transport)
	session := extensionRuntimeContractTestSession()
	bindExtensionRuntimeContract(&session, writeExtensionRuntimeContractFile(t))

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "dsh-session-contract"
	if _, err := adapter.Exec(context.Background(), session, textPrompt("first user prompt"), "", "turn-1", nil, nil); err != nil {
		t.Fatalf("first Exec: %v", err)
	}
	if _, err := adapter.Exec(context.Background(), session, textPrompt("second user prompt"), "", "turn-2", nil, nil); err != nil {
		t.Fatalf("second Exec: %v", err)
	}

	if len(transport.specs) != 1 {
		t.Fatalf("process starts = %d, want 1", len(transport.specs))
	}
	spec := transport.specs[0]
	if spec.CWD != extensionRuntimeContractCWD {
		t.Fatalf("process cwd = %q, want contract cwd", spec.CWD)
	}
	if got := sessionEnvValue(spec.Env, extensionRuntimeHomeEnv); got != extensionRuntimePrepHomeValue {
		t.Fatalf("home env = %q, want runtimePrep value %q", got, extensionRuntimePrepHomeValue)
	}
	if got := sessionEnvValue(spec.Env, extensionRuntimeAPIKeyEnv); got != extensionRuntimePrepAPIKeyValue {
		t.Fatalf("api key env = %q, want runtimePrep value %q", got, extensionRuntimePrepAPIKeyValue)
	}
	if got := sessionEnvValue(spec.Env, extensionRuntimeContractIssueEnv); got != "issue-42" {
		t.Fatalf("%s = %q, want contract value", extensionRuntimeContractIssueEnv, got)
	}

	transport.conn.mu.Lock()
	newSession := cloneACPParams(transport.conn.lastNewSessionParams)
	initialize := cloneACPParams(transport.conn.lastInitializeParamsSnapshot)
	prompts := append([]map[string]any(nil), transport.conn.promptParamsSnapshots...)
	transport.conn.mu.Unlock()
	if initialize == nil {
		t.Fatal("initialize params were not recorded")
	}
	if got := asString(newSession["cwd"]); got != extensionRuntimeContractCWD {
		t.Fatalf("session/new cwd = %q, want contract cwd", got)
	}
	servers, _ := newSession["mcpServers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("session/new MCP servers = %#v, want stdio plus HTTP contract entries", servers)
	}
	byName := map[string]map[string]any{}
	for _, raw := range servers {
		server, _ := raw.(map[string]any)
		byName[asString(server["name"])] = server
	}
	if byName["workflow_report"]["command"] != "workflow-report" {
		t.Fatalf("stdio MCP = %#v", byName["workflow_report"])
	}
	if byName["audit"]["type"] != "http" || byName["audit"]["url"] != "http://127.0.0.1:9/mcp" {
		t.Fatalf("HTTP MCP = %#v", byName["audit"])
	}

	calls := transport.conn.setConfigOptionCalls()
	if len(calls) != 1 {
		t.Fatalf("config option calls = %#v, want one model update", calls)
	}
	if asString(calls[0]["configId"]) != "model" || asString(calls[0]["value"]) != extensionRuntimeContractModel {
		t.Fatalf("model config option = %#v, want %s", calls[0], extensionRuntimeContractModel)
	}

	if len(prompts) != 2 {
		t.Fatalf("provider prompt count = %d, want 2", len(prompts))
	}
	first := acpTestPromptText(prompts[0])
	if !strings.Contains(first, "first user prompt") || !strings.Contains(first, extensionRuntimeContractSystemPrompt) {
		t.Fatalf("first provider prompt = %q, want user content plus contract system prompt", first)
	}
	second := acpTestPromptText(prompts[1])
	if !strings.Contains(second, "second user prompt") || strings.Contains(second, extensionRuntimeContractSystemPrompt) {
		t.Fatalf("second provider prompt = %q, want user content without repeated system prompt", second)
	}
}

func TestExtensionAdapterWithoutRuntimeContractKeepsSessionNewGolden(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-no-contract")
	adapter := newExtensionRuntimeContractTestAdapter(transport)
	session := extensionRuntimeContractTestSession()

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}

	transport.conn.mu.Lock()
	newSession := cloneACPParams(transport.conn.lastNewSessionParams)
	transport.conn.mu.Unlock()
	if got := asString(newSession["cwd"]); got != "/workspace/room-1" {
		t.Fatalf("session/new cwd = %q, want pre-change golden /workspace/room-1", got)
	}
	servers, _ := newSession["mcpServers"].([]any)
	if len(servers) != 0 {
		t.Fatalf("session/new MCP servers = %#v, want pre-change empty golden", servers)
	}
	if len(transport.specs) != 1 {
		t.Fatalf("process starts = %d, want 1", len(transport.specs))
	}
	spec := transport.specs[0]
	if spec.CWD != "/workspace/room-1" {
		t.Fatalf("process cwd = %q, want session cwd", spec.CWD)
	}
	if got := sessionEnvValue(spec.Env, extensionRuntimeHomeEnv); got != extensionRuntimePrepHomeValue {
		t.Fatalf("home env = %q, want runtimePrep value", got)
	}
	if got := sessionEnvValue(spec.Env, extensionRuntimeContractIssueEnv); got != "" {
		t.Fatalf("%s = %q, want empty without contract", extensionRuntimeContractIssueEnv, got)
	}
	if calls := transport.conn.setConfigOptionCalls(); len(calls) != 0 {
		t.Fatalf("config option calls = %#v, want none without contract model", calls)
	}
}

func TestExtensionAdapterResumeLoadIncludesContractMCPAndFailsWithLegacyCode(t *testing.T) {
	t.Parallel()

	t.Run("load receives contract MCP", func(t *testing.T) {
		t.Parallel()
		transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-load")
		transport.conn.supportsLoadSession = true
		adapter := newExtensionRuntimeContractTestAdapter(transport)
		session := extensionRuntimeContractTestSession()
		path := writeExtensionRuntimeContractFile(t)
		session.RuntimeContext = map[string]any{"rndmaster": map[string]any{
			"contractFile": path, "resumeProviderSessionId": "legacy-provider-session",
		}}
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatalf("Start: %v", err)
		}
		transport.conn.mu.Lock()
		load := cloneACPParams(transport.conn.lastLoadSessionParams)
		newSession := cloneACPParams(transport.conn.lastNewSessionParams)
		transport.conn.mu.Unlock()
		if newSession != nil {
			t.Fatalf("session/new params = %#v, want session/load for resumeProviderSessionId", newSession)
		}
		if asString(load["sessionId"]) != "legacy-provider-session" {
			t.Fatalf("session/load sessionId = %q", load["sessionId"])
		}
		if asString(load["cwd"]) != extensionRuntimeContractCWD {
			t.Fatalf("session/load cwd = %q, want contract cwd", load["cwd"])
		}
		servers, _ := load["mcpServers"].([]any)
		if len(servers) != 2 {
			t.Fatalf("session/load MCP servers = %#v, want stdio plus HTTP contract entries", servers)
		}
	})

	t.Run("load failure returns legacy_session_unavailable", func(t *testing.T) {
		t.Parallel()
		transport := newStandardACPTransport("DeepSeek Harness", "dsh-session-load-fail")
		transport.conn.supportsLoadSession = true
		transport.conn.loadSessionError = &acpError{Code: -32002, Message: "Resource not found"}
		adapter := newExtensionRuntimeContractTestAdapter(transport)
		session := extensionRuntimeContractTestSession()
		path := writeExtensionRuntimeContractFile(t)
		session.RuntimeContext = map[string]any{"rndmaster": map[string]any{
			"contractFile": path, "resumeProviderSessionId": "missing-provider-session",
		}}
		_, err := adapter.Start(context.Background(), session)
		if AppErrorCode(err) != AppErrorLegacySessionUnavailable {
			t.Fatalf("app error code = %q, want %q (err=%v)", AppErrorCode(err), AppErrorLegacySessionUnavailable, err)
		}
	})
}

// 没有 extension:* 目标时合同只贡献 mcpServers：env/cwd/model/systemPrompt 一概不覆盖，
// 但那台 stdio MCP 必须进 session/new——否则托管 claude 会话里没有后端工具。
func TestStandardACPAdapterMergesOnlyContractMCPWithoutExtensionTarget(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-contract-ignored")
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	session.ProviderSessionID = ""
	bindExtensionRuntimeContract(&session, writeExtensionRuntimeContractFile(t))
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	transport.conn.mu.Lock()
	newSession := cloneACPParams(transport.conn.lastNewSessionParams)
	transport.conn.mu.Unlock()
	if got := asString(newSession["cwd"]); got != "/workspace/room-1" {
		t.Fatalf("session/new cwd = %q, want unchanged without extension:* target", got)
	}
	servers, _ := newSession["mcpServers"].([]any)
	names := make([]string, 0, len(servers))
	for _, raw := range servers {
		names = append(names, asString(payloadObject(raw)["name"]))
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "audit" || names[1] != "workflow_report" {
		t.Fatalf("session/new MCP servers = %#v, want the contract servers merged", servers)
	}
}
