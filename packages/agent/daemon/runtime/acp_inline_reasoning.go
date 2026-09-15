package agentruntime

import "strings"

// Some providers routed through the daemon do not use the protocol's dedicated
// reasoning channel. They emit their chain of thought inline in the assistant
// text, wrapped in literal <think>...</think> markup. Codex app-server is the
// common case: a non-OpenAI backend (e.g. deepseek-flash) never sends
// item/reasoning/textDelta, and instead puts the whole monologue into
// item/agentMessage/delta and the final agentMessage item text.
//
// Left alone, that markup is rendered as the assistant's reply, so the user
// sees raw English deliberation instead of the answer. Worse, a response that
// is cut off mid-thought arrives with no closing tag at all, and the truncated
// fragment becomes the final answer.
//
// inlineReasoningSplitter separates the two channels without knowing anything
// about which provider produced the text. It is fed the same text the assistant
// segment would have received and returns the assistant-visible remainder plus
// the reasoning content to divert to the thinking channel.
//
// The splitter is deliberately tolerant of chunk boundaries: a tag may be split
// across two deltas ("<thi" then "nk>"), so a tail that could still become a tag
// is held back rather than emitted. A "<" that cannot begin a known tag is
// flushed immediately, so ordinary text such as "a < b" or "<div>" is never
// stalled or misclassified.
type inlineReasoningSplitter struct {
	// pending holds a tail that is a proper prefix of an open/close tag and may
	// be completed by the next chunk. It is never emitted as assistant text
	// until it is proven not to be a tag.
	pending string
	// open reports whether we are currently inside a <think> block, which is
	// how an unterminated block is detected at turn end.
	open bool
}

const (
	inlineReasoningOpenTag  = "<think>"
	inlineReasoningCloseTag = "</think>"
)

// Feed consumes the next slice of assistant text and returns the assistant text
// that should remain visible and the reasoning text to divert. Either may be
// empty. Text inside an unterminated <think> block is returned as reasoning, so
// a truncated monologue is diverted rather than shown as the answer.
func (s *inlineReasoningSplitter) Feed(text string) (assistant string, reasoning string) {
	if text == "" {
		return "", ""
	}
	buf := s.pending + text
	s.pending = ""

	var assistantOut strings.Builder
	var reasoningOut strings.Builder
	for len(buf) > 0 {
		if !s.open {
			idx := strings.Index(buf, inlineReasoningOpenTag)
			if idx < 0 {
				// No complete open tag. Hold back a tail that is still a
				// prefix of the tag so a split tag is not emitted as text.
				hold := inlineReasoningPartialTagSuffix(buf)
				assistantOut.WriteString(buf[:len(buf)-len(hold)])
				s.pending = hold
				break
			}
			assistantOut.WriteString(buf[:idx])
			buf = buf[idx+len(inlineReasoningOpenTag):]
			s.open = true
			continue
		}
		idx := strings.Index(buf, inlineReasoningCloseTag)
		if idx < 0 {
			// Still inside the block. Hold back a tail that could complete the
			// close tag; everything else is reasoning content. When nothing
			// more arrives the held tail is flushed as reasoning by Flush, so
			// an unterminated block is never lost.
			hold := inlineReasoningPartialTagSuffix(buf)
			reasoningOut.WriteString(buf[:len(buf)-len(hold)])
			s.pending = hold
			break
		}
		reasoningOut.WriteString(buf[:idx])
		buf = buf[idx+len(inlineReasoningCloseTag):]
		s.open = false
	}
	return assistantOut.String(), reasoningOut.String()
}

// Flush releases any held partial tail and reports whether the text ended while
// still inside an unterminated <think> block. The caller uses the bool to decide
// that the turn's remaining content is reasoning and must not be published as an
// assistant answer.
func (s *inlineReasoningSplitter) Flush() (assistant string, reasoning string, unterminated bool) {
	held := s.pending
	s.pending = ""
	if held == "" {
		return "", "", s.open
	}
	// The tail never completed into a tag, so it is ordinary text belonging to
	// whichever channel is currently being accumulated.
	if s.open {
		return "", held, true
	}
	return held, "", false
}

// InThinkBlock reports whether the splitter is currently inside an
// unterminated <think> block.
func (s *inlineReasoningSplitter) InThinkBlock() bool {
	return s != nil && s.open
}

func (s *inlineReasoningSplitter) Reset() {
	if s == nil {
		return
	}
	s.pending = ""
	s.open = false
}

// inlineReasoningPartialTagSuffix returns the longest suffix of text that is a
// proper prefix of an open or close tag. Returning a suffix (rather than only
// matching at the end) keeps the scan linear enough for streaming chunks while
// guaranteeing no tag is ever split across the assistant/reasoning boundary.
func inlineReasoningPartialTagSuffix(text string) string {
	max := len(inlineReasoningOpenTag)
	if closeLen := len(inlineReasoningCloseTag); closeLen > max {
		max = closeLen
	}
	if max > len(text) {
		max = len(text)
	}
	// A tag must start with '<'; start the scan at the last possible opener.
	for start := len(text) - max; start < len(text); start++ {
		if start < 0 {
			continue
		}
		if text[start] != '<' {
			continue
		}
		candidate := text[start:]
		if inlineReasoningIsTagPrefix(candidate) {
			return candidate
		}
	}
	return ""
}

func inlineReasoningIsTagPrefix(candidate string) bool {
	if candidate == "" {
		return false
	}
	// A lone "<" is held back only because it may begin a tag; callers flush it
	// on the next chunk or at turn end, so it is never dropped.
	if strings.HasPrefix(inlineReasoningOpenTag, candidate) ||
		strings.HasPrefix(inlineReasoningCloseTag, candidate) {
		return true
	}
	return false
}

// stripInlineReasoningTags removes complete <think>...</think> blocks and any
// dangling opening tag from a whole authoritative text. It exists for paths
// outside the streaming splitter (such as the plan item snapshot) that receive
// one finished string rather than a chunk sequence.
func stripInlineReasoningTags(text string) string {
	if text == "" {
		return text
	}
	if !strings.Contains(text, "<think") {
		return text
	}
	splitter := &inlineReasoningSplitter{}
	assistant, _ := splitter.Feed(text)
	assistantTail, _, _ := splitter.Flush()
	return assistant + assistantTail
}
