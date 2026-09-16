package agentruntime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ProviderErrorReadableDetail turns the raw text of an unclassified provider
// failure into the one short explanation a person can act on.
//
// Codex reports an upstream HTTP rejection as the response body itself, for
// example the OpenAI-style envelope
//
//	{"error":{"code":"local_pool_unavailable","message":"...","type":"invalid_request_error"}}
//
// The human sentence sits inside `error.message`; the envelope around it is
// noise on a conversation card or in a host's task error. When the text holds
// such an envelope (optionally after a prefix like `unexpected status 400:`),
// the result is `<prefix>: <message> (<code>)`. Any other text is kept as-is.
//
// The result never carries credentials: bearer tokens, `sk-` keys, and
// `authorization` / `api_key`-style assignments are masked, and the value is
// capped at CanonicalTurnErrorDetailLimit so it always fits the published
// WorkspaceAgentTurnError.detail contract.
func ProviderErrorReadableDetail(raw string) string {
	cleaned := cleanVisibleErrorText(raw)
	if cleaned == "" {
		return ""
	}
	readable := cleaned
	if extracted, ok := providerErrorEnvelopeText(cleaned); ok {
		readable = extracted
	}
	return limitCanonicalTurnErrorDetail(redactProviderErrorSecrets(readable))
}

func providerErrorEnvelopeText(text string) (string, bool) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", false
	}
	var payload map[string]any
	// A Decoder stops after the first JSON value, so a trailing suffix such as
	// `, request id: ...` does not defeat the envelope match.
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&payload); err != nil {
		return "", false
	}
	message, code := providerErrorEnvelopeFields(payload)
	if message == "" {
		return "", false
	}
	if code != "" && !strings.Contains(message, code) {
		message = message + " (" + code + ")"
	}
	prefix := strings.TrimRight(strings.TrimSpace(text[:start]), ":： \t")
	if prefix != "" {
		message = prefix + ": " + message
	}
	return message, true
}

func providerErrorEnvelopeFields(payload map[string]any) (string, string) {
	switch nested := payload["error"].(type) {
	case map[string]any:
		return providerErrorScalar(nested["message"]), providerErrorScalar(nested["code"])
	case string:
		return strings.TrimSpace(nested), providerErrorScalar(payload["code"])
	}
	return firstNonEmptyString(
		providerErrorScalar(payload["message"]),
		providerErrorScalar(payload["detail"]),
	), providerErrorScalar(payload["code"])
}

func providerErrorScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	default:
		return ""
	}
}

var providerErrorSecretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer [redacted]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), "sk-[redacted]"},
	{
		regexp.MustCompile(`(?i)\b(authorization|x-api-key|api[_-]?key|access[_-]?token|secret)("?\s*[:=]\s*"?)[^\s"',;}]{8,}`),
		"${1}${2}[redacted]",
	},
}

func redactProviderErrorSecrets(value string) string {
	for _, rule := range providerErrorSecretPatterns {
		value = rule.pattern.ReplaceAllString(value, rule.replacement)
	}
	return value
}
