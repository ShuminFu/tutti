package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeInstructionsResolver is an MCPAppResolver that also answers server
// instructions, recording the servers it was asked about.
type fakeInstructionsResolver struct {
	mu      sync.Mutex
	text    map[string]string
	err     error
	servers []MCPAppServer
}

func (*fakeInstructionsResolver) CachedToolUI(MCPAppServer, string) (MCPAppToolUI, bool, bool) {
	return MCPAppToolUI{}, false, true
}

func (*fakeInstructionsResolver) Resolve(MCPAppServer, func()) {}

func (r *fakeInstructionsResolver) ServerInstructions(_ context.Context, server MCPAppServer) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers = append(r.servers, server)
	if r.err != nil {
		return "", r.err
	}
	return r.text[server.Name], nil
}

// catalogOnlyResolver predates MCPServerInstructionsResolver.
type catalogOnlyResolver struct{}

func (catalogOnlyResolver) CachedToolUI(MCPAppServer, string) (MCPAppToolUI, bool, bool) {
	return MCPAppToolUI{}, false, true
}

func (catalogOnlyResolver) Resolve(MCPAppServer, func()) {}

const testRuntimeInstructionsFile = "/runs/session-1/runtime-instructions.md"

func contractSessionWithMCPServers(t *testing.T, servers map[string]any) Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.json")
	raw, err := json.Marshal(map[string]any{
		"version":   1,
		"provider":  "codex",
		"mcpConfig": map[string]any{"mcpServers": servers},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	session := testAppServerSession()
	session.RuntimeContext = map[string]any{"rndmaster": map[string]any{"contractFile": path}}
	session.Env = []string{runtimeInstructionsFileEnv + "=" + testRuntimeInstructionsFile}
	return session
}

// Regression (G6): a codex session never saw the RnDMaster MCP server's
// initialize instructions (e.g. "use show_widget, read show_widget_guide
// first"), because the codex app-server does not put them into the prompt.
func TestCodexAppServerTurnCarriesContractMCPServerInstructions(t *testing.T) {
	t.Parallel()

	adapter, transport, _ := startedAppServerAdapter(t)
	controller := NewController([]Adapter{adapter}, nil)
	resolver := &fakeInstructionsResolver{text: map[string]string{
		"workflow_report": "draw with show_widget; read show_widget_guide first",
	}}
	controller.SetMCPAppResolver(resolver)

	session := contractSessionWithMCPServers(t, map[string]any{
		"workflow_report": map[string]any{"command": "cliagent-backend", "args": []any{"--mcp"}},
		"silent":          map[string]any{"command": "silent-server"},
	})
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Exec(context.Background(), session, textPrompt("draw the flow"), "", "turn-local-1", nil, nil); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	turnStart := appServerRequestParams(t, transport.conn, appServerMethodTurnStart)
	// Delivered on codex's developer-instructions channel, after the mode's own text.
	developer := asString(payloadObject(payloadObject(turnStart["collaborationMode"])["settings"])["developer_instructions"])
	for _, want := range []string{
		"\n\n# MCP Server Instructions\n",
		"\n\n## workflow_report\n\ndraw with show_widget; read show_widget_guide first",
	} {
		if !strings.Contains(developer, want) {
			t.Fatalf("developer_instructions missing %q: %q", want, developer)
		}
	}
	if strings.Contains(developer, "## silent") {
		t.Fatalf("server without instructions rendered a section: %q", developer)
	}
	// The user's own input stays untouched as the first input item.
	input := payloadArray(turnStart["input"])
	if len(input) == 0 || asString(payloadObject(input[0])["text"]) != "draw the flow" {
		t.Fatalf("turn/start input = %#v", turnStart["input"])
	}

	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if len(resolver.servers) != 2 {
		t.Fatalf("resolver asked about %d servers, want 2", len(resolver.servers))
	}
	for _, server := range resolver.servers {
		// codex already reads DinTalDock's runtime policy from CODEX_HOME/AGENTS.md;
		// the probe must not make the server echo it a second time.
		if got := envValueFromList(server.Env, runtimeInstructionsFileEnv); got != "" {
			t.Fatalf("%s probe env %s=%q, want blank", server.Name, runtimeInstructionsFileEnv, got)
		}
		if got := envValueFromList(server.Env, mcpAppsEnv); got != "1" {
			t.Fatalf("%s probe env %s=%q, want the provider's value", server.Name, mcpAppsEnv, got)
		}
		catalog := mcpAppContractServers(session)[server.Name]
		if server.Fingerprint == "" || server.Fingerprint == catalog.Fingerprint {
			t.Fatalf("%s probe fingerprint %q must differ from the catalog probe", server.Name, server.Fingerprint)
		}
	}
}

func TestContractMCPServerInstructionsFailOpen(t *testing.T) {
	t.Parallel()

	session := contractSessionWithMCPServers(t, map[string]any{
		"workflow_report": map[string]any{"command": "cliagent-backend"},
	})
	cases := map[string]MCPAppResolver{
		"no resolver":           nil,
		"catalog-only resolver": catalogOnlyResolver{},
		"resolver error":        &fakeInstructionsResolver{err: errors.New("initialize: boom")},
	}
	for name, resolver := range cases {
		controller := NewController(nil, nil)
		controller.SetMCPAppResolver(resolver)
		if got := controller.contractMCPServerInstructions(context.Background(), session); got != "" {
			t.Fatalf("%s: instructions = %q, want empty", name, got)
		}
	}

	controller := NewController(nil, nil)
	controller.SetMCPAppResolver(&fakeInstructionsResolver{text: map[string]string{"workflow_report": "x"}})
	if got := controller.contractMCPServerInstructions(context.Background(), testAppServerSession()); got != "" {
		t.Fatalf("session without contract: instructions = %q, want empty", got)
	}
}

func TestMCPServerInstructionsProbeKeepsServerDeclaredInstructionsFile(t *testing.T) {
	t.Parallel()

	session := contractSessionWithMCPServers(t, map[string]any{
		"own": map[string]any{
			"command": "server",
			"env":     map[string]any{runtimeInstructionsFileEnv: "/server/own.md"},
		},
	})
	server := mcpInstructionsProbeServers(session)["own"]
	if got := envValueFromList(server.Env, runtimeInstructionsFileEnv); got != "/server/own.md" {
		t.Fatalf("server-declared %s = %q, want kept", runtimeInstructionsFileEnv, got)
	}
}

// claude-agent-acp and grok already put MCP instructions into the prompt; only
// the codex app-server path may opt in, or they would get the text twice.
func TestOnlyAppServerAdaptersOptIntoMCPServerInstructions(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, adapter := range newMigratedProviderAdapters(nil, LegacyHostMetadata(), nil, nil) {
		_, optedIn := adapter.(MCPServerInstructionsSourceAdapter)
		_, appServer := adapter.(*CodexAppServerAdapter)
		if optedIn != appServer {
			t.Fatalf("provider %q (%T) opted in = %v, want %v", adapter.Provider(), adapter, optedIn, appServer)
		}
		seen[adapter.Provider()] = optedIn
		t.Logf("provider %q (%T) opted in = %v", adapter.Provider(), adapter, optedIn)
	}
	if optedIn, ok := seen[ProviderCodex]; !ok || !optedIn {
		t.Fatalf("codex opted in = %v (present %v), want true", optedIn, ok)
	}
	if optedIn, ok := seen[ProviderClaudeCode]; !ok || optedIn {
		t.Fatalf("claude-code opted in = %v (present %v), want false", optedIn, ok)
	}
}
