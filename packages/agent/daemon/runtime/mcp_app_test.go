package agentruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func mcpAppTestServers() map[string]MCPAppServer {
	return map[string]MCPAppServer{
		"workflow_report": {Name: "workflow_report", Command: "cliagent-backend"},
		"workflow":        {Name: "workflow", Command: "other"},
	}
}

var showWidgetArgs = map[string]any{"title": "Chart", "widget_code": "<svg></svg>"}

// resolveJSONPointer resolves an RFC 6901 pointer against a decoded JSON value.
func resolveJSONPointer(t *testing.T, document map[string]any, pointer string) any {
	t.Helper()
	if !strings.HasPrefix(pointer, "/") {
		t.Fatalf("pointer %q is not absolute", pointer)
	}
	var current any = document
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("pointer %q walks through non-object %#v", pointer, current)
		}
		current, ok = object[token]
		if !ok {
			t.Fatalf("pointer %q token %q missing in %#v", pointer, token, object)
		}
	}
	return current
}

// storedToolPayload runs the same projection the canonical store applies, then
// round-trips JSON so the pointer is checked against what the GUI receives.
func storedToolPayload(t *testing.T, status string, payload map[string]any) map[string]any {
	t.Helper()
	compacted := canonical.CompactToolCallPayload(status, clonePayload(payload))
	raw, err := json.Marshal(compacted)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func assertMCPToolCall(t *testing.T, payload map[string]any, wantPointer string) {
	t.Helper()
	call, ok := identifyMCPToolCall(payload, mcpAppTestServers())
	if !ok {
		t.Fatalf("identifyMCPToolCall(%#v) = not an MCP call", payload)
	}
	want := mcpAppToolCall{ServerName: "workflow_report", ToolName: "show_widget", ArgumentsPointer: wantPointer}
	if call != want {
		t.Fatalf("call = %#v, want %#v", call, want)
	}
	stored := storedToolPayload(t, "completed", payload)
	if got := resolveJSONPointer(t, stored, call.ArgumentsPointer); !reflect.DeepEqual(got, showWidgetArgs) {
		t.Fatalf("stored payload %s = %#v, want %#v (stored %#v)", call.ArgumentsPointer, got, showWidgetArgs, stored)
	}
}

func TestIdentifyMCPToolCallClaudeShape(t *testing.T) {
	// Shape observed in dev tuttid.db (provider claude-code).
	payload := map[string]any{
		"source":   "runtime",
		"name":     "mcp__workflow_report__show_widget",
		"toolName": "mcp__workflow_report__show_widget",
		"input":    clonePayload(showWidgetArgs),
	}
	assertMCPToolCall(t, payload, MCPAppArgumentsPointerInput)
}

func TestIdentifyMCPToolCallGrokACPShape(t *testing.T) {
	// grok emits a generic use_tool ACP tool call; run it through the real ACP
	// normalizer and message projection instead of hand-writing the payload.
	session := Session{Provider: "acp:grok", AgentSessionID: "agent-1", RoomID: "room-1"}
	event, ok := acpToolCallEventWithID(session, "event-1", "turn-1", map[string]any{
		"toolCallId": "call-1",
		"title":      "use_tool",
		"kind":       "other",
		"status":     "completed",
		"rawInput": map[string]any{
			"variant":    "UseTool",
			"tool_name":  "workflow_report__show_widget",
			"tool_input": clonePayload(showWidgetArgs),
		},
		"rawOutput": map[string]any{"type": "MCP"},
	})
	if !ok {
		t.Fatal("expected grok tool call event")
	}
	update, ok := callMessageUpdateFromSessionEvent(canonical.EventSource{}, event, "agent-1", 1)
	if !ok {
		t.Fatal("expected message update")
	}
	if update.Payload["name"] != "use_tool" {
		t.Fatalf("grok payload name = %#v, want use_tool (payload %#v)", update.Payload["name"], update.Payload)
	}
	assertMCPToolCall(t, update.Payload, MCPAppArgumentsPointerGrokToolInput)
}

func TestIdentifyMCPToolCallCodexShape(t *testing.T) {
	// No codex sample exists in the dev database yet; derive the shape from the
	// app-server mcpToolCall projection (codex_appserver_event_items.go).
	acpUpdate, ok := appServerItemToolCallUpdate(map[string]any{
		"id":        "mcp-1",
		"type":      "mcpToolCall",
		"status":    "completed",
		"server":    "workflow_report",
		"tool":      "show_widget",
		"arguments": clonePayload(showWidgetArgs),
		"result": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "shown"}},
		},
	}, true)
	if !ok {
		t.Fatal("expected codex MCP update")
	}
	session := Session{Provider: "codex", AgentSessionID: "agent-1", RoomID: "room-1"}
	event, ok := acpToolCallEventWithID(session, "event-1", "turn-1", acpUpdate)
	if !ok {
		t.Fatal("expected codex tool call event")
	}
	update, ok := callMessageUpdateFromSessionEvent(canonical.EventSource{}, event, "agent-1", 1)
	if !ok {
		t.Fatal("expected message update")
	}
	assertMCPToolCall(t, update.Payload, MCPAppArgumentsPointerCodexArguments)
}

