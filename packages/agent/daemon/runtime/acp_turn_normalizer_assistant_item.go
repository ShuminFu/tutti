package agentruntime

import (
	"strings"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func (n *acpTurnNormalizer) AppendAssistantChunkForItem(
	session Session,
	turnID string,
	itemID string,
	chunk string,
) []activityshared.Event {
	events := n.BindAssistantItem(session, turnID, itemID, "")
	events = append(events, n.AppendAssistantChunk(session, turnID, chunk)...)
	n.recordAssistantItemMessage(itemID, n.assistantMessageID)
	return events
}

// BindAssistantItem associates the current (or next) assistant segment with a
// provider item and optional purpose phase. A different in-flight item is
// finished first so tool boundaries and the next delta cannot inherit its
// purpose. Empty phase never overwrites a known kind.
func (n *acpTurnNormalizer) BindAssistantItem(
	session Session,
	turnID string,
	itemID string,
	phase string,
) []activityshared.Event {
	if n == nil {
		return nil
	}
	itemID = strings.TrimSpace(itemID)
	kind := assistantMessageKindFromPhase(phase)
	if itemID == "" {
		if kind != "" {
			n.assistantMessageKind = kind
		}
		return nil
	}
	if n.assistantItemID == itemID {
		return n.applyAssistantItemKind(session, turnID, itemID, kind)
	}
	if n.assistantItemID == "" && n.assistantMessageID != "" && !n.assistantSegmentCompleted {
		n.assistantItemID = itemID
		n.recordAssistantItemMessage(itemID, n.assistantMessageID)
		return n.applyAssistantItemKind(session, turnID, itemID, kind)
	}
	var events []activityshared.Event
	if n.assistantMessageID != "" && n.assistantContent.Len() > 0 && !n.assistantSegmentCompleted {
		events = n.Finish(session, turnID, messageStreamStateCompleted)
	}
	n.assistantItemID = itemID
	n.rememberAssistantItemKind(itemID, kind)
	n.assistantMessageKind = n.assistantItemKinds[itemID]
	if existing := n.assistantItemMessageIDs[itemID]; existing != "" {
		n.assistantMessageID = existing
		n.assistantSegmentCompleted = true
		n.assistantContent.Reset()
		n.assistantStreamKind = acpAssistantStreamUnknown
		return events
	}
	n.assistantMessageID = itemID
	n.assistantContent.Reset()
	n.assistantSegmentCompleted = false
	n.assistantStreamKind = acpAssistantStreamUnknown
	return events
}

// FinishAssistantItem applies the item's authoritative text and purpose, then
// emits the completed snapshot. Late completed events for a previous item
// update that item in place and leave the current streaming segment alone.
func (n *acpTurnNormalizer) FinishAssistantItem(
	session Session,
	turnID string,
	itemID string,
	text string,
	phase string,
) []activityshared.Event {
	if n == nil {
		return nil
	}
	itemID = strings.TrimSpace(itemID)
	kind := assistantMessageKindFromPhase(phase)
	if itemID != "" && n.assistantItemID != "" && n.assistantItemID != itemID {
		return n.emitStandaloneAssistantItem(session, turnID, itemID, text, kind)
	}
	if itemID != "" {
		n.rememberAssistantItemKind(itemID, kind)
		n.assistantItemID = itemID
		n.assistantMessageKind = firstNonEmpty(n.assistantItemKinds[itemID], n.assistantMessageKind)
	} else if kind != "" {
		n.assistantMessageKind = kind
	}
	n.applyAssistantFinalText(text, itemID)
	events := n.Finish(session, turnID, messageStreamStateCompleted)
	if len(events) == 0 {
		events = n.emitAssistantKindUpdate(session, turnID, true)
	}
	n.recordAssistantItemMessage(itemID, n.assistantMessageID)
	return events
}

func (n *acpTurnNormalizer) ensureAssistantSegment(itemID string) {
	if n == nil {
		return
	}
	itemID = strings.TrimSpace(itemID)
	sameOpenSegment := n.assistantMessageID != "" && !n.assistantSegmentCompleted &&
		(itemID == "" || n.assistantItemID == itemID || n.assistantItemID == "")
	if sameOpenSegment {
		if itemID != "" && n.assistantItemID == "" {
			n.assistantItemID = itemID
			n.recordAssistantItemMessage(itemID, n.assistantMessageID)
		}
		return
	}
	if itemID != "" {
		n.assistantItemID = itemID
		n.assistantMessageKind = n.assistantItemKinds[itemID]
		if existing := n.assistantItemMessageIDs[itemID]; existing != "" {
			n.assistantMessageID = existing
		} else {
			n.assistantMessageID = itemID
		}
	} else {
		n.assistantMessageID = newID()
		n.assistantItemID = ""
		n.assistantMessageKind = ""
	}
	n.assistantContent.Reset()
	n.assistantSegmentCompleted = false
	n.assistantStreamKind = acpAssistantStreamUnknown
}

func (n *acpTurnNormalizer) applyAssistantItemKind(
	session Session,
	turnID string,
	itemID string,
	kind string,
) []activityshared.Event {
	previous := n.assistantMessageKind
	n.rememberAssistantItemKind(itemID, kind)
	n.assistantMessageKind = firstNonEmpty(n.assistantItemKinds[itemID], n.assistantMessageKind)
	n.recordAssistantItemMessage(itemID, n.assistantMessageID)
	if kind == "" || n.assistantMessageKind == previous {
		return nil
	}
	return n.emitAssistantKindUpdate(session, turnID, false)
}

func (n *acpTurnNormalizer) rememberAssistantItemKind(itemID, kind string) {
	if n == nil {
		return
	}
	itemID = strings.TrimSpace(itemID)
	kind = strings.TrimSpace(kind)
	if itemID == "" || kind == "" {
		return
	}
	if n.assistantItemKinds == nil {
		n.assistantItemKinds = map[string]string{}
	}
	n.assistantItemKinds[itemID] = kind
}

func (n *acpTurnNormalizer) recordAssistantItemMessage(itemID, messageID string) {
	if n == nil {
		return
	}
	itemID = strings.TrimSpace(itemID)
	messageID = strings.TrimSpace(messageID)
	if itemID == "" || messageID == "" {
		return
	}
	if n.assistantItemMessageIDs == nil {
		n.assistantItemMessageIDs = map[string]string{}
	}
	n.assistantItemMessageIDs[itemID] = messageID
}

func (n *acpTurnNormalizer) assistantSnapshotMetadata(streamState string) map[string]any {
	metadata := map[string]any{
		"messageId":   n.assistantMessageID,
		"contentMode": messageContentModeSnapshot,
		"streamState": streamState,
	}
	if kind := strings.TrimSpace(n.assistantMessageKind); kind != "" {
		metadata["messageKind"] = kind
	}
	return metadata
}

func (n *acpTurnNormalizer) emitAssistantKindUpdate(
	session Session,
	turnID string,
	completed bool,
) []activityshared.Event {
	if n == nil || n.assistantMessageID == "" || n.assistantContent.Len() == 0 {
		return nil
	}
	if strings.TrimSpace(n.assistantMessageKind) == "" {
		return nil
	}
	streamState := messageStreamStateStreaming
	if completed || n.assistantSegmentCompleted {
		streamState = messageStreamStateCompleted
	}
	event := n.assistantSnapshotEvent(session, turnID, streamState)
	if completed || n.assistantSegmentCompleted {
		n.assistantSegmentCompleted = true
	}
	return []activityshared.Event{event}
}

func (n *acpTurnNormalizer) emitStandaloneAssistantItem(
	session Session,
	turnID string,
	itemID string,
	text string,
	kind string,
) []activityshared.Event {
	n.rememberAssistantItemKind(itemID, kind)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	messageID := strings.TrimSpace(n.assistantItemMessageIDs[itemID])
	if messageID == "" {
		messageID = itemID
	}
	n.recordAssistantItemMessage(itemID, messageID)
	resolvedKind := firstNonEmpty(n.assistantItemKinds[itemID], kind)
	metadata := map[string]any{
		"messageId":   messageID,
		"contentMode": messageContentModeSnapshot,
		"streamState": messageStreamStateCompleted,
	}
	if resolvedKind != "" {
		metadata["messageKind"] = resolvedKind
	}
	return []activityshared.Event{newTurnActivityEventWithID(
		session,
		messageID,
		EventMessage,
		turnID,
		messageStreamStateCompleted,
		RoleAssistant,
		text,
		metadata,
	)}
}
