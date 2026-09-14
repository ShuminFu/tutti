package agentruntime

import (
	"context"
	"reflect"
	"testing"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestRuntimePathProjectionPreservesCanonicalInput(t *testing.T) {
	content := []PromptContentBlock{
		{Type: "text", Text: "inspect"},
		{Type: "file", Name: "项目资料", Path: "/Users/me/项目资料 (draft)/"},
		{Type: "file", Name: "report", Path: `C:\Users\me\a b.txt`, SizeBytes: 12},
		{Type: "image", MimeType: "image/png", Data: "aGk="},
		{Type: "skill", Name: "review", Path: "/skills/review/SKILL.md"},
		{Type: "mention", Name: "Artifacts", Path: "mention://workspace-reference/1"},
	}
	original := append([]PromptContentBlock(nil), content...)
	want := append([]PromptContentBlock(nil), content...)
	want[1] = PromptContentBlock{Type: "text", Text: content[1].Path}
	want[2] = PromptContentBlock{Type: "text", Text: content[2].Path}
	projected := projectRuntimePromptContent(content)
	if !reflect.DeepEqual(projected, want) || !reflect.DeepEqual(content, original) {
		t.Fatalf("projection=%#v canonical=%#v", projected, content)
	}
	if !reflect.DeepEqual(projectRuntimePromptContent(projected), projected) {
		t.Fatal("projection must not duplicate paths on a second pass")
	}
	if got := projectRuntimePromptContent(content[1:2]); len(got) != 1 || got[0].Text != content[1].Path {
		t.Fatalf("reference-only input = %#v", got)
	}
}

func TestSubmittedPromptReceiptKeepsOriginalBlocksAndScope(t *testing.T) {
	canonical := []PromptContentBlock{{Type: "file", Name: "资料", Path: "/tmp/资料", SizeBytes: 42}}
	display := "[@资料](/tmp/资料)"
	ctx := withSubmittedPrompt(context.Background(), canonical, display)
	wire := projectRuntimePromptContent(canonical)
	canonical[0].Path = "/should/not/change/snapshot"
	session := Session{RoomID: "room", AgentSessionID: "session", Provider: ProviderCodex}
	event := newUserPromptActivityEvent(ctx, session, wire, "wire display", "/tmp/资料", "turn", nil)
	if event.Payload.Content != display {
		t.Fatalf("visible receipt = %q", event.Payload.Content)
	}
	blocks := event.Payload.Metadata["content"].([]map[string]any)
	if len(blocks) != 1 || blocks[0]["type"] != "file" || blocks[0]["path"] != "/tmp/资料" {
		t.Fatalf("receipt blocks = %#v", blocks)
	}
	if event.Payload.Metadata["displayPrompt"] != display {
		t.Fatalf("receipt metadata = %#v", event.Payload.Metadata)
	}
	other := newUserPromptActivityEvent(context.Background(), session, textPrompt("another request"), "", "another request", "other", nil)
	if other.Payload.Content != "another request" {
		t.Fatalf("prompt snapshot leaked: %#v", other)
	}
	// Goal audit receipts use the same metadata path without the event helper.
	extra := userPromptActivityPayloadExtraFromExecMetadata(ctx, map[string]any{"audit": true})
	if extra["content"].([]map[string]any)[0]["type"] != "file" || extra["audit"] != true {
		t.Fatalf("audit receipt lost canonical content: %#v", extra)
	}
}

type capturedPathPrompt struct {
	content []PromptContentBlock
	receipt activityshared.Event
}

type pathProjectionAdapter struct {
	returnOnlyFinalAdapter
	captured  chan capturedPathPrompt
	validated []PromptContentBlock
}

func (a *pathProjectionAdapter) ValidatePromptContent(_ Session, content []PromptContentBlock) error {
	a.validated = append([]PromptContentBlock(nil), content...)
	return nil
}

func (a *pathProjectionAdapter) Exec(ctx context.Context, session Session, content []PromptContentBlock, display string, turnID string, _ EventSink, _ CommandSnapshotSink) ([]activityshared.Event, error) {
	receipt := newUserPromptActivityEvent(ctx, session, content, display, promptDisplayText(content), turnID, nil)
	a.captured <- capturedPathPrompt{content: content, receipt: receipt}
	return []activityshared.Event{receipt, newTurnActivityEvent(session, EventTurnCompleted, turnID, SessionStatusReady, "", "", nil)}, nil
}

func (a *pathProjectionAdapter) GuideActiveTurn(ctx context.Context, session Session, content []PromptContentBlock, display string, turnID string, emit EventSink, commands CommandSnapshotSink) ([]activityshared.Event, error) {
	return a.Exec(ctx, session, content, display, turnID, emit, commands)
}

func TestControllerProjectsPathsForExecutionAndGuidance(t *testing.T) {
	for _, guidance := range []bool{false, true} {
		t.Run(map[bool]string{false: "execution", true: "guidance"}[guidance], func(t *testing.T) {
			adapter := &pathProjectionAdapter{captured: make(chan capturedPathPrompt, 1)}
			controller := NewController([]Adapter{adapter}, nil)
			started, err := controller.Start(context.Background(), StartInput{RoomID: "room", AgentSessionID: "session", Provider: ProviderCodex})
			if err != nil {
				t.Fatal(err)
			}
			input := ExecInput{RoomID: "room", AgentSessionID: started.Session.AgentSessionID, Content: []PromptContentBlock{{Type: "file", Name: "资料", Path: "/tmp/项目 资料"}}, DisplayPrompt: "[@资料](/tmp/项目 资料)", Guidance: guidance}
			if err := controller.ValidatePromptContent(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if len(adapter.validated) != 1 || adapter.validated[0].Text != "/tmp/项目 资料" {
				t.Fatalf("preflight: %#v", adapter.validated)
			}
			if guidance {
				if _, err := controller.beginTurn(started.Session, "active", func() {}); err != nil {
					t.Fatal(err)
				}
				input.TurnID = "active"
			}
			if _, err := controller.Exec(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			select {
			case captured := <-adapter.captured:
				if len(captured.content) != 1 || captured.content[0].Type != "text" || captured.content[0].Text != "/tmp/项目 资料" {
					t.Fatalf("provider input: %#v", captured.content)
				}
				if captured.receipt.Payload.Metadata["content"].([]map[string]any)[0]["type"] != "file" || captured.receipt.Payload.Content != input.DisplayPrompt {
					t.Fatalf("receipt: %#v", captured.receipt)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("adapter was not called")
			}
		})
	}
}

type asyncPathProjectionAdapter struct{ *pathProjectionAdapter }

func (a *asyncPathProjectionAdapter) ExecAsync(ctx context.Context, session Session, content []PromptContentBlock, display string, turnID string, emit EventSink, commands CommandSnapshotSink) error {
	events, err := a.Exec(ctx, session, content, display, turnID, nil, commands)
	if emit != nil {
		emit(events)
	}
	return err
}

func TestControllerAsyncPathProjection(t *testing.T) {
	adapter := &asyncPathProjectionAdapter{&pathProjectionAdapter{captured: make(chan capturedPathPrompt, 1)}}
	controller := NewController([]Adapter{adapter}, nil)
	started, err := controller.Start(t.Context(), StartInput{RoomID: "room", Provider: ProviderCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Exec(t.Context(), ExecInput{RoomID: "room", AgentSessionID: started.Session.AgentSessionID, Content: []PromptContentBlock{{Type: "file", Name: "folder", Path: "/tmp/folder"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case captured := <-adapter.captured:
		if captured.content[0].Text != "/tmp/folder" || captured.receipt.Payload.Metadata["content"].([]map[string]any)[0]["type"] != "file" {
			t.Fatalf("async: %#v", captured)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("async adapter not called")
	}
}

func TestControllerPathProjectionReachesCodexAcceptanceAndReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "acceptance", true: "replacement"}[replacement], func(t *testing.T) {
			var connection *scriptedAppServerConnection
			controller, _, sessionID := startedEditRetryController(t, func(_ *CodexAppServerAdapter, transport *scriptedAppServerTransport) { connection = transport.conn })
			_, err := controller.Exec(t.Context(), ExecInput{
				RoomID: "room-edit-retry", AgentSessionID: sessionID, TurnID: "path-turn", ClientSubmitID: "path-submit", CanonicalSubmitOccurredAtUnixMS: 1_008,
				Content:                   []PromptContentBlock{{Type: "text", Text: "inspect"}, {Type: "file", Name: "folder", Path: "/tmp/项目 资料"}},
				RequireProviderAcceptance: !replacement, HistoryReplacement: replacement,
			})
			if err != nil {
				t.Fatal(err)
			}
			params := appServerRequestParams(t, connection, appServerMethodTurnStart)
			input := payloadArray(params["input"])
			if len(input) != 2 || payloadString(payloadObject(input[1]), "text") != "/tmp/项目 资料" {
				t.Fatalf("Codex input: %#v", input)
			}
		})
	}
}
