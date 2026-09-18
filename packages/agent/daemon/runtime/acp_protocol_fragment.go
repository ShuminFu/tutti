package agentruntime

import (
	"regexp"
	"strings"
	"unicode"
)

// DeepSeek (and similar Chat-compat models) sometimes emit a tool call as
// markup in the assistant text instead of a protocol tool item. When the
// stream is cut off, that leftover is the entire "answer", and publishing it
// as user-visible copy is the protocol-fragment class of a dead session.
//
// Two spellings reach us. The bare one writes the protocol tag names directly.
// The DSML one wraps the same names in a full-width ｜DSML｜ sentinel, and the
// model is not consistent about it: the 2026-09-18 session ended with three
// closing-only tags written with doubled sentinel bars and a space between the
// sentinel and the tag name. A half-width marker list cannot see those, and
// closing-only tags carry no opening half to match either, so the whole
// leftover fell through to the user as the assistant reply.
var (
	// bareProtocolTag matches the protocol tag names without a sentinel.
	bareProtocolTag = regexp.MustCompile(
		`(?is)</?(?:parameter|write_stdin|invoke|tool_call|tool_calls|function_calls)(?:\b[^>]*)?>`,
	)
	// dsmlProtocolTag matches any tag carrying the ｜DSML｜ sentinel, whatever
	// its name and whatever the bar count or spacing around the sentinel.
	dsmlProtocolTag = regexp.MustCompile(`(?is)</?[^\s>]*[｜|]+\s*DSML\s*[｜|]+[^>]*>`)
	// dsmlSentinel matches the sentinel alone, including inside a tag that has
	// not received its closing angle bracket yet.
	dsmlSentinel = regexp.MustCompile(`(?i)[｜|]+\s*DSML\s*[｜|]+`)
)

// stripProtocolMarkup removes every protocol tag spelling from text so the
// prose guard judges what the reader would actually see.
func stripProtocolMarkup(text string) string {
	return bareProtocolTag.ReplaceAllString(dsmlProtocolTag.ReplaceAllString(text, " "), " ")
}

func isAssistantProtocolFragment(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if looksLikeDSML(trimmed) {
		if !strings.Contains(trimmed, ">") {
			return true
		}
		return !hasVisibleProse(stripProtocolMarkup(trimmed))
	}
	if strings.HasPrefix(trimmed, "<") && !strings.Contains(trimmed, ">") && len(trimmed) < 240 {
		return true
	}
	return false
}

// looksLikeDSML reports whether text carries protocol markup at all. It runs
// before the prose guard so that ordinary prose is never stripped and re-judged
// just because it happens to contain an angle bracket.
func looksLikeDSML(text string) bool {
	if dsmlSentinel.MatchString(text) {
		return true
	}
	if dsmlProtocolTag.MatchString(text) || bareProtocolTag.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"<parameter", "<write_stdin", "<invoke", "<tool_call"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasVisibleProse(text string) bool {
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			letters++
			if letters >= 8 {
				return true
			}
		}
	}
	return false
}