func TestIdentifyMCPToolCallRejectsNonContractTools(t *testing.T) {
	servers := mcpAppTestServers()
	for name, payload := range map[string]map[string]any{
		"builtin claude tool":   {"name": "Bash", "input": map[string]any{"command": "ls"}},
		"unknown mcp server":    {"name": "mcp__github__create_issue", "input": map[string]any{}},
		"grok builtin":          {"name": "use_tool", "input": map[string]any{"tool_name": "read_file", "tool_input": map[string]any{}}},
		"codex title no args":   {"name": "workflow_report.show_widget", "input": map[string]any{"query": "x"}},
		"server name only":      {"name": "mcp__workflow_report__", "input": map[string]any{}},
		"search title with dot": {"name": "Searching for: workflow_report.show_widget", "input": map[string]any{"arguments": map[string]any{}}},
	} {
		if call, ok := identifyMCPToolCall(payload, servers); ok {
			t.Fatalf("%s: identified %#v, want rejection", name, call)
		}
	}
	// Longest contract server name wins: "workflow_report" must not be read as
	// server "workflow" + tool "report__show_widget".
	call, ok := identifyMCPToolCall(map[string]any{"name": "mcp__workflow_report__show_widget", "input": map[string]any{}}, servers)
	if !ok || call.ServerName != "workflow_report" || call.ToolName != "show_widget" {
		t.Fatalf("call = %#v ok=%v, want workflow_report/show_widget", call, ok)
	}
}

