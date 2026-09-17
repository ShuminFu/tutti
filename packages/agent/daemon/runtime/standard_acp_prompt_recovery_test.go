package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestClaudeCodeACPPromptRecoversFromRootTranscript(t *testing.T) {
	prevSilence := claudeACPPromptEvidenceSilence
	prevInterval := claudeACPPromptEvidenceInterval
	claudeACPPromptEvidenceSilence = 20 * time.Millisecond
	claudeACPPromptEvidenceInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		claudeACPPromptEvidenceSilence = prevSilence
		claudeACPPromptEvidenceInterval = prevInterval
	})

	cwd := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	transport := newStandardACPTransport("Claude Agent", "claude-prompt-recover-1")
	transport.conn.holdPromptResult = true
	transport.conn.promptResultUpdates = []map[string]any{
		{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "Inspecting files."},
		},
	}
	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		transport,
		LegacyHostMetadata(),
		nil,
	)
	session := standardTestSession(ProviderClaudeCode)
	session.CWD = cwd
	session.ProviderSessionID = "claude-prompt-recover-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	acpSession := adapter.getSession(session.AgentSessionID)
	if acpSession == nil || acpSession.providerSessionID == "" {
		t.Fatal("ACP session was not started")
	}
	session.ProviderSessionID = acpSession.providerSessionID

	transcriptDir := filepath.Join(configDir, "projects", claudeACPProjectID(cwd))
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatalf("mkdir transcript: %v", err)
	}
	transcript := `{"type":"user","isSidechain":false,"message":{"content":[{"type":"text","text":"inspect"}]}}
{"type":"assistant","isSidechain":false,"message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Inspecting files."}]}}
`
	if err := os.WriteFile(filepath.Join(transcriptDir, session.ProviderSessionID+".jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	done := make(chan []activityshared.Event, 1)
	errCh := make(chan error, 1)
	go func() {
		events, err := adapter.Exec(context.Background(), session, textPrompt("inspect"), "", "turn-recover-1", nil, nil)
		errCh <- err
		done <- events
	}()
	var events []activityshared.Event
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Exec: %v", err)
		}
		events = <-done
	case <-time.After(3 * time.Second):
		t.Fatal("Exec did not recover from the provider transcript")
	}
	completed := eventsOfType(events, activityshared.EventRootProviderTurnCompleted)
	if len(completed) != 1 || completed[0].Payload.TurnOutcome != string(activityshared.TurnOutcomeCompleted) {
		t.Fatalf("recovered events = %#v", activityEventTypeCounts(events))
	}
}

func TestClaudeCodeACPPromptDoesNotRecoverWhilePermissionIsPending(t *testing.T) {
	prevSilence := claudeACPPromptEvidenceSilence
	prevInterval := claudeACPPromptEvidenceInterval
	claudeACPPromptEvidenceSilence = 20 * time.Millisecond
	claudeACPPromptEvidenceInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		claudeACPPromptEvidenceSilence = prevSilence
		claudeACPPromptEvidenceInterval = prevInterval
	})

	cwd := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	transport := newStandardACPTransport("Claude Agent", "claude-prompt-permission-1")
	transport.conn.promptPermission = true
	adapter := newClaudeCodeACPAdapterFromProviderDescriptor(
		claudeCodeTestDescriptor(t),
		transport,
		LegacyHostMetadata(),
		nil,
	)
	session := standardTestSession(ProviderClaudeCode)
	session.CWD = cwd
	session.ProviderSessionID = "claude-prompt-permission-1"
	session.PermissionModeID = "default"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := adapter.ApplyPermissionMode(context.Background(), session); err != nil {
		t.Fatalf("ApplyPermissionMode: %v", err)
	}
	acpSession := adapter.getSession(session.AgentSessionID)
	if acpSession == nil || acpSession.providerSessionID == "" {
		t.Fatal("ACP session was not started")
	}
	session.ProviderSessionID = acpSession.providerSessionID

	transcriptDir := filepath.Join(configDir, "projects", claudeACPProjectID(cwd))
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatalf("mkdir transcript: %v", err)
	}
	transcript := `{"type":"assistant","isSidechain":false,"message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Inspecting files."}]}}
`
	if err := os.WriteFile(filepath.Join(transcriptDir, session.ProviderSessionID+".jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	var streamed []activityshared.Event
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	events, err := adapter.Exec(ctx, session, textPrompt("inspect"), "", "turn-permission-1", func(next []activityshared.Event) {
		streamed = append(streamed, next...)
	}, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	all := append(streamed, events...)
	for _, event := range eventsOfType(all, activityshared.EventRootProviderTurnCompleted) {
		if event.Payload.TurnOutcome == string(activityshared.TurnOutcomeCompleted) {
			t.Fatalf("permission-waiting turn settled as completed: %#v", event)
		}
	}
}
