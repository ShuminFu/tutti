package agentruntime

import (
	"strings"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestInlineReasoningSplitterRoutesClosedBlockToThinking(t *testing.T) {
	t.Parallel()

	splitter := &inlineReasoningSplitter{}
	assistant, reasoning := splitter.Feed("<think>weighing options</think>Here is the answer.")
	if reasoning != "weighing options" {
		t.Fatalf("reasoning = %q, want the block contents", reasoning)
	}
	if assistant != "Here is the answer." {
		t.Fatalf("assistant = %q, want only the post-block text", assistant)
	}
	if splitter.InThinkBlock() {
		t.Fatal("splitter still reports an open think block after a closed one")
	}
}

func TestInlineReasoningSplitterHandlesTagSplitAcrossChunks(t *testing.T) {
	t.Parallel()

	splitter := &inlineReasoningSplitter{}
	var assistant, reasoning strings.Builder
	for _, chunk := range []string{"<thi", "nk>deliberating</thi", "nk>", "Final answer."} {
		a, r := splitter.Feed(chunk)
		assistant.WriteString(a)
		reasoning.WriteString(r)
	}
	if got := reasoning.String(); got != "deliberating" {
		t.Fatalf("reasoning = %q, want the block contents reassembled across chunks", got)
	}
	if got := assistant.String(); got != "Final answer." {
		t.Fatalf("assistant = %q, want only the visible answer", got)
	}
}

func TestInlineReasoningSplitterKeepsOrdinaryAngleBracketText(t *testing.T) {
	t.Parallel()

	// A bare "<" that cannot begin a known tag must not be held back, or normal
	// prose stalls until the turn ends.
	splitter := &inlineReasoningSplitter{}
	assistant, reasoning := splitter.Feed("use a < b and <div> here")
	if reasoning != "" {
		t.Fatalf("reasoning = %q, want none for ordinary angle-bracket prose", reasoning)
	}
	if assistant != "use a < b and <div> here" {
		t.Fatalf("assistant = %q, want the prose preserved verbatim", assistant)
	}
}

func TestInlineReasoningSplitterReportsUnterminatedBlock(t *testing.T) {
	t.Parallel()

	splitter := &inlineReasoningSplitter{}
	splitter.Feed("<think>truncated mid thought")
	assistantTail, reasoningTail, unterminated := splitter.Flush()
	if !unterminated {
		t.Fatal("unterminated = false, want true for a block with no closing tag")
	}
	if reasoningTail != "" || assistantTail != "" {
		t.Fatalf("tails = (%q, %q), want empty because all text was already diverted", assistantTail, reasoningTail)
	}
}

func TestInlineReasoningSplitterFlushesHeldTagTailAsReasoning(t *testing.T) {
	t.Parallel()

	// The final chunk ends with a partial close tag; Flush must release it as
	// reasoning rather than dropping it.
	splitter := &inlineReasoningSplitter{}
	splitter.Feed("<think>thinking</thi")
	_, reasoningTail, unterminated := splitter.Flush()
	if !unterminated {
		t.Fatal("unterminated = false, want true")
	}
	if reasoningTail != "</thi" {
		t.Fatalf("reasoningTail = %q, want the held partial tag released as reasoning", reasoningTail)
	}
}

func assertCompletedAssistantContent(t *testing.T, events []activityshared.Event) (string, bool) {
	t.Helper()
	for _, event := range events {
		if event.Type != activityshared.EventMessageAppended && event.Type != activityshared.EventMessageCreated {
			continue
		}
		if event.Payload.Role != activityshared.MessageRoleAssistant {
			continue
		}
		state, _ := event.Payload.Metadata["streamState"].(string)
		if state == messageStreamStateCompleted {
			return event.Payload.Content, true
		}
	}
	return "", false
}

func assertCompletedThinkingContent(t *testing.T, events []activityshared.Event) (string, bool) {
	t.Helper()
	content := ""
	found := false
	for _, event := range events {
		if event.Type != activityshared.EventMessageAppended && event.Type != activityshared.EventMessageCreated {
			continue
		}
		if event.Payload.Role != activityshared.MessageRoleAssistantThinking {
			continue
		}
		state, _ := event.Payload.Metadata["streamState"].(string)
		if state != messageStreamStateCompleted {
			continue
		}
		content = event.Payload.Content
		found = true
	}
	return content, found
}

func TestAppendAssistantChunkDivertsClosedThinkBlock(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	var events []activityshared.Event
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", "<think>considering the ticket</think>")...)
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", "工单已处理。")...)
	events = append(events, normalizer.Finish(session, "turn-1", messageStreamStateCompleted)...)

	content, ok := assertCompletedAssistantContent(t, events)
	if !ok {
		t.Fatal("no completed assistant message")
	}
	if content != "工单已处理。" {
		t.Fatalf("assistant content = %q, want only the visible answer", content)
	}

	thinking, ok := assertCompletedThinkingContent(t, events)
	if !ok {
		t.Fatal("no completed thinking message, want the inline block diverted to the thinking channel")
	}
	if thinking != "considering the ticket" {
		t.Fatalf("thinking content = %q, want the diverted monologue", thinking)
	}
}

func TestApplyAssistantFinalTextDropsUnterminatedThinkFromAnswer(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	// A response cut off mid-thought: no closing tag, so there is no answer.
	normalizer.ApplyAssistantFinalText("<think>\nNeed read command-guide issue entries. Use rg maybe.")
	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)

	if content, ok := assertCompletedAssistantContent(t, events); ok {
		t.Fatalf("assistant answer = %q, want none: a truncated monologue is not an answer", content)
	}
	thinking, ok := assertCompletedThinkingContent(t, events)
	if !ok {
		t.Fatal("no completed thinking message, want the truncated monologue preserved as reasoning")
	}
	if !strings.Contains(thinking, "Need read command-guide") {
		t.Fatalf("thinking content = %q, want the truncated monologue", thinking)
	}
}

