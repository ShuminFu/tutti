package agentruntime

import (
	"context"
	"strings"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestACPStopReasonTruncatedOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		stopReason string
		want       bool
	}{
		{name: "output ceiling reached", stopReason: "max_tokens", want: true},
		{name: "normal end of turn", stopReason: "end_turn"},
		{name: "canceled", stopReason: "canceled"},
		{name: "refusal", stopReason: "refusal"},
		{name: "turn request budget", stopReason: "max_turn_requests"},
		{name: "absent", stopReason: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acpStopReasonTruncatedOutput(tc.stopReason); got != tc.want {
				t.Fatalf("acpStopReasonTruncatedOutput(%q) = %v, want %v", tc.stopReason, got, tc.want)
			}
		})
	}
}

// A truncated turn is not a dropped connection, so its notice and its resume
// prompt have to name the output limit instead of transport wording.
func TestACPTruncatedOutputNoticeAndPromptContent(t *testing.T) {
	t.Parallel()

	session := standardTestSession(hermesExtensionTestProvider)
	event, ok := acpTruncatedOutputNoticeEvent(session, "turn-1", 1)
	if !ok {
		t.Fatal("acpTruncatedOutputNoticeEvent returned no event")
	}
	if got := asString(event.Payload.Metadata["noticeKind"]); got != "output_truncated" {
		t.Fatalf("noticeKind = %q, want output_truncated", got)
	}
	if got := asString(event.Payload.Metadata["detail"]); got != acpTruncatedOutputError {
		t.Fatalf("notice detail = %q, want %q", got, acpTruncatedOutputError)
	}
	if got := asString(event.Payload.Metadata["title"]); !strings.Contains(got, "output token limit") {
		t.Fatalf("notice title = %q, want the output-limit wording", got)
	}

	text := asString(acpAutoContinueTruncatedPromptContent()[0]["text"])
	if !strings.Contains(text, "output token limit") {
		t.Fatalf("truncated continue prompt = %q, want the output-limit wording", text)
	}
	if !strings.Contains(text, "Continue from exactly where it stopped") {
		t.Fatalf("truncated continue prompt = %q, want the resume wording", text)
	}
}

// A provider that reaches its output ceiling mid-turn must be resumed rather
// than left as a dead turn: the provider session still holds the cut-off output.
// The extension adapter stands in for every standard-ACP extension provider
// (deepseek-harness among them) and has no transport-retry opt-in, which is what
// used to make this case fail unexplained.
func TestStandardACPAdapterAutoContinuesAfterOutputTruncation(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-1")
	transport.conn.truncatedOutputPrompts = 1
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "hermes-session-1"

	events, err := adapter.Exec(context.Background(), session, textPrompt("finish the report"), "", "turn-1", nil, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	transport.conn.mu.Lock()
	promptCalls := transport.conn.promptCallCount
	snapshots := append([]map[string]any(nil), transport.conn.promptParamsSnapshots...)
	transport.conn.mu.Unlock()
	if promptCalls != 2 {
		t.Fatalf("prompt calls = %d, want the truncation to send one continue prompt", promptCalls)
	}
	if len(snapshots) < 2 {
		t.Fatalf("prompt snapshots = %d, want at least 2", len(snapshots))
	}
	text := acpTestPromptText(snapshots[1])
	if !strings.Contains(text, "output token limit") {
		t.Fatalf("continue prompt = %q, want the truncation wording", text)
	}

	var sawNotice, sawCompleted, sawFailed bool
	for _, event := range events {
		if event.Type == activityshared.EventMessageAppended &&
			asString(event.Payload.Metadata["noticeKind"]) == "output_truncated" {
			sawNotice = true
		}
		if event.Type == activityshared.EventRootProviderTurnCompleted {
			switch event.Payload.TurnOutcome {
			case string(activityshared.TurnOutcomeCompleted):
				sawCompleted = true
			case string(activityshared.TurnOutcomeFailed):
				sawFailed = true
			}
		}
	}
	if !sawNotice {
		t.Fatalf("events = %v, want an output_truncated system notice", activityEventTypeCounts(events))
	}
	if !sawCompleted || sawFailed {
		t.Fatalf("turn terminal completed=%v failed=%v, want completed only", sawCompleted, sawFailed)
	}
}

// Continuations that also hit the ceiling leave the turn failed, but a failed
// turn has to carry the reason: an unexplained failure card is what made this
// condition look like an agent crash.
func TestStandardACPAdapterOutputTruncationExhaustedNamesTheReason(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-1")
	transport.conn.truncatedOutputPrompts = 100
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "hermes-session-1"

	events, err := adapter.Exec(context.Background(), session, textPrompt("finish the report"), "", "turn-1", nil, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	transport.conn.mu.Lock()
	promptCalls := transport.conn.promptCallCount
	transport.conn.mu.Unlock()
	if want := 1 + acpAutoContinueMaxAttempts; promptCalls != want {
		t.Fatalf("prompt calls = %d, want %d (original + bounded continuations)", promptCalls, want)
	}

	var failedError string
	var sawCompleted bool
	for _, event := range events {
		if event.Type != activityshared.EventRootProviderTurnCompleted {
			continue
		}
		switch event.Payload.TurnOutcome {
		case string(activityshared.TurnOutcomeFailed):
			failedError = asString(event.Payload.Metadata["error"])
		case string(activityshared.TurnOutcomeCompleted):
			sawCompleted = true
		}
	}
	if !strings.Contains(failedError, "provider_output_truncated") {
		t.Fatalf("turn failed error = %q, want the truncation reason", failedError)
	}
	if sawCompleted {
		t.Fatal("turn must not also report completion after exhausting continuations")
	}
}

// A truncation with nothing observable produced nothing to continue from, so it
// fails immediately with the reason instead of spending another provider call.
func TestStandardACPAdapterOutputTruncationWithoutOutputFailsOnce(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("Hermes Agent", "hermes-session-1")
	transport.conn.emptyPromptResult = true
	transport.conn.truncatedOutputPrompts = 1
	adapter := newHermesExtensionTestAdapter(transport)
	session := standardTestSession(hermesExtensionTestProvider)
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "hermes-session-1"

	events, err := adapter.Exec(context.Background(), session, textPrompt("finish the report"), "", "turn-1", nil, nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	transport.conn.mu.Lock()
	promptCalls := transport.conn.promptCallCount
	transport.conn.mu.Unlock()
	if promptCalls != 1 {
		t.Fatalf("prompt calls = %d, want no continue prompt without observable output", promptCalls)
	}

	var failedError string
	for _, event := range events {
		if event.Type == activityshared.EventRootProviderTurnCompleted &&
			event.Payload.TurnOutcome == string(activityshared.TurnOutcomeFailed) {
			failedError = asString(event.Payload.Metadata["error"])
		}
	}
	if !strings.Contains(failedError, "provider_output_truncated") {
		t.Fatalf("turn failed error = %q, want the truncation reason", failedError)
	}
}
