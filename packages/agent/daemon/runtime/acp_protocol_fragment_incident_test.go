package agentruntime

import (
	"strings"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

// The 2026-09-18 session ended with the model writing three closing-only DSML
// tags into the assistant text, with the opening halves never arriving. The
// detector previously knew only the half-width spellings, so the whole leftover
// was published as the reply and the turn settled as completed. The sentinel is
// spelled from runes here so the test file stays plain ASCII; the string built
// at runtime is the byte sequence the session recorded.
const incidentDSMLBar = "｜"

func incidentClosingTag(name string) string {
	return "</" + incidentDSMLBar + incidentDSMLBar + "DSML" + incidentDSMLBar + incidentDSMLBar + " " + name + ">"
}

var incidentAssistantText = strings.Join([]string{
	incidentClosingTag("parameter"),
	incidentClosingTag("invoke"),
	incidentClosingTag("calls"),
}, "\n")

func TestIsAssistantProtocolFragmentCoversTheRecordedSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "the incident closing-only tags", text: incidentAssistantText, want: true},
		{name: "one closing-only tag", text: incidentClosingTag("parameter"), want: true},
		{
			name: "single sentinel bar",
			text: "<" + incidentDSMLBar + "DSML" + incidentDSMLBar + "tool_calls>",
			want: true,
		},
		{
			name: "sentinel without a space before the tag name",
			text: "</" + incidentDSMLBar + "DSML" + incidentDSMLBar + "invoke>",
			want: true,
		},
		{name: "bare protocol spelling", text: "<parameter name=\"cmd\">pwd</parameter>", want: true},
		{name: "unclosed tag", text: "<write_stdin", want: true},
		{name: "comparison is not a tag", text: "use a < b when sorting", want: false},
		{name: "html mention is not DSML", text: "Wrap the label in a <div> please.", want: false},
		{
			name: "an answer that quotes the protocol stays an answer",
			text: "引擎用 " + "<" + incidentDSMLBar + "DSML" + incidentDSMLBar + "tool_calls>" + " 作为工具调用标记，下面是格式说明。",
			want: false,
		},
		{
			name: "an answer followed by a leaked close tag stays an answer",
			text: "已经检查完仓库里的相关文件，结论见上。" + incidentClosingTag("parameter"),
			want: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isAssistantProtocolFragment(test.text); got != test.want {
				t.Fatalf("isAssistantProtocolFragment(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

// A fragment that streams in pieces must not reach the conversation as it
// arrives: the assistant message is republished as a snapshot keyed by
// messageId, so withholding the snapshot is what keeps it out of the bubble.
func TestAppendAssistantChunkWithholdsStreamedProtocolFragment(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	chunks := []string{
		"</",
		incidentDSMLBar + incidentDSMLBar + "DSML" + incidentDSMLBar,
		incidentDSMLBar + " parameter>",
		"\n" + incidentClosingTag("invoke") + "\n" + incidentClosingTag("calls"),
	}
	var events []activityshared.Event
	for _, chunk := range chunks {
		events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", chunk)...)
	}
	events = append(events, normalizer.Finish(session, "turn-1", messageStreamStateCompleted)...)

	for _, event := range events {
		if strings.Contains(event.Payload.Content, "DSML") || strings.Contains(event.Payload.Content, incidentDSMLBar) {
			t.Fatalf("protocol residue reached the conversation as text: %q", event.Payload.Content)
		}
	}
	if _, ok := assertCompletedAssistantContent(t, events); ok {
		t.Fatal("a withheld fragment was published as the answer")
	}
	if !normalizer.AssistantAnswerWithheldAsProtocolFragment() {
		t.Fatal("the turn does not report that its answer was withheld")
	}
}

// An answer published earlier in the same turn keeps the turn valid: the
// withheld text was trailing noise, not the reply. The trailing markup itself
// stays visible, because the prose guard must not strip an answer that merely
// writes about the protocol it uses.
func TestAssistantAnswerSurvivesATrailingProtocolFragment(t *testing.T) {
	t.Parallel()

	const answer = "已经检查完仓库里的相关文件，结论见上。"
	session := testSession()
	normalizer := newACPTurnNormalizer()

	var events []activityshared.Event
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", "已经检查完仓库里的相关文件，")...)
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", "结论见上。")...)
	normalizer.ApplyAssistantFinalText(answer + incidentAssistantText)
	events = append(events, normalizer.Finish(session, "turn-1", messageStreamStateCompleted)...)

	content, ok := assertCompletedAssistantContent(t, events)
	if !ok {
		t.Fatal("the answer was dropped together with the trailing fragment")
	}
	if !strings.HasPrefix(content, answer) {
		t.Fatalf("assistant content = %q, want it to keep the answer", content)
	}
	if normalizer.AssistantAnswerWithheldAsProtocolFragment() {
		t.Fatal("a turn with a published answer must not report a withheld answer")
	}
}

// The provider app-server reports "completed" for a turn it did not fail
// itself. When the only assistant output was withheld protocol markup there is
// no answer, so the host must report a failed turn instead of a successful
// empty one.
func TestAppServerTurnTerminalEventsFailsTurnWhoseAnswerWasWithheld(t *testing.T) {
	t.Parallel()

	session := testSession()

	withheld := newACPTurnNormalizer()
	withheld.ApplyAssistantFinalText(incidentAssistantText)
	events := appServerTurnTerminalEvents(session, "turn-1", map[string]any{
		"id": "provider-turn-1", "status": "completed",
	}, withheld)

	failed := false
	for _, event := range events {
		if event.Type != activityshared.EventRootProviderTurnCompleted {
			continue
		}
		if event.Payload.TurnOutcome == string(activityshared.TurnOutcomeCompleted) {
			t.Fatalf("turn reported completed with no answer: %#v", event.Payload)
		}
		if event.Payload.TurnOutcome == string(activityshared.TurnOutcomeFailed) {
			failed = true
			if !strings.Contains(activityshared.BestEffortErrorMessage(event.Payload), "provider_empty_response") {
				t.Fatalf("failure carries no recognizable code: %#v", event.Payload.Metadata)
			}
		}
	}
	if !failed {
		t.Fatalf("no failed terminal event in %#v", events)
	}

	answered := newACPTurnNormalizer()
	answered.ApplyAssistantFinalText("已经检查完仓库里的相关文件，结论见上。")
	answerEvents := appServerTurnTerminalEvents(session, "turn-2", map[string]any{
		"id": "provider-turn-2", "status": "completed",
	}, answered)
	completed := false
	for _, event := range answerEvents {
		if event.Type == activityshared.EventRootProviderTurnCompleted &&
			event.Payload.TurnOutcome == string(activityshared.TurnOutcomeCompleted) {
			completed = true
		}
	}
	if !completed {
		t.Fatalf("an ordinary completed turn was reported as %#v", answerEvents)
	}
}
