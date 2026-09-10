package agentruntime

import (
	"os"
	"path/filepath"
	"testing"
)

const managedContractMCPFixture = `{
  "version": 1,
  "provider": "claude",
  "systemPrompt": "contract prompt",
  "cwd": "/contract/cwd",
  "model": "contract-model",
  "env": {"ANTHROPIC_AUTH_TOKEN": "contract-token"},
  "mcpConfig": {"mcpServers": {"workflow_report": {
    "command": "/opt/cliagent-backend",
    "args": ["--mcp", "--port", "18788"]
  }}}
}`

func writeManagedContract(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(managedContractMCPFixture), 0o600); err != nil {
		t.Fatalf("write contract: %v", err)
	}
	return path
}

func managedClaudeSession(contractPath string) Session {
	return Session{
		AgentSessionID: "sess-1",
		AgentTargetID:  "local:claude-code",
		CWD:            "/session/cwd",
		Env:            []string{"KEEP=1"},
		RuntimeContext: map[string]any{
			"rndmaster": map[string]any{"contractFile": contractPath},
		},
	}
}

// 托管 claude 走 standard-acp，既不是 cursor 也不是 extension:*；合同里的 stdio MCP
// 必须出现在 session/new 的 mcpServers 里，否则会话内没有 peer_send/peer_list。
func TestPrepareRnDMasterACPSessionMergesContractMCPForManagedClaude(t *testing.T) {
	adapter := &standardACPAdapter{config: standardACPConfig{
		provider:    ProviderClaudeCode,
		adapterName: "claude-agent-acp",
	}}
	session := managedClaudeSession(writeManagedContract(t))

	prepared, contract, err := adapter.prepareRnDMasterACPSession(session)
	if err != nil {
		t.Fatalf("prepareRnDMasterACPSession: %v", err)
	}
	servers, httpUnsupported, err := adapter.mergeFilteredContractMCP(prepared, contract, nil)
	if err != nil {
		t.Fatalf("mergeFilteredContractMCP: %v", err)
	}
	if httpUnsupported {
		t.Fatalf("stdio-only contract must not require HTTP MCP")
	}
	if len(servers) != 1 {
		t.Fatalf("mcpServers = %#v, want the contract stdio server", servers)
	}
	entry := payloadObject(servers[0])
	if asString(entry["name"]) != "workflow_report" {
		t.Fatalf("mcp server name = %#v, want workflow_report", entry["name"])
	}
	if asString(entry["command"]) != "/opt/cliagent-backend" {
		t.Fatalf("mcp server command = %#v, want /opt/cliagent-backend", entry["command"])
	}
}

// 只合并 MCP：合同里的 env/cwd/model 不得顶掉会话自己的那份（宿主托管凭据、网关模型）。
func TestPrepareRnDMasterACPSessionKeepsManagedClaudeRuntimeEnvAndCWD(t *testing.T) {
	adapter := &standardACPAdapter{config: standardACPConfig{
		provider:    ProviderClaudeCode,
		adapterName: "claude-agent-acp",
	}}
	session := managedClaudeSession(writeManagedContract(t))

	prepared, contract, err := adapter.prepareRnDMasterACPSession(session)
	if err != nil {
		t.Fatalf("prepareRnDMasterACPSession: %v", err)
	}
	if prepared.CWD != "/session/cwd" {
		t.Fatalf("cwd = %q, want the session cwd", prepared.CWD)
	}
	if len(prepared.Env) != 1 || prepared.Env[0] != "KEEP=1" {
		t.Fatalf("env = %#v, want the session env untouched", prepared.Env)
	}
	if prepared.Settings != nil && prepared.Settings.Model != "" {
		t.Fatalf("model = %q, want the contract model ignored", prepared.Settings.Model)
	}
	if contract.SystemPrompt != "" || len(contract.Env) != 0 ||
		contract.CWD != "" || contract.Model != "" {
		t.Fatalf("contract = %#v, want mcpConfig only", contract)
	}
}
