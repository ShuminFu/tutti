// Package contextwindow carries the "[1m]" model-id marker: the convention a
// user asks for a 1M-token context window with. The marker rides the model
// value itself so every provider agrees on one carrier, and each runtime
// decides how to consume it — Claude Code strips it outbound and adds the
// context-1m beta, the Codex app-server takes the window as thread config, and
// an ACP agent that has no such knob simply gets the bare model id.
package contextwindow

import "strings"

// OneMillionTokens is the window the marker asks for.
const OneMillionTokens int64 = 1_000_000

const marker = "[1m]"

// Split reports whether model carries the 1M marker and returns the bare id.
// Whitespace around either part is trimmed; the marker itself is matched
// case-insensitively so a hand-typed "[1M]" behaves like the catalog spelling.
func Split(model string) (string, bool) {
	trimmed := strings.TrimSpace(model)
	if len(trimmed) < len(marker) {
		return trimmed, false
	}
	if !strings.EqualFold(trimmed[len(trimmed)-len(marker):], marker) {
		return trimmed, false
	}
	return strings.TrimSpace(trimmed[:len(trimmed)-len(marker)]), true
}

// Bare returns model without the 1M marker.
func Bare(model string) string {
	base, _ := Split(model)
	return base
}

// WithMarker returns the 1M spelling of id. It is idempotent, and an empty id
// stays empty rather than becoming a bare marker.
func WithMarker(id string) string {
	base, _ := Split(id)
	if base == "" {
		return ""
	}
	return base + marker
}

// RequestsOneMillion reports whether model asks for the 1M window.
func RequestsOneMillion(model string) bool {
	_, ok := Split(model)
	return ok
}

// Window returns the token window model asks for, or 0 when it asks for none.
func Window(model string) int64 {
	if _, ok := Split(model); ok {
		return OneMillionTokens
	}
	return 0
}
