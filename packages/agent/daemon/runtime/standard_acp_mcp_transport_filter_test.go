package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMCPTransportContractFile(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"version": 1,
		"cwd":     "/issue/workspace",
		"mcpConfig": map[string]any{
			"mcpServers": map[string]any{
				"workflow_report": map[string]any{
					"command": "workflow-report",
					"args":    []any{"--stdio"},
				},
				"loopback_report": map[string]any{
					"type": "http",
					"url":  "http://127.0.0.1:18786/mcp",
					"headers": []any{
						map[string]any{"name": "X-Rndmaster-Role-ID", "value": "engineer"},
						map[string]any{"name": "X-Cliagent-Task-ID", "value": "task_1"},
						map[string]any{"name": "X-Automation-Run-ID", "value": "run_1"},
					},
				},
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

func newStdioDeniedExtensionAdapter(transport *standardACPTransport) *standardACPAdapter {
	adapter, err := NewStandardACPAdapter(StandardACPAdapterConfig{
		Provider:         "acp:deepseek-harness",
		Name:             "deepseek-harness-acp",
		DisplayName:      "DeepSeek Harness",
		Command:          []string{"dsh", "acp"},
		AgentTargetID:    "extension:deepseek-harness",
		DeclaredHTTPMCP:  true,
		DeclaredStdioMCP: boolPtr(false),
	}, transport, LegacyHostMetadata())
	if err != nil {
		panic(err)
	}
	return adapter.(*standardACPAdapter)
}

func newUndeclaredStdioExtensionAdapter(transport *standardACPTransport) *standardACPAdapter {
	adapter, err := NewStandardACPAdapter(StandardACPAdapterConfig{
		Provider:      "acp:grok",
		Name:          "grok-acp",
		DisplayName:   "Grok Build",
		Command:       []string{"grok", "acp"},
		AgentTargetID: "extension:grok",
	}, transport, LegacyHostMetadata())
	if err != nil {
		panic(err)
	}
	return adapter.(*standardACPAdapter)
}

func sessionNewMCPByName(t *testing.T, transport *standardACPTransport) map[string]map[string]any {
	t.Helper()
	transport.conn.mu.Lock()
	newSession := cloneACPParams(transport.conn.lastNewSessionParams)
	transport.conn.mu.Unlock()
	servers, _ := newSession["mcpServers"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range servers {
		server, _ := raw.(map[string]any)
		byName[asString(server["name"])] = server
	}
	return byName
}

func TestExtensionAdapterDropsStdioMCPWhenDeclaredFalse(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	transport := newStandardACPTransport("DeepSeek Harness", "dsh-http-only")
	adapter := newStdioDeniedExtensionAdapter(transport)
	session := standardTestSession("acp:deepseek-harness")
	session.ProviderSessionID = ""
	session.AgentTargetID = "extension:deepseek-harness"
	bindExtensionRuntimeContract(&session, writeMCPTransportContractFile(t))

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	byName := sessionNewMCPByName(t, transport)
	if _, ok := byName["workflow_report"]; ok {
		t.Fatalf("stdio MCP survived filter: %#v", byName)
	}
	if len(byName) != 1 {
		t.Fatalf("session/new MCP servers = %#v, want the single HTTP entry", byName)
	}
	httpServer := byName["loopback_report"]
	if httpServer["type"] != "http" || httpServer["url"] != "http://127.0.0.1:18786/mcp" {
		t.Fatalf("HTTP MCP = %#v", httpServer)
	}
	headers, _ := httpServer["headers"].([]any)
	got := map[string]string{}
	for _, raw := range headers {
		item, _ := raw.(map[string]any)
		got[asString(item["name"])] = asString(item["value"])
	}
	for name, value := range map[string]string{
		"X-Rndmaster-Role-ID": "engineer",
		"X-Cliagent-Task-ID":  "task_1",
		"X-Automation-Run-ID": "run_1",
	} {
		if got[name] != value {
			t.Fatalf("header %s=%q want %q in %#v", name, got[name], value, headers)
		}
	}
	logged := logs.String()
	if !strings.Contains(logged, "workflow_report") || !strings.Contains(logged, "extension:deepseek-harness") {
		t.Fatalf("WARN missing server name or target id: %s", logged)
	}
}

func TestExtensionAdapterKeepsStdioMCPWhenUndeclared(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Grok Build", "grok-stdio-kept")
	adapter := newUndeclaredStdioExtensionAdapter(transport)
	session := standardTestSession("acp:grok")
	session.ProviderSessionID = ""
	session.AgentTargetID = "extension:grok"
	bindExtensionRuntimeContract(&session, writeMCPTransportContractFile(t))

	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	byName := sessionNewMCPByName(t, transport)
	if byName["workflow_report"]["command"] != "workflow-report" {
		t.Fatalf("stdio MCP missing: %#v", byName)
	}
	if byName["loopback_report"]["type"] != "http" || byName["loopback_report"]["url"] != "http://127.0.0.1:18786/mcp" {
		t.Fatalf("HTTP MCP missing: %#v", byName)
	}
	if len(byName) != 2 {
		t.Fatalf("session/new MCP servers = %#v, want stdio plus HTTP", byName)
	}
}
