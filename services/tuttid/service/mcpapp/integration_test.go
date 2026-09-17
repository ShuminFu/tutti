package mcpapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	tuttiapi "github.com/tutti-os/tutti/services/tuttid/api"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
)

// storeMessageReporter persists runtime message updates into the real agent
// store, the way tuttid's activity projection does for message reports.
type storeMessageReporter struct {
	store *storesqlite.Store
	mu    sync.Mutex
	errs  []error
}

func (r *storeMessageReporter) Report(ctx context.Context, report agentsessionstore.ReportActivityInput) error {
	bySession := map[string][]storesqlite.MessageUpdate{}
	for _, update := range report.MessageUpdates {
		bySession[update.AgentSessionID] = append(bySession[update.AgentSessionID], storesqlite.MessageUpdate{
			MessageID:        update.MessageID,
			TurnID:           update.TurnID,
			Role:             update.Role,
			Kind:             update.Kind,
			Status:           update.Status,
			Payload:          update.Payload,
			OccurredAtUnixMS: update.OccurredAtUnixMS,
		})
	}
	for sessionID, messages := range bySession {
		if _, err := r.store.ReportSessionMessages(ctx, storesqlite.SessionMessageReport{
			WorkspaceID: report.WorkspaceID, AgentSessionID: sessionID,
			Origin: "runtime", Provider: "claude-code", Messages: messages,
		}); err != nil {
			r.mu.Lock()
			r.errs = append(r.errs, err)
			r.mu.Unlock()
			return err
		}
	}
	return nil
}

func (r *storeMessageReporter) ReportSubmitProvenance(ctx context.Context, report agentsessionstore.ReportActivityInput) error {
	return r.Report(ctx, report)
}

// showWidgetAdapter is a provider whose Start emits one completed MCP tool
// call in the claude-code shape.
type showWidgetAdapter struct{}

func (showWidgetAdapter) Provider() string { return "claude-code" }

func (showWidgetAdapter) Start(_ context.Context, session agentruntime.Session) ([]activityshared.Event, error) {
	return []activityshared.Event{{
		EventID:          "event-1",
		Type:             activityshared.EventCallCompleted,
		AgentSessionID:   session.AgentSessionID,
		OccurredAtUnixMS: 1_000,
		Payload: activityshared.EventPayload{
			TurnID: "turn-1",
			CallID: "call-1",
			Name:   "mcp__workflow_report__show_widget",
			Input:  map[string]any{"title": "Chart", "widget_code": "<svg><rect/></svg>"},
			Output: map[string]any{"text": "已在对话里展示『Chart』"},
		},
	}}, nil
}

func (showWidgetAdapter) Resume(context.Context, agentruntime.Session) error { return nil }
func (showWidgetAdapter) Close(context.Context, agentruntime.Session) error  { return nil }
func (showWidgetAdapter) Exec(context.Context, agentruntime.Session, []agentruntime.PromptContentBlock, string, string, agentruntime.EventSink, agentruntime.CommandSnapshotSink) ([]activityshared.Event, error) {
	return nil, nil
}
func (showWidgetAdapter) Cancel(context.Context, agentruntime.Session, string) ([]activityshared.Event, error) {
	return nil, nil
}

