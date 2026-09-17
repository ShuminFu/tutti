package agentruntime

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// FailureCodeProviderProtocolIncompatible is the run-failure code for a model
// endpoint that rejects the tool protocol its caller declared. The Responses to
// Chat Model Gateway reaches this state when an Agent registers a tool the
// configured Chat endpoint cannot represent, so the upstream answers with a
// declaration error such as:
//
//	invalid_request_error: tools[7].type: unknown variant `custom`,
//	expected `function`
//
// The mismatch is between the client's tool declarations and the endpoint's
// tool vocabulary, not an authentication, quota, or generic provider crash.
// Consequently the conversation card must not offer sign-in, detect, or
// reinstall guidance — none of them can change an endpoint's tool vocabulary —
// and re-sending the unchanged request cannot succeed.
const FailureCodeProviderProtocolIncompatible = "provider_protocol_incompatible"

// providerToolDeclarationTypePattern matches the tool-declaration slot that a
// deserializer names when it rejects a tool's `type` field: `tools[7].type` for
// an indexed array (Serde and Go JSON shapes) or `tools.7.type` for a dotted
// path. Requiring the numbered slot keeps the match tied to one concrete
// declaration instead of any prose that happens to mention a tool type.
var providerToolDeclarationTypePattern = regexp.MustCompile(`tools(?:\[\d+\]|\.\d+)\s*\.\s*type`)

// providerToolTypeRejectionMarkers are the upstream verdicts that follow a
// rejected tool `type`. They stay narrow on purpose: the failure this code
// describes is a missing or unsupported variant, not every mention of "type"
// near "tool".
var providerToolTypeRejectionMarkers = []string{
	"unknown variant",
	"unsupported variant",
	"invalid tool type",
	"unsupported tool type",
	"not supported for tool type",
}

// ProviderToolProtocolIncompatible reports whether a provider failure detail
// describes a model endpoint rejecting a tool declaration it cannot represent.
//
// Deliberately narrow: the detail must name a numbered tool-declaration slot
// (`tools[7].type`) and the rejection verdict for that slot. A plain
// `invalid_request_error` raised for any other reason — a missing parameter, a
// malformed message, a rejected `developer` role — therefore keeps its generic
// classification instead of being reported as a tool-protocol mismatch.
func ProviderToolProtocolIncompatible(detail string) bool {
	if strings.TrimSpace(detail) == "" {
		return false
	}
	normalized := strings.ToLower(detail)
	if !providerToolDeclarationTypePattern.MatchString(normalized) {
		return false
	}
	for _, marker := range providerToolTypeRejectionMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

// CanonicalTurnErrorDetailLimit mirrors the published
// WorkspaceAgentTurnError.detail maxLength, so the read-side refinement below
// can never produce a value the durable Turn contract cannot carry.
const CanonicalTurnErrorDetailLimit = 240

// canonicalTurnErrorDetailEllipsis is counted inside the limit above, so a
// truncated detail stays within the published length instead of exceeding it.
const canonicalTurnErrorDetailEllipsis = "..."

// ProjectStoredTurnError refines a canonical Turn error for the read-side
// transport projection.
//
// Turns that settled before the runtime learned this classification keep a
// coarse stored code (typically `provider_error`) with the raw upstream text in
// the message. Re-deriving the narrow code from that stored text lets
// historical failures show the same reason as new ones without rewriting a
// single database row.
//
// It returns the effective code together with the raw detail that belongs to
// it. An unclassified `provider_error` additionally carries the readable
// upstream explanation (see ProviderErrorReadableDetail): no sign-in, detect,
// or reinstall step explains such a failure, so that text is the only reason a
// person or a host integration can show. Every other code returns its stored
// value with an empty detail, so unrelated failures keep exactly the transport
// projection they had before.
func ProjectStoredTurnError(code string, message string) (string, string) {
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode != "" && trimmedCode != "provider_error" && trimmedCode != "unknown" && trimmedCode != FailureCodeProviderProtocolIncompatible {
		return trimmedCode, ""
	}
	trimmedMessage := strings.TrimSpace(message)
	if !ProviderToolProtocolIncompatible(
		strings.Join([]string{trimmedCode, trimmedMessage}, " "),
	) {
		if trimmedCode == "provider_error" {
			return trimmedCode, ProviderErrorReadableDetail(trimmedMessage)
		}
		return trimmedCode, ""
	}
	return FailureCodeProviderProtocolIncompatible, limitCanonicalTurnErrorDetail(trimmedMessage)
}

func limitCanonicalTurnErrorDetail(value string) string {
	if len(value) <= CanonicalTurnErrorDetailLimit {
		return value
	}
	truncated := value[:CanonicalTurnErrorDetailLimit-len(canonicalTurnErrorDetailEllipsis)]
	// Cut on a rune boundary so cropping a multi-byte character cannot put an
	// invalid UTF-8 sequence into the transport projection.
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return strings.TrimRight(truncated, " \t\n") + canonicalTurnErrorDetailEllipsis
}
