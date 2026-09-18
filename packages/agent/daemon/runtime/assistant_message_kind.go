package agentruntime

import "strings"

// Provider-neutral assistant text purpose tags persisted as payload.messageKind.
// Codex maps native MessagePhase onto these values; other ACP providers leave
// them unset. Unknown or empty phases must not mint a known tag.
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