func TestFirstShowWidgetCallGetsMCPAppAndSnapshotEndToEnd(t *testing.T) {
	ctx := context.Background()
	store := openSnapshotStore(t)
	if _, err := store.ReportSessionState(ctx, storesqlite.SessionStateReport{
		WorkspaceID: "room-1", AgentSessionID: "agent-1", Origin: "runtime", Provider: "claude-code",
		ProviderSessionID: "provider-1", Status: "running", OccurredAtUnixMS: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := store.RecordTurnTransition(ctx, storesqlite.TurnTransition{
		WorkspaceID: "room-1", AgentSessionID: "agent-1", TurnID: "turn-1",
		Phase: storesqlite.TurnPhaseRunning, OccurredAtUnixMS: 101,
	}); err != nil || !accepted {
		t.Fatalf("RecordTurnTransition accepted=%v err=%v", accepted, err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts.log")
	contractPath := filepath.Join(dir, "contract.json")
	contract, _ := json.Marshal(map[string]any{
		"version": 1,
		"mcpConfig": map[string]any{"mcpServers": map[string]any{
			"workflow_report": map[string]any{
				"command": executable,
				"args":    []any{"-test.run=^$"},
				"env": map[string]any{
					fakeServerModeEnv:           "ui",
					"MCPAPP_FAKE_SERVER_MARKER": marker,
				},
			},
		}},
	})
	if err := os.WriteFile(contractPath, contract, 0o600); err != nil {
		t.Fatal(err)
	}
	// The fake "ui" server (resolver_test.go) advertises show_widget under
	// ui://fake/widget; the contract carries no TUTTI_MCP_APPS, the runtime
	// injects it.

	reporter := &storeMessageReporter{store: store}
	controller := agentruntime.NewController([]agentruntime.Adapter{showWidgetAdapter{}}, reporter)
	controller.SetMCPAppResolver(&Resolver{Transport: agentruntime.NewLocalProcessTransport(), Store: store})

	if _, err := controller.Start(ctx, agentruntime.StartInput{
		RoomID:         "room-1",
		AgentSessionID: "agent-1",
		Provider:       "claude-code",
		CWD:            dir,
		RuntimeContext: map[string]any{"rndmaster": map[string]any{"contractFile": contractPath}},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	toolCompleted := time.Now()

	var message storesqlite.Message
	for {
		page, _, err := store.ListSessionMessages(ctx, storesqlite.ListSessionMessagesInput{
			WorkspaceID: "room-1", AgentSessionID: "agent-1", MessageID: "toolcall:call-1", Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) == 1 {
			if _, ok := page.Messages[0].Payload["mcpApp"]; ok {
				message = page.Messages[0]
				break
			}
		}
		if time.Since(toolCompleted) > 2*time.Second {
			reporter.mu.Lock()
			errs := reporter.errs
			reporter.mu.Unlock()
			t.Fatalf("payload has no mcpApp within 2s of tool completion; messages=%#v reporter errors=%v", page.Messages, errs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("mcpApp landed %s after tool completion (message version %d)", time.Since(toolCompleted), message.Version)
	if message.Version < 2 {
		t.Fatalf("mcpApp should arrive through a republished message version, got version %d", message.Version)
	}

	mcpApp, _ := message.Payload["mcpApp"].(map[string]any)
	if mcpApp["serverName"] != "workflow_report" || mcpApp["toolName"] != "show_widget" ||
		mcpApp["resourceUri"] != "ui://fake/widget" || mcpApp["argumentsPointer"] != "/input" {
		t.Fatalf("mcpApp = %#v", mcpApp)
	}
	if message.Status != "completed" || message.Payload["input"] == nil || message.Payload["output"] == nil {
		t.Fatalf("republish clobbered the original tool call: %#v", message)
	}
	if input, _ := message.Payload["input"].(map[string]any); input["widget_code"] != "<svg><rect/></svg>" {
		t.Fatalf("arguments at /input = %#v", message.Payload["input"])
	}

	sha, _ := mcpApp["resourceSha256"].(string)
	mux := http.NewServeMux()
	tuttiapi.RegisterRoutes(mux, tuttiapi.NewRoutes(tuttiapi.DaemonAPI{AgentMCPAppResources: store}))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/workspaces/room-1/agent-mcp-app-resources/"+sha, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("snapshot route status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var resource tuttigenerated.WorkspaceAgentMcpAppResourceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}
	if resource.Html != fakeWidgetHTML || resource.Uri != "ui://fake/widget" || resource.Meta.Csp == nil ||
		strings.Join(*resource.Meta.Csp.ResourceDomains, ",") != "https://registry.npmmirror.com" {
		t.Fatalf("snapshot resource = %#v", resource)
	}

	raw, _ := os.ReadFile(marker)
	if !strings.Contains(string(raw), "start apps=1") {
		t.Fatalf("resolver launched the server without TUTTI_MCP_APPS=1: %q", raw)
	}
}
