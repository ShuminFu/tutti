package agentruntime

import (
	"regexp"
	"strings"
	"unicode"
)

// DeepSeek (and similar Chat-compat models) sometimes emit a tool call as
// DSML markup in the assistant text instead of a protocol tool item. When the
// stream is cut off, that leftover is the entire "answer":
// `<write_stdin">\n</parameter>`. Publishing it as user-visible copy is the
// protocol-fragment class of a dead session.
var protocolFragmentMarkup = regexp.MustCompile(
	`(?is)</?(?:parameter|write_stdin|invoke|tool_call|tool_calls|function_calls)(?:\b[^>]*)?>`,
)

func isAssistantProtocolFragment(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if looksLikeDSML(lower) {
		if !strings.Contains(trimmed, ">") {
			return true
		}
		return !hasVisibleProse(protocolFragmentMarkup.ReplaceAllString(trimmed, " "))
	}
	if strings.HasPrefix(trimmed, "<") && !strings.Contains(trimmed, ">") && len(trimmed) < 240 {
		return true
	}
	return false
}

func looksLikeDSML(lower string) bool {
	for _, marker := range []string{
		"<parameter", "</parameter>", "<write_stdin",
		"<invoke", "</invoke>", "<tool_call", "</tool_call>",
	} {
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
