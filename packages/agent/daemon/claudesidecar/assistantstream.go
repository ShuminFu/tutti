package claudesidecar

import (
	"fmt"
)

type assistantSegmentState struct {
	messageID   string
	messageBase string
	kind        string // "assistant" or "thinking"
	source      string // "live" or "fallback"
	snapshot    string
	completed   bool
}

// AssistantStreamProjector mirrors assistantStream.ts: it turns streaming
// deltas and terminal content blocks into assistant/thinking events without
// double-emitting text that already streamed.
type AssistantStreamProjector struct {
	activeTurnID       func() string
	emit               Emitter
	currentMessageBase string
	sequence           int
	segmentsByKey      map[string]*assistantSegmentState
	segmentOrder       []string
	segmentKeyByIndex  map[string]string
}

func NewAssistantStreamProjector(activeTurnID func() string, emit Emitter) *AssistantStreamProjector {
	return &AssistantStreamProjector{
		activeTurnID:      activeTurnID,
		emit:              emit,
		segmentsByKey:     map[string]*assistantSegmentState{},
		segmentKeyByIndex: map[string]string{},
	}
}

func (p *AssistantStreamProjector) Reset() {
	p.currentMessageBase = ""
	p.sequence = 0
	p.segmentsByKey = map[string]*assistantSegmentState{}
	p.segmentOrder = nil
	p.segmentKeyByIndex = map[string]string{}
}

func (p *AssistantStreamProjector) SetMessageBase(messageID string) {
	p.currentMessageBase = messageID
}

func (p *AssistantStreamProjector) Start(index *int, kind string) {
	p.ensureLiveSegment(index, kind)
}

func (p *AssistantStreamProjector) AppendDelta(index *int, kind string, delta string) {
	p.emitDelta(p.ensureLiveSegment(index, kind), delta)
}

func (p *AssistantStreamProjector) CompleteIndex(index *int) bool {
	segment := p.segmentForIndex(index, "")
	if segment == nil {
		return false
	}
	p.completeSegment(index, segment, "")
	return true
}

func (p *AssistantStreamProjector) CompleteContent(kind string, messageBase string, content string, usedSegmentIDs map[string]struct{}) {
	if content == "" {
		return
	}
	if existing := p.segmentWithContent(kind, messageBase, content, usedSegmentIDs); existing != nil {
		p.completeSegment(nil, existing, content)
		usedSegmentIDs[existing.messageID] = struct{}{}
		return
	}
	base := messageBase
	if base == "" {
		base = p.activeTurnID()
	}
	segment := p.createSegment(kind, "fallback", base, nil)
	p.completeSegment(nil, segment, content)
	usedSegmentIDs[segment.messageID] = struct{}{}
}

func (p *AssistantStreamProjector) emitDelta(segment *assistantSegmentState, delta string) {
	if delta == "" {
		return
	}
	segment.snapshot += delta
	eventType := "assistant_delta"
	if segment.kind != "assistant" {
		eventType = "thinking_delta"
	}
	p.emit(eventType, "", map[string]any{
		"turnId":    p.activeTurnID(),
		"messageId": segment.messageID,
		"content":   delta,
		"snapshot":  segment.snapshot,
	})
}

func (p *AssistantStreamProjector) ensureLiveSegment(index *int, kind string) *assistantSegmentState {
	if existing := p.segmentForIndex(index, kind); existing != nil && !existing.completed {
		return existing
	}
	return p.createSegment(kind, "live", "", index)
}

func (p *AssistantStreamProjector) createSegment(kind string, source string, messageBase string, liveIndex *int) *assistantSegmentState {
	base := messageBase
	if base == "" {
		base = p.currentMessageBase
	}
	if base == "" {
		base = p.activeTurnID()
	}
	messageID := fmt.Sprintf("claude-sdk:%s:%s:%s:%d", kind, base, source, p.sequence)
	p.sequence++
	key := kind + ":" + messageID
	segment := &assistantSegmentState{
		messageID:   messageID,
		messageBase: base,
		kind:        kind,
		source:      source,
	}
	p.segmentsByKey[key] = segment
	p.segmentOrder = append(p.segmentOrder, key)
	if liveIndex != nil {
		p.segmentKeyByIndex[indexKey(kind, *liveIndex)] = key
	}
	return segment
}

func (p *AssistantStreamProjector) segmentForIndex(index *int, kind string) *assistantSegmentState {
	if index == nil {
		return nil
	}
	if kind != "" {
		key, ok := p.segmentKeyByIndex[indexKey(kind, *index)]
		if !ok {
			return nil
		}
		return p.segmentsByKey[key]
	}
	if key, ok := p.segmentKeyByIndex[indexKey("assistant", *index)]; ok {
		return p.segmentsByKey[key]
	}
	if key, ok := p.segmentKeyByIndex[indexKey("thinking", *index)]; ok {
		return p.segmentsByKey[key]
	}
	return nil
}

func (p *AssistantStreamProjector) completeSegment(index *int, segment *assistantSegmentState, fallbackText string) {
	if index != nil {
		delete(p.segmentKeyByIndex, indexKey(segment.kind, *index))
	}
	if segment.completed {
		return
	}
	tail := ""
	if fallbackText != "" && len(fallbackText) >= len(segment.snapshot) && fallbackText[:len(segment.snapshot)] == segment.snapshot {
		tail = fallbackText[len(segment.snapshot):]
	}
	if tail != "" && (segment.source != "fallback" || segment.snapshot != "") {
		p.emitDelta(segment, tail)
	} else if segment.snapshot == "" && fallbackText != "" {
		segment.snapshot = fallbackText
	}
	segment.completed = true
	if segment.snapshot == "" {
		return
	}
	eventType := "assistant_completed"
	if segment.kind != "assistant" {
		eventType = "thinking_completed"
	}
	p.emit(eventType, "", map[string]any{
		"turnId":    p.activeTurnID(),
		"messageId": segment.messageID,
		"content":   segment.snapshot,
	})
}

func (p *AssistantStreamProjector) segmentWithContent(kind string, messageBase string, content string, usedSegmentIDs map[string]struct{}) *assistantSegmentState {
	messageBases := map[string]struct{}{}
	for _, base := range []string{messageBase, p.currentMessageBase, p.activeTurnID()} {
		if base != "" {
			messageBases[base] = struct{}{}
		}
	}
	var tailCandidate *assistantSegmentState
	for _, key := range p.segmentOrder {
		segment := p.segmentsByKey[key]
		if segment == nil {
			continue
		}
		_, used := usedSegmentIDs[segment.messageID]
		_, baseMatches := messageBases[segment.messageBase]
		if segment.kind == kind && !used && baseMatches && segment.snapshot == content {
			return segment
		}
		if tailCandidate == nil && segment.kind == kind && !segment.completed && !used && baseMatches &&
			len(content) >= len(segment.snapshot) && content[:len(segment.snapshot)] == segment.snapshot {
			tailCandidate = segment
		}
	}
	return tailCandidate
}

func indexKey(kind string, index int) string {
	return fmt.Sprintf("%s:%d", kind, index)
}
