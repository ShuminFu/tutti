package agentextension

import (
	"encoding/json"
	"testing"
)

func TestComposerDeclaredStdioMCPDistinguishesUndeclared(t *testing.T) {
	t.Parallel()

	var undeclared ComposerProfile
	if err := json.Unmarshal([]byte(`{"schemaVersion":"tutti.agent.composer.v1","acp":{"mcpCapabilities":{"http":true}}}`), &undeclared); err != nil {
		t.Fatal(err)
	}
	if undeclared.DeclaredStdioMCP() != nil {
		t.Fatal("undeclared stdio must stay nil so grok keeps stdio MCP")
	}
	if !undeclared.DeclaresHTTPMCP() {
		t.Fatal("http:true must still declare HTTP MCP")
	}

	var denied ComposerProfile
	if err := json.Unmarshal([]byte(`{"schemaVersion":"tutti.agent.composer.v1","acp":{"mcpCapabilities":{"http":true,"stdio":false}}}`), &denied); err != nil {
		t.Fatal(err)
	}
	if denied.DeclaredStdioMCP() == nil || *denied.DeclaredStdioMCP() {
		t.Fatal("stdio:false must be a declared false, not undeclared")
	}
}