func TestApplyAssistantFinalTextKeepsAnswerAfterClosedThink(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	normalizer.ApplyAssistantFinalText("<think>weighing it</think>\n已按工单要求完成修改。")
	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)

	content, ok := assertCompletedAssistantContent(t, events)
	if !ok {
		t.Fatal("no completed assistant message")
	}
	if content != "已按工单要求完成修改。" {
		t.Fatalf("assistant content = %q, want the post-think answer", content)
	}
	if strings.Contains(content, "<think>") {
		t.Fatal("assistant content still contains raw think markup")
	}
}

func TestApplyAssistantFinalTextClearsStreamedMonologueOnTruncatedTurn(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	// The provider streamed the monologue as assistant text first (the reported
	// bug), then the authoritative final text confirms it was all reasoning.
	_ = normalizer.AppendAssistantChunk(session, "turn-1", "<think>Now inspect local/remote relation and files")
	normalizer.ApplyAssistantFinalText("<think>Now inspect local/remote relation and files in remote maybe no conflicts.")
	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)

	if content, ok := assertCompletedAssistantContent(t, events); ok {
		t.Fatalf("assistant answer published for a truncated turn: %q", content)
	}
	if _, ok := assertCompletedThinkingContent(t, events); !ok {
		t.Fatal("no thinking messages, want the monologue routed to the thinking channel")
	}
}

func TestAppendAssistantChunkLeavesPlainTextUntouched(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	var events []activityshared.Event
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", "Hello! How can")...)
	events = append(events, normalizer.AppendAssistantChunk(session, "turn-1", " I help you?")...)
	events = append(events, normalizer.Finish(session, "turn-1", messageStreamStateCompleted)...)

	if thinking, ok := assertCompletedThinkingContent(t, events); ok {
		t.Fatalf("thinking content = %q, want none for plain text", thinking)
	}
	content, ok := assertCompletedAssistantContent(t, events)
	if !ok {
		t.Fatal("no completed assistant message")
	}
	if content != "Hello! How can I help you?" {
		t.Fatalf("assistant content = %q, want the plain text unchanged", content)
	}
}

func TestStripInlineReasoningTagsRemovesBlocksAndDanglingTag(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"closed block", "<think>a</think>plan text", "plan text"},
		{"leading block", "<think>a</think>", ""},
		{"no markup", "plain plan", "plain plan"},
		{"unterminated block", "<think>a", ""},
		{"multiple blocks", "<think>a</think>keep<think>b</think>", "keep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stripInlineReasoningTags(tc.in); got != tc.want {
				t.Fatalf("stripInlineReasoningTags(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestInlineReasoningKeepsTurnObservable pins the ordering contract that
// standard ACP relies on: ApplyAssistantFinalText runs before
// HasObservableOutput. A turn whose whole output was inline reasoning must still
// count as observable, otherwise it is misreported as provider_empty_response
// and the user loses both the answer and the thinking row.
func TestInlineReasoningKeepsTurnObservable(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	normalizer.ApplyAssistantFinalText("<think>only reasoning, no final answer</think>")
	if !normalizer.HasObservableOutput() {
		t.Fatal("HasObservableOutput = false, want true so a reasoning-only turn is not reported as an empty response")
	}

	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)
	thinking, ok := assertCompletedThinkingContent(t, events)
	if !ok {
		t.Fatal("no completed thinking message for a reasoning-only turn")
	}
	if thinking != "only reasoning, no final answer" {
		t.Fatalf("thinking content = %q, want the diverted reasoning", thinking)
	}
}

// TestInlineReasoningDoesNotLeakAcrossSegments guards the splitter's held tag
// tail from surviving a segment reset, which would corrupt the next turn's text.
func TestInlineReasoningDoesNotLeakAcrossSegments(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	_ = normalizer.AppendAssistantChunk(session, "turn-1", "<thi")
	_ = normalizer.AppendAssistantChunk(session, "turn-1", "nk>thought</think>visible")
	events := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)

	content, ok := assertCompletedAssistantContent(t, events)
	if !ok {
		t.Fatal("no completed assistant message")
	}
	if content != "visible" {
		t.Fatalf("assistant content = %q, want only text outside the think block", content)
	}
}
