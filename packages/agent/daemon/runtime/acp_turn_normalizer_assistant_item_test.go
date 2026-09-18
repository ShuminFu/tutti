package agentruntime

import (
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestBindAssistantItemDoesNotInheritPhaseAcrossItems(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	if events := normalizer.BindAssistantItem(session, "turn-1", "item-a", "commentary"); len(events) != 0 {
		t.Fatalf("bind without text emitted %#v", events)
	}
	if events := normalizer.AppendAssistantChunk(session, "turn-1", "Looking around."); len(events) != 1 {
		t.Fatalf("commentary stream events = %d, want 1", len(events))
	}
	first := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "Looking around.", "commentary")
	if got := assistantKind(first); got != assistantMessageKindCommentary {
		t.Fatalf("commentary kind = %#v, want %q", got, assistantMessageKindCommentary)
	}

	if events := normalizer.BindAssistantItem(session, "turn-1", "item-b", "final_answer"); len(events) != 0 {
		t.Fatalf("bind next item emitted %#v", events)
	}
	second := normalizer.FinishAssistantItem(session, "turn-1", "item-b", "Here is the answer.", "final_answer")
	if len(second) != 1 || second[0].EventID == first[0].EventID {
		t.Fatalf("final item reused commentary bubble: first=%#v second=%#v", first, second)
	}
	if got := assistantKind(second); got != assistantMessageKindFinal {
		t.Fatalf("final kind = %#v, want %q", got, assistantMessageKindFinal)
	}
}

func TestFinishAssistantItemAllowsKindUpdateWhenTextUnchanged(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()
	text := "Inspecting the repo."

	streamed := normalizer.AppendAssistantChunkForItem(session, "turn-1", "item-a", text)
	if len(streamed) != 1 {
		t.Fatalf("stream events = %d, want 1", len(streamed))
	}
	closed := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)
	if got := assistantKind(closed); got != "" {
		t.Fatalf("early finish kind = %#v, want empty", got)
	}

	updated := normalizer.FinishAssistantItem(session, "turn-1", "item-a", text, "commentary")
	if len(updated) != 1 || updated[0].EventID != streamed[0].EventID {
		t.Fatalf("kind confirmation = %#v, want original message %q", updated, streamed[0].EventID)
	}
	if got := assistantKind(updated); got != assistantMessageKindCommentary {
		t.Fatalf("late kind = %#v, want %q", got, assistantMessageKindCommentary)
	}
}

func TestFinishAssistantItemKeepsSameTextDifferentItemsSeparate(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()
	text := "Same words."

	first := normalizer.FinishAssistantItem(session, "turn-1", "item-a", text, "commentary")
	second := normalizer.FinishAssistantItem(session, "turn-1", "item-b", text, "final_answer")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("items = %#v %#v, want one event each", first, second)
	}
	if first[0].EventID == second[0].EventID {
		t.Fatalf("distinct items collapsed onto %q", first[0].EventID)
	}
	if assistantKind(first) != assistantMessageKindCommentary || assistantKind(second) != assistantMessageKindFinal {
		t.Fatalf("kinds = %q %q", assistantKind(first), assistantKind(second))
	}
}

func TestFinishAssistantItemEmptyPhaseDoesNotClearKnownKind(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	_ = normalizer.BindAssistantItem(session, "turn-1", "item-a", "commentary")
	first := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "Working on it.", "commentary")
	replay := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "Working on it.", "")
	if assistantKind(first) != assistantMessageKindCommentary {
		t.Fatalf("initial kind = %#v", assistantKind(first))
	}
	if got := assistantKind(replay); got != "" && got != assistantMessageKindCommentary {
		t.Fatalf("empty phase replay kind = %#v, must not replace commentary", got)
	}
	if normalizer.assistantMessageKind != assistantMessageKindCommentary {
		t.Fatalf("stored kind = %q, want commentary", normalizer.assistantMessageKind)
	}
}

func TestLateCompletedItemDoesNotPolluteCurrentSegment(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()

	first := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "First commentary.", "commentary")
	_ = normalizer.AppendAssistantChunkForItem(session, "turn-1", "item-b", "Drafting the answer")
	late := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "First commentary.", "commentary")
	if len(late) != 1 || late[0].EventID != first[0].EventID {
		t.Fatalf("late completed = %#v, want original item %q", late, first[0].EventID)
	}
	if normalizer.assistantItemID != "item-b" {
		t.Fatalf("current item = %q, want item-b", normalizer.assistantItemID)
	}
	final := normalizer.FinishAssistantItem(session, "turn-1", "item-b", "Drafting the answer.", "final_answer")
	if len(final) != 1 || final[0].EventID == first[0].EventID {
		t.Fatalf("current final = %#v, collided with commentary", final)
	}
	if assistantKind(final) != assistantMessageKindFinal {
		t.Fatalf("final kind = %#v", assistantKind(final))
	}
}

func TestTurnFinalTextDoesNotRewriteCompletedItem(t *testing.T) {
	t.Parallel()

	session := testSession()
	normalizer := newACPTurnNormalizer()
	first := normalizer.FinishAssistantItem(session, "turn-1", "item-a", "Hello.", "final_answer")
	normalizer.ApplyAssistantFinalText("Hello.\nChecking files too.")
	second := normalizer.Finish(session, "turn-1", messageStreamStateCompleted)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("events first=%#v second=%#v", first, second)
	}
	if second[0].EventID == first[0].EventID {
		t.Fatalf("turn snapshot rewrote item %q", first[0].EventID)
	}
	if assistantKind(first) != assistantMessageKindFinal {
		t.Fatalf("item kind = %#v", assistantKind(first))
	}
	if assistantKind(second) != "" {
		t.Fatalf("turn snapshot inherited item kind = %#v", assistantKind(second))
	}
}

func TestUnknownPhaseDoesNotMintAssistantKind(t *testing.T) {
	t.Parallel()

	if got := assistantMessageKindFromPhase(""); got != "" {
		t.Fatalf("empty phase = %q", got)
	}
	if got := assistantMessageKindFromPhase("thinking"); got != "" {
		t.Fatalf("unknown phase = %q", got)
	}
}

func assistantKind(events []activityshared.Event) string {
	messages := activityMessagesWithRole(events, activityshared.MessageRoleAssistant)
	if len(messages) == 0 {
		return ""
	}
	kind, _ := messages[0].Payload.Metadata["messageKind"].(string)
	return kind
}