func TestRnDMasterMCPServersInjectMCPAppsCapability(t *testing.T) {
	contract := runtimeInstructionsMCPContract()
	servers, ok := rndmasterMCPServers(contract, nil)
	if !ok {
		t.Fatal("mcpServers missing")
	}
	for _, name := range []string{"stdio-plain", "stdio-keep"} {
		if got := asString(payloadObject(payloadObject(servers[name])["env"])[mcpAppsEnv]); got != "1" {
			t.Fatalf("%s env %s = %q, want 1", name, mcpAppsEnv, got)
		}
	}
	if _, exists := payloadObject(servers["http-remote"])["env"]; exists {
		t.Fatalf("http-remote env = %#v, want untouched", payloadObject(servers["http-remote"])["env"])
	}
	acpServers, _, err := rndmasterACPMCPServers(contract, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := acpEnvValue(acpMCPServerByName(t, acpServers)["stdio-plain"], mcpAppsEnv); got != "1" {
		t.Fatalf("ACP stdio-plain %s = %q, want 1", mcpAppsEnv, got)
	}
	// The source contract must not be mutated by the injection.
	if _, exists := payloadObject(runtimeInstructionsMCPServersFixture()["stdio-plain"])["env"]; exists {
		t.Fatal("fixture mutated")
	}
}

type fakeMCPAppResolver struct {
	mu        sync.Mutex
	known     map[string]bool
	ui        map[string]MCPAppToolUI
	resolves  []MCPAppServer
	onResolve func(server MCPAppServer, done func())
}

func (r *fakeMCPAppResolver) CachedToolUI(server MCPAppServer, toolName string) (MCPAppToolUI, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.known[server.Fingerprint] {
		return MCPAppToolUI{}, false, false
	}
	ui, ok := r.ui[server.Name+"/"+toolName]
	return ui, ok, true
}

func (r *fakeMCPAppResolver) Resolve(server MCPAppServer, done func()) {
	r.mu.Lock()
	r.resolves = append(r.resolves, server)
	hook := r.onResolve
	r.mu.Unlock()
	if hook != nil {
		hook(server, done)
	}
}

type recordingMCPAppReporter struct {
	mu      sync.Mutex
	reports []agentsessionstore.ReportActivityInput
	notify  chan struct{}
}

func (r *recordingMCPAppReporter) Report(_ context.Context, report agentsessionstore.ReportActivityInput) error {
	r.mu.Lock()
	r.reports = append(r.reports, report)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return nil
}

func (r *recordingMCPAppReporter) ReportSubmitProvenance(ctx context.Context, report agentsessionstore.ReportActivityInput) error {
	return r.Report(ctx, report)
}

func writeMCPAppContract(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.json")
	raw, err := json.Marshal(map[string]any{
		"version": 1,
		"mcpConfig": map[string]any{"mcpServers": map[string]any{
			"workflow_report": map[string]any{"command": "cliagent-backend", "args": []any{"--mcp", "--port", "1"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mcpAppTestSession(contractFile string) Session {
	return Session{
		Provider:       "claude-code",
		AgentSessionID: "agent-1",
		RoomID:         "room-1",
		CWD:            "/tmp",
		Env:            []string{"KEEP=1"},
		RuntimeContext: map[string]any{"rndmaster": map[string]any{"contractFile": contractFile}},
	}
}

func showWidgetUpdate(status string) agentsessionstore.WorkspaceAgentMessageUpdate {
	return agentsessionstore.WorkspaceAgentMessageUpdate{
		AgentSessionID: "agent-1",
		MessageID:      "msg-1",
		TurnID:         "turn-1",
		Role:           "assistant",
		Kind:           "tool_call",
		Status:         status,
		Payload: map[string]any{
			"name":  "mcp__workflow_report__show_widget",
			"input": clonePayload(showWidgetArgs),
		},
	}
}

func TestControllerAnnotatesCachedMCPAppSynchronously(t *testing.T) {
	session := mcpAppTestSession(writeMCPAppContract(t))
	server := mcpAppContractServers(session)["workflow_report"]
	if server.Fingerprint == "" || !reflect.DeepEqual(server.Args, []string{"--mcp", "--port", "1"}) {
		t.Fatalf("server = %#v", server)
	}
	if envValueFromList(server.Env, mcpAppsEnv) != "1" || envValueFromList(server.Env, "KEEP") != "1" {
		t.Fatalf("server env = %#v, want session env + TUTTI_MCP_APPS=1", server.Env)
	}
	resolver := &fakeMCPAppResolver{
		known: map[string]bool{server.Fingerprint: true},
		ui:    map[string]MCPAppToolUI{"workflow_report/show_widget": {ResourceURI: "ui://workflow_report/widget", ResourceSHA256: "abc"}},
	}
	controller := &Controller{}
	controller.SetMCPAppResolver(resolver)
	report := agentsessionstore.ReportActivityInput{MessageUpdates: []agentsessionstore.WorkspaceAgentMessageUpdate{
		showWidgetUpdate("completed"),
		{MessageID: "msg-2", Kind: "tool_call", Payload: map[string]any{"name": "Bash", "input": map[string]any{"command": "ls"}}},
	}}
	controller.annotateMCPAppMessageUpdates(session, &report)
	want := map[string]any{
		"serverName":       "workflow_report",
		"toolName":         "show_widget",
		"resourceUri":      "ui://workflow_report/widget",
		"resourceSha256":   "abc",
		"argumentsPointer": "/input",
	}
	if got := report.MessageUpdates[0].Payload[MCPAppPayloadKey]; !reflect.DeepEqual(got, want) {
		t.Fatalf("mcpApp = %#v, want %#v", got, want)
	}
	if _, exists := report.MessageUpdates[1].Payload[MCPAppPayloadKey]; exists {
		t.Fatal("non-MCP tool call was annotated")
	}
	if len(resolver.resolves) != 0 {
		t.Fatalf("cached catalog triggered resolve: %#v", resolver.resolves)
	}
	// The whitelist keeps mcpApp in the stored payload.
	stored := storedToolPayload(t, "completed", report.MessageUpdates[0].Payload)
	if !reflect.DeepEqual(stored[MCPAppPayloadKey], want) {
		t.Fatalf("stored mcpApp = %#v, want %#v", stored[MCPAppPayloadKey], want)
	}
}

func TestControllerRepublishesMCPAppAfterFirstResolution(t *testing.T) {
	session := mcpAppTestSession(writeMCPAppContract(t))
	reporter := &recordingMCPAppReporter{notify: make(chan struct{}, 16)}
	controller := NewController(nil, reporter)
	resolver := &fakeMCPAppResolver{known: map[string]bool{}, ui: map[string]MCPAppToolUI{}}
	resolver.onResolve = func(server MCPAppServer, done func()) {
		go func() {
			resolver.mu.Lock()
			resolver.known[server.Fingerprint] = true
			resolver.ui[server.Name+"/show_widget"] = MCPAppToolUI{ResourceURI: "ui://workflow_report/widget", ResourceSHA256: "sha-1"}
			resolver.mu.Unlock()
			done()
		}()
	}
	controller.SetMCPAppResolver(resolver)

	report := agentsessionstore.ReportActivityInput{
		WorkspaceID:    "room-1",
		MessageUpdates: []agentsessionstore.WorkspaceAgentMessageUpdate{showWidgetUpdate("completed")},
	}
	controller.annotateMCPAppMessageUpdates(session, &report)
	if _, exists := report.MessageUpdates[0].Payload[MCPAppPayloadKey]; exists {
		t.Fatal("unknown catalog must not block or annotate the original update")
	}
	deadline := time.After(2 * time.Second)
	for {
		reporter.mu.Lock()
		count := len(reporter.reports)
		var last agentsessionstore.ReportActivityInput
		if count > 0 {
			last = reporter.reports[count-1]
		}
		reporter.mu.Unlock()
		if count > 0 {
			if len(last.MessageUpdates) != 1 {
				t.Fatalf("republish = %#v", last)
			}
			update := last.MessageUpdates[0]
			if update.MessageID != "msg-1" || update.Kind != "tool_call" || update.Status != "completed" || update.TurnID != "turn-1" {
				t.Fatalf("republished identity = %#v", update)
			}
			mcpApp := payloadObject(update.Payload[MCPAppPayloadKey])
			if mcpApp["resourceSha256"] != "sha-1" || len(update.Payload) != 1 {
				t.Fatalf("republished payload = %#v", update.Payload)
			}
			if last.WorkspaceID != "room-1" {
				t.Fatalf("republished workspace = %q", last.WorkspaceID)
			}
			return
		}
		select {
		case <-reporter.notify:
		case <-deadline:
			t.Fatal("no republish within 2s of resolution (" + strconv.Itoa(count) + " reports)")
		}
	}
}

func TestControllerSkipsMCPAppWithoutContract(t *testing.T) {
	resolver := &fakeMCPAppResolver{}
	controller := &Controller{}
	controller.SetMCPAppResolver(resolver)
	report := agentsessionstore.ReportActivityInput{MessageUpdates: []agentsessionstore.WorkspaceAgentMessageUpdate{showWidgetUpdate("completed")}}
	controller.annotateMCPAppMessageUpdates(Session{AgentSessionID: "agent-1"}, &report)
	if len(resolver.resolves) != 0 {
		t.Fatal("session without RnDMaster contract must not resolve MCP Apps")
	}
}
