package agentruntime

import "strings"

// Provider-neutral assistant text purpose tags persisted as payload.messageKind.
// Codex maps native MessagePhase onto these values so copy targeting can prefer
// an explicit final answer. Turn disclosure no longer requires these tags:
// completed Turns fold unmarked intermediate replies in the GUI. Unknown or
// empty phases must not mint a known tag.
const (
	assistantMessageKindCommentary = "assistant-commentary"
	assistantMessageKindFinal      = "assistant-final"
)

func assistantMessageKindFromPhase(phase string) string {
	switch strings.TrimSpace(phase) {
	case "commentary":
		return assistantMessageKindCommentary
	case "final_answer":
		return assistantMessageKindFinal
	default:
		return ""
	}
}
