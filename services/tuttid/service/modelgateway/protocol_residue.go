package modelgateway

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// errModelProtocolResidue marks an upstream Chat response whose assistant text
// was nothing but tool protocol markup. Handler paths translate it into the
// model_protocol_error code instead of reporting a generic upstream error.
var errModelProtocolResidue = errors.New("model_protocol_error: upstream returned tool protocol markup in place of an answer")

// The Chat-completions upstreams this bridge fronts are not always faithful:
// DeepSeek sometimes spells a tool call as markup inside message content
// instead of emitting structured tool_calls. Passing that content through
// verbatim hands the Responses client a turn whose entire answer is protocol
// residue - the 2026-09-18 session ended on three closing-only DSML tags, which
// carry no opening half to reconstruct a call from. Such a turn must fail
// closed rather than complete.
//
// Two spellings appear. The bare one writes the protocol tag names directly.
// The DSML one wraps the same names in a full-width ｜DSML｜ sentinel, and the
// model varies the spelling: the incident used doubled sentinel bars and a
// space between the sentinel and the tag name, so only a sentinel-shaped match
// catches it.
var (
	// chatProtocolTag matches either spelling in one pass, so the residue check
	// and the stream holdback cannot drift apart on what counts as markup.
	chatProtocolTag = regexp.MustCompile(
		`(?is)(?:</?[^\s>]*[｜|]+\s*DSML\s*[｜|]+[^>]*>` +
			`|</?(?:parameter|write_stdin|invoke|tool_call|tool_calls|function_calls)(?:\b[^>]*)?>)`,
	)
)

// assistantTextIsProtocolResidue reports whether text carries protocol markup
// and, once every markup tag is removed, nothing a reader would call an answer.
// The prose guard is what keeps an answer that merely quotes the protocol - a
// reply explaining the format, or a document example - out of this class.
func assistantTextIsProtocolResidue(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || !chatProtocolTag.MatchString(trimmed) {
		return false
	}
	return !hasReadableProse(chatProtocolTag.ReplaceAllString(trimmed, " "))
}

// streamHoldbackWindow bounds how far back the streaming holdback looks, and
// therefore how much text it can delay. A protocol fragment is short, and the
// terminal residue check still judges the whole text once the stream ends, so
// the bound shapes how early the client sees a tail, never the verdict. Without
// it the holdback would rescan the whole accumulated answer on every delta.
const streamHoldbackWindow = 8 << 10

// streamingHoldbackStart returns the index from which accumulated assistant
// text must not be sent to the client yet, because it is still a candidate for
// protocol markup: a trailing run of complete tags, or a trailing tag that has
// not received its closing angle bracket. Deltas before that index are safe,
// and the caller releases the rest once the text proves to be prose.
func streamingHoldbackStart(text string) int {
	start := 0
	if len(text) > streamHoldbackWindow {
		start = len(text) - streamHoldbackWindow
	}
	window := text[start:]
	cut := len(window)
	matches := chatProtocolTag.FindAllStringIndex(window, -1)
	for index := len(matches) - 1; index >= 0; index-- {
		match := matches[index]
		// Walking backwards, the run only continues while each earlier tag is
		// still separated from the held text by whitespace alone.
		if match[1] > cut || strings.TrimSpace(window[match[1]:cut]) != "" {
			break
		}
		cut = match[0]
	}
	if open := strings.LastIndex(window[:cut], "<"); open >= 0 && !strings.Contains(window[open:cut], ">") {
		cut = open
	}
	return start + cut
}

func hasReadableProse(text string) bool {
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
